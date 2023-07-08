package checkpoint_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/poet/shared"
	"github.com/spacemeshos/post/initialization"
	postShared "github.com/spacemeshos/post/shared"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/bootstrap"
	"github.com/spacemeshos/go-spacemesh/checkpoint"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/poets"
	"github.com/spacemeshos/go-spacemesh/sql/recovery"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

const recoverLayer uint32 = 18

var goldenAtx = types.ATXID{1}

func atxequal(tb testing.TB, satx types.AtxSnapshot, vatx *types.VerifiedActivationTx, commitAtx types.ATXID, vrfnonce types.VRFPostIndex) {
	require.True(tb, bytes.Equal(satx.ID, vatx.ID().Bytes()))
	require.EqualValues(tb, satx.Epoch, vatx.PublishEpoch)
	require.True(tb, bytes.Equal(satx.CommitmentAtx, commitAtx.Bytes()))
	require.EqualValues(tb, satx.VrfNonce, vrfnonce)
	require.Equal(tb, satx.NumUnits, vatx.NumUnits)
	require.Equal(tb, satx.BaseTickHeight, vatx.BaseTickHeight())
	require.Equal(tb, satx.TickCount, vatx.TickCount())
	require.True(tb, bytes.Equal(satx.PublicKey, vatx.SmesherID.Bytes()))
	require.Equal(tb, satx.Sequence, vatx.Sequence)
	require.True(tb, bytes.Equal(satx.Coinbase, vatx.Coinbase.Bytes()))
	require.True(tb, vatx.Golden())
}

func accountequal(tb testing.TB, cacct types.AccountSnapshot, acct *types.Account) {
	require.True(tb, bytes.Equal(cacct.Address, acct.Address.Bytes()))
	require.Equal(tb, cacct.Balance, acct.Balance)
	require.Equal(tb, cacct.Nonce, acct.NextNonce)
	if cacct.Template == nil {
		require.Nil(tb, acct.TemplateAddress)
		require.Nil(tb, acct.State)
	} else {
		require.True(tb, bytes.Equal(cacct.Template, acct.TemplateAddress.Bytes()))
		require.True(tb, bytes.Equal(cacct.State, acct.State))
	}
}

func verifyDbContent(tb testing.TB, db *sql.Database) {
	var expected types.Checkpoint
	require.NoError(tb, json.Unmarshal([]byte(checkpointdata), &expected))
	expAtx := map[types.ATXID]types.AtxSnapshot{}
	for _, satx := range expected.Data.Atxs {
		expAtx[types.ATXID(types.BytesToHash(satx.ID))] = satx
	}
	expAcct := map[types.Address]types.AccountSnapshot{}
	for _, acct := range expected.Data.Accounts {
		var addr types.Address
		copy(addr[:], acct.Address)
		expAcct[addr] = acct
	}
	allIds, err := atxs.All(db)
	require.NoError(tb, err)
	var extra []*types.VerifiedActivationTx
	for _, id := range allIds {
		vatx, err := atxs.Get(db, id)
		require.NoError(tb, err)
		commitatx, err := atxs.CommitmentATX(db, vatx.SmesherID)
		require.NoError(tb, err)
		vrfnonce, err := atxs.VRFNonce(db, vatx.SmesherID, vatx.PublishEpoch+1)
		require.NoError(tb, err)
		if _, ok := expAtx[id]; ok {
			atxequal(tb, expAtx[id], vatx, commitatx, vrfnonce)
		} else {
			extra = append(extra, vatx)
		}
	}
	allAccts, err := accounts.All(db)
	require.NoError(tb, err)
	for _, acct := range allAccts {
		cacct, ok := expAcct[acct.Address]
		require.True(tb, ok)
		require.NotNil(tb, acct)
		accountequal(tb, cacct, acct)
		require.EqualValues(tb, recoverLayer-1, acct.Layer)
	}
	require.Empty(tb, extra)
}

func TestRecover(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(checkpointdata))
		require.NoError(t, err)
	}))
	defer ts.Close()

	tt := []struct {
		name   string
		uri    string
		expErr error
	}{
		{
			name: "http",
			uri:  fmt.Sprintf("%s/snapshot-15", ts.URL),
		},
		{
			name:   "url unreachable",
			uri:    "http://nowhere/snapshot-15",
			expErr: checkpoint.ErrCheckpointNotFound,
		},
		{
			name:   "ftp",
			uri:    "ftp://snapshot-15",
			expErr: checkpoint.ErrUrlSchemeNotSupported,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			fs := afero.NewMemMapFs()
			cfg := &checkpoint.RecoverConfig{
				GoldenAtx:      goldenAtx,
				PostDataDir:    t.TempDir(),
				DataDir:        t.TempDir(),
				DbFile:         "test.sql",
				PreserveOwnAtx: true,
				NodeID:         types.NodeID{2, 3, 4},
				Uri:            tc.uri,
				Restore:        types.LayerID(recoverLayer),
			}
			bsdir := filepath.Join(cfg.DataDir, bootstrap.DirName)
			require.NoError(t, fs.MkdirAll(bsdir, 0o700))
			db := sql.InMemory()
			preserve, err := checkpoint.RecoverWithDb(ctx, logtest.New(t), db, fs, cfg)
			if tc.expErr != nil {
				require.ErrorIs(t, err, tc.expErr)
				return
			}
			require.NoError(t, err)
			require.Nil(t, preserve)
			newdb, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
			require.NoError(t, err)
			require.NotNil(t, newdb)
			defer newdb.Close()
			verifyDbContent(t, newdb)
			restore, err := recovery.CheckpointInfo(newdb)
			require.NoError(t, err)
			require.EqualValues(t, recoverLayer, restore)
			exist, err := afero.Exists(fs, bsdir)
			require.NoError(t, err)
			require.False(t, exist)
		})
	}
}

func TestRecover_SameRecoveryInfo(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(checkpointdata))
		require.NoError(t, err)
	}))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fs := afero.NewMemMapFs()
	cfg := &checkpoint.RecoverConfig{
		GoldenAtx:      goldenAtx,
		PostDataDir:    t.TempDir(),
		DataDir:        t.TempDir(),
		DbFile:         "test.sql",
		PreserveOwnAtx: true,
		NodeID:         types.NodeID{2, 3, 4},
		Uri:            fmt.Sprintf("%s/snapshot-15", ts.URL),
		Restore:        types.LayerID(recoverLayer),
	}
	bsdir := filepath.Join(cfg.DataDir, bootstrap.DirName)
	require.NoError(t, fs.MkdirAll(bsdir, 0o700))
	db := sql.InMemory()
	types.SetEffectiveGenesis(0)
	require.NoError(t, recovery.SetCheckpoint(db, types.LayerID(recoverLayer)))
	preserve, err := checkpoint.RecoverWithDb(ctx, logtest.New(t), db, fs, cfg)
	require.NoError(t, err)
	require.Nil(t, preserve)
	require.EqualValues(t, recoverLayer-1, types.GetEffectiveGenesis())
	exist, err := afero.Exists(fs, bsdir)
	require.NoError(t, err)
	require.True(t, exist)
}

func validateAndPreserveData(tb testing.TB, db *sql.Database, deps []*types.VerifiedActivationTx, proofs []*types.PoetProofMessage) {
	lg := logtest.New(tb)
	layersPerEpoch := uint32(3)
	edVerifier, err := signing.NewEdVerifier()
	require.NoError(tb, err)
	poetDb := activation.NewPoetDb(db, lg)
	ctrl := gomock.NewController(tb)
	mclock := activation.NewMocklayerClock(ctrl)
	mfetch := smocks.NewMockFetcher(ctrl)
	mvalidator := activation.NewMocknipostValidator(ctrl)
	mreceiver := activation.NewMockAtxReceiver(ctrl)
	mtrtl := smocks.NewMockTortoise(ctrl)
	cdb := datastore.NewCachedDB(db, lg)
	atxHandler := activation.NewHandler(
		cdb,
		edVerifier,
		mclock,
		nil,
		mfetch,
		layersPerEpoch,
		10,
		goldenAtx,
		mvalidator,
		mreceiver,
		mtrtl,
		lg,
		activation.PoetConfig{},
	)
	mfetch.EXPECT().GetAtxs(gomock.Any(), gomock.Any()).AnyTimes()
	for i, vatx := range deps {
		encoded, err := codec.Encode(vatx)
		require.NoError(tb, err)
		mclock.EXPECT().LayerToTime(gomock.Any()).Return(time.Now())
		mclock.EXPECT().CurrentLayer().Return(vatx.PublishEpoch.FirstLayer())
		mfetch.EXPECT().RegisterPeerHashes(gomock.Any(), gomock.Any())
		mfetch.EXPECT().GetPoetProof(gomock.Any(), vatx.GetPoetProofRef())
		if vatx.InitialPost != nil {
			mvalidator.EXPECT().InitialNIPostChallenge(&vatx.ActivationTx.NIPostChallenge, gomock.Any(), goldenAtx).AnyTimes()
			mvalidator.EXPECT().Post(gomock.Any(), vatx.SmesherID, *vatx.CommitmentATX, vatx.InitialPost, gomock.Any(), vatx.NumUnits)
			mvalidator.EXPECT().VRFNonce(vatx.SmesherID, *vatx.CommitmentATX, vatx.VRFNonce, gomock.Any(), vatx.NumUnits)
		} else {
			mvalidator.EXPECT().NIPostChallenge(&vatx.ActivationTx.NIPostChallenge, cdb, vatx.SmesherID)
		}
		mvalidator.EXPECT().PositioningAtx(&vatx.PositioningATX, cdb, goldenAtx, vatx.PublishEpoch, layersPerEpoch)
		mvalidator.EXPECT().NIPost(gomock.Any(), vatx.SmesherID, gomock.Any(), vatx.NIPost, gomock.Any(), vatx.NumUnits)
		mreceiver.EXPECT().OnAtx(gomock.Any())
		mtrtl.EXPECT().OnAtx(gomock.Any())
		require.NoError(tb, atxHandler.HandleAtxData(context.Background(), "self", encoded))
		err = poetDb.ValidateAndStore(context.Background(), proofs[i])
		require.ErrorContains(tb, err, "failed to validate poet proof for poetID 706f65745f round 1337")
	}
}

func newChainedAtx(tb testing.TB, prev, pos types.ATXID, commitAtx *types.ATXID, epoch uint32, seq, vrfnonce uint64, sig *signing.EdSigner) *types.VerifiedActivationTx {
	atx := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch:   types.EpochID(epoch),
				Sequence:       seq,
				PrevATXID:      prev,
				PositioningATX: pos,
				CommitmentATX:  commitAtx,
			},
			NIPost: &types.NIPost{
				PostMetadata: &types.PostMetadata{
					Challenge: types.RandomBytes(5),
				},
			},
			NumUnits: 2,
			Coinbase: types.Address{1, 2, 3},
		},
	}
	if prev == types.EmptyATXID {
		atx.InitialPost = &types.Post{}
		nodeID := sig.NodeID()
		atx.NodeID = &nodeID
	}
	if vrfnonce != 0 {
		atx.VRFNonce = (*types.VRFPostIndex)(&vrfnonce)
	}
	atx.SmesherID = sig.NodeID()
	atx.SetEffectiveNumUnits(atx.NumUnits)
	atx.SetReceived(time.Now().Local())
	atx.Signature = sig.Sign(signing.ATX, atx.SignedBytes())
	return newvatx(tb, atx)
}

func createAtxChain(tb testing.TB, sig *signing.EdSigner) ([]*types.VerifiedActivationTx, []*types.PoetProofMessage) {
	other, err := signing.NewEdSigner()
	require.NoError(tb, err)
	// epoch 2
	othAtx1 := newChainedAtx(tb, types.EmptyATXID, goldenAtx, &goldenAtx, 2, 0, 113, other)
	// epoch 3
	othAtx2 := newChainedAtx(tb, othAtx1.ID(), othAtx1.ID(), nil, 3, 1, 0, other)
	// epoch 4
	othAtx3 := newChainedAtx(tb, othAtx2.ID(), othAtx2.ID(), nil, 4, 2, 0, other)
	commitAtxID := othAtx2.ID()
	atx1 := newChainedAtx(tb, types.EmptyATXID, othAtx2.ID(), &commitAtxID, 4, 0, 513, sig)
	// epoch 5
	othAtx4 := newChainedAtx(tb, othAtx3.ID(), atx1.ID(), nil, 5, 3, 0, other)
	// epoch 6
	othAtx5 := newChainedAtx(tb, othAtx4.ID(), othAtx4.ID(), nil, 6, 4, 0, other)
	atx2 := newChainedAtx(tb, atx1.ID(), othAtx4.ID(), nil, 6, 1, 0, sig)
	// epoch 7
	othAtx6 := newChainedAtx(tb, othAtx5.ID(), atx2.ID(), nil, 7, 5, 0, other)
	// epoch 8
	atx3 := newChainedAtx(tb, atx2.ID(), othAtx6.ID(), nil, 8, 2, 0, sig)
	// epoch 9
	othAtx7 := newChainedAtx(tb, othAtx6.ID(), atx3.ID(), nil, 9, 6, 0, other)

	vatxs := []*types.VerifiedActivationTx{othAtx1, othAtx2, othAtx3, atx1, othAtx4, othAtx5, atx2, othAtx6, atx3, othAtx7}
	var proofs []*types.PoetProofMessage
	for range vatxs {
		proofMessage := &types.PoetProofMessage{
			PoetProof: types.PoetProof{
				MerkleProof: shared.MerkleProof{
					Root:         []byte{1, 2, 3},
					ProvenLeaves: [][]byte{{1}, {2}},
					ProofNodes:   [][]byte{{1}, {2}},
				},
				LeafCount: 1234,
			},
			PoetServiceID: []byte("poet_id_123456"),
			RoundID:       "1337",
		}
		proofs = append(proofs, proofMessage)
	}
	return vatxs, proofs
}

func createAtxChainDepsOnly(tb testing.TB) ([]*types.VerifiedActivationTx, []*types.PoetProofMessage) {
	other, err := signing.NewEdSigner()
	require.NoError(tb, err)
	// epoch 2
	othAtx1 := newChainedAtx(tb, types.EmptyATXID, goldenAtx, &goldenAtx, 2, 0, 113, other)
	// epoch 3
	othAtx2 := newChainedAtx(tb, othAtx1.ID(), othAtx1.ID(), nil, 3, 1, 0, other)
	// epoch 4
	othAtx3 := newChainedAtx(tb, othAtx2.ID(), othAtx2.ID(), nil, 4, 2, 0, other)
	vatxs := []*types.VerifiedActivationTx{othAtx1, othAtx2, othAtx3}
	var proofs []*types.PoetProofMessage
	for range vatxs {
		proofMessage := &types.PoetProofMessage{
			PoetProof: types.PoetProof{
				MerkleProof: shared.MerkleProof{
					Root:         []byte{1, 2, 3},
					ProvenLeaves: [][]byte{{1}, {2}},
					ProofNodes:   [][]byte{{1}, {2}},
				},
				LeafCount: 1234,
			},
			PoetServiceID: []byte("poet_id_123456"),
			RoundID:       "1337",
		}
		proofs = append(proofs, proofMessage)
	}
	return vatxs, proofs
}

func atxIDs(atxs []*types.VerifiedActivationTx) []types.ATXID {
	ids := make([]types.ATXID, 0, len(atxs))
	for _, atx := range atxs {
		ids = append(ids, atx.ID())
	}
	return ids
}

func TestRecover_OwnAtxNotInCheckpoint_Preserve(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(checkpointdata))
		require.NoError(t, err)
	}))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	cfg := &checkpoint.RecoverConfig{
		GoldenAtx:      goldenAtx,
		PostDataDir:    t.TempDir(),
		DataDir:        t.TempDir(),
		DbFile:         "test.sql",
		PreserveOwnAtx: true,
		NodeID:         sig.NodeID(),
		Uri:            fmt.Sprintf("%s/snapshot-15", ts.URL),
		Restore:        types.LayerID(recoverLayer),
	}

	olddb, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, olddb)

	vatxs, proofs := createAtxChain(t, sig)
	validateAndPreserveData(t, olddb, vatxs, proofs)
	// the proofs are not valid, but save them anyway for the purpose of testing
	for i, vatx := range vatxs {
		encoded, err := codec.Encode(proofs[i])
		require.NoError(t, err)
		require.NoError(t, poets.Add(olddb, types.PoetProofRef(vatx.GetPoetProofRef()), encoded, proofs[i].PoetServiceID, proofs[i].RoundID))
	}
	require.NoError(t, olddb.Close())

	preserve, err := checkpoint.Recover(ctx, logtest.New(t), afero.NewOsFs(), cfg)
	require.NoError(t, err)
	require.NotNil(t, preserve)
	// the two set of atxs have different received time. just compare IDs
	require.ElementsMatch(t, atxIDs(vatxs[:len(vatxs)-1]), atxIDs(preserve.Deps))
	require.ElementsMatch(t, proofs[:len(proofs)-1], preserve.Proofs)

	newdb, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, newdb)
	t.Cleanup(func() { require.NoError(t, newdb.Close()) })
	verifyDbContent(t, newdb)
	validateAndPreserveData(t, newdb, preserve.Deps, preserve.Proofs)
	// note that poet proofs are not saved to newdb due to verification errors

	restore, err := recovery.CheckpointInfo(newdb)
	require.NoError(t, err)
	require.EqualValues(t, recoverLayer, restore)

	// sqlite create .sql, .sql-shm and .sql-wal files.
	files, err := filepath.Glob(fmt.Sprintf("%s/backup.*/%s*", cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 1)
}

func TestRecover_OwnAtxNotInCheckpoint_Preserve_IncludePending(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(checkpointdata))
		require.NoError(t, err)
	}))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	cfg := &checkpoint.RecoverConfig{
		GoldenAtx:      goldenAtx,
		PostDataDir:    t.TempDir(),
		DataDir:        t.TempDir(),
		DbFile:         "test.sql",
		PreserveOwnAtx: true,
		NodeID:         sig.NodeID(),
		Uri:            fmt.Sprintf("%s/snapshot-15", ts.URL),
		Restore:        types.LayerID(recoverLayer),
	}

	olddb, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, olddb)

	vatxs, proofs := createAtxChain(t, sig)
	validateAndPreserveData(t, olddb, vatxs, proofs)
	// the proofs are not valid, but save them anyway for the purpose of testing
	for i, vatx := range vatxs {
		encoded, err := codec.Encode(proofs[i])
		require.NoError(t, err)
		require.NoError(t, poets.Add(olddb, types.PoetProofRef(vatx.GetPoetProofRef()), encoded, proofs[i].PoetServiceID, proofs[i].RoundID))
	}
	require.NoError(t, olddb.Close())

	// write pending nipost challenge to simulate a pending atx still waiting for poet proof.
	prevAtx := vatxs[len(vatxs)-2]
	posAtx := vatxs[len(vatxs)-1]
	require.NoError(t, activation.SaveNipostChallenge(cfg.PostDataDir, &types.NIPostChallenge{
		PublishEpoch:   posAtx.PublishEpoch + 1,
		Sequence:       prevAtx.Sequence + 1,
		PrevATXID:      prevAtx.ID(),
		PositioningATX: posAtx.ID(),
	}))

	preserve, err := checkpoint.Recover(ctx, logtest.New(t), afero.NewOsFs(), cfg)
	require.NoError(t, err)
	require.NotNil(t, preserve)
	// the two set of atxs have different received time. just compare IDs
	require.ElementsMatch(t, atxIDs(vatxs), atxIDs(preserve.Deps))
	require.ElementsMatch(t, proofs, preserve.Proofs)

	newdb, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, newdb)
	t.Cleanup(func() { require.NoError(t, newdb.Close()) })
	verifyDbContent(t, newdb)
	validateAndPreserveData(t, newdb, preserve.Deps, preserve.Proofs)
	// note that poet proofs are not saved to newdb due to verification errors

	restore, err := recovery.CheckpointInfo(newdb)
	require.NoError(t, err)
	require.EqualValues(t, recoverLayer, restore)

	// sqlite create .sql, .sql-shm and .sql-wal files.
	files, err := filepath.Glob(fmt.Sprintf("%s/backup.*/%s*", cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 1)
}

func TestRecover_OwnAtxNotInCheckpoint_Preserve_Still_Initializing(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(checkpointdata))
		require.NoError(t, err)
	}))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	cfg := &checkpoint.RecoverConfig{
		GoldenAtx:      goldenAtx,
		PostDataDir:    t.TempDir(),
		DataDir:        t.TempDir(),
		DbFile:         "test.sql",
		PreserveOwnAtx: true,
		NodeID:         sig.NodeID(),
		Uri:            fmt.Sprintf("%s/snapshot-15", ts.URL),
		Restore:        types.LayerID(recoverLayer),
	}

	olddb, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, olddb)

	vatxs, proofs := createAtxChainDepsOnly(t)
	validateAndPreserveData(t, olddb, vatxs, proofs)
	// the proofs are not valid, but save them anyway for the purpose of testing
	for i, vatx := range vatxs {
		encoded, err := codec.Encode(proofs[i])
		require.NoError(t, err)
		require.NoError(t, poets.Add(olddb, types.PoetProofRef(vatx.GetPoetProofRef()), encoded, proofs[i].PoetServiceID, proofs[i].RoundID))
	}

	commitment, err := atxs.GetIDWithMaxHeight(olddb, types.EmptyNodeID)
	require.NoError(t, err)
	require.NoError(t, olddb.Close())

	// create post metadata to indicate that miner is still initializing
	require.Equal(t, commitment, vatxs[len(vatxs)-1].ID())
	require.NoError(t, initialization.SaveMetadata(cfg.PostDataDir, &postShared.PostMetadata{
		NodeId:          sig.NodeID().Bytes(),
		CommitmentAtxId: commitment.Bytes(),
	}))

	preserve, err := checkpoint.Recover(ctx, logtest.New(t), afero.NewOsFs(), cfg)
	require.NoError(t, err)
	require.NotNil(t, preserve)
	// the two set of atxs have different received time. just compare IDs
	require.ElementsMatch(t, atxIDs(vatxs), atxIDs(preserve.Deps))
	require.ElementsMatch(t, proofs, preserve.Proofs)

	newdb, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, newdb)
	t.Cleanup(func() { require.NoError(t, newdb.Close()) })
	verifyDbContent(t, newdb)
	validateAndPreserveData(t, newdb, preserve.Deps, preserve.Proofs)
	// note that poet proofs are not saved to newdb due to verification errors

	restore, err := recovery.CheckpointInfo(newdb)
	require.NoError(t, err)
	require.EqualValues(t, recoverLayer, restore)

	// sqlite create .sql, .sql-shm and .sql-wal files.
	files, err := filepath.Glob(fmt.Sprintf("%s/backup.*/%s*", cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 1)
}

func TestRecover_OwnAtxNotInCheckpoint_Preserve_DepIsGolden(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(checkpointdata))
		require.NoError(t, err)
	}))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	cfg := &checkpoint.RecoverConfig{
		GoldenAtx:      goldenAtx,
		PostDataDir:    t.TempDir(),
		DataDir:        t.TempDir(),
		DbFile:         "test.sql",
		PreserveOwnAtx: true,
		NodeID:         sig.NodeID(),
		Uri:            fmt.Sprintf("%s/snapshot-15", ts.URL),
		Restore:        types.LayerID(recoverLayer),
	}

	olddb, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, olddb)
	vatxs, proofs := createAtxChain(t, sig)
	// make the first one from the previous snapshot
	golden := vatxs[0]
	require.NoError(t, atxs.AddCheckpointed(olddb, &atxs.CheckpointAtx{
		ID:             golden.ID(),
		Epoch:          golden.PublishEpoch,
		CommitmentATX:  *golden.CommitmentATX,
		VRFNonce:       *golden.VRFNonce,
		NumUnits:       golden.NumUnits,
		BaseTickHeight: golden.BaseTickHeight(),
		TickCount:      golden.TickCount(),
		SmesherID:      golden.SmesherID,
		Sequence:       golden.Sequence,
		Coinbase:       golden.Coinbase,
	}))
	validateAndPreserveData(t, olddb, vatxs[1:], proofs[1:])
	// the proofs are not valid, but save them anyway for the purpose of testing
	for i, proof := range proofs {
		if i == 0 {
			continue
		}
		encoded, err := codec.Encode(proof)
		require.NoError(t, err)
		require.NoError(t, poets.Add(olddb, types.PoetProofRef(vatxs[i].GetPoetProofRef()), encoded, proof.PoetServiceID, proof.RoundID))
	}
	require.NoError(t, olddb.Close())

	preserve, err := checkpoint.Recover(ctx, logtest.New(t), afero.NewOsFs(), cfg)
	require.NoError(t, err)
	require.Nil(t, preserve)

	newdb, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, newdb)
	t.Cleanup(func() { require.NoError(t, newdb.Close()) })
	verifyDbContent(t, newdb)
	restore, err := recovery.CheckpointInfo(newdb)
	require.NoError(t, err)
	require.EqualValues(t, recoverLayer, restore)

	// sqlite create .sql, .sql-shm and .sql-wal files.
	files, err := filepath.Glob(fmt.Sprintf("%s/backup.*/%s*", cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 1)
}

func TestRecover_OwnAtxNotInCheckpoint_DontPreserve(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(checkpointdata))
		require.NoError(t, err)
	}))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	cfg := &checkpoint.RecoverConfig{
		GoldenAtx:      goldenAtx,
		PostDataDir:    t.TempDir(),
		DataDir:        t.TempDir(),
		DbFile:         "test.sql",
		PreserveOwnAtx: false,
		NodeID:         sig.NodeID(),
		Uri:            fmt.Sprintf("%s/snapshot-15", ts.URL),
		Restore:        types.LayerID(recoverLayer),
	}

	olddb, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, olddb)
	vatxs, proofs := createAtxChain(t, sig)
	validateAndPreserveData(t, olddb, vatxs, proofs)
	// the proofs are not valid, but save them anyway for the purpose of testing
	for i, vatx := range vatxs {
		encoded, err := codec.Encode(proofs[i])
		require.NoError(t, err)
		require.NoError(t, poets.Add(olddb, types.PoetProofRef(vatx.GetPoetProofRef()), encoded, proofs[i].PoetServiceID, proofs[i].RoundID))
	}
	require.NoError(t, olddb.Close())

	preserve, err := checkpoint.Recover(ctx, logtest.New(t), afero.NewOsFs(), cfg)
	require.NoError(t, err)
	require.Nil(t, preserve)

	newdb, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, newdb)
	t.Cleanup(func() { require.NoError(t, newdb.Close()) })
	verifyDbContent(t, newdb)
	restore, err := recovery.CheckpointInfo(newdb)
	require.NoError(t, err)
	require.EqualValues(t, recoverLayer, restore)

	// sqlite create .sql, .sql-shm and .sql-wal files.
	files, err := filepath.Glob(fmt.Sprintf("%s/backup.*/%s*", cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 1)
}

func TestRecover_OwnAtxInCheckpoint(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(checkpointdata))
		require.NoError(t, err)
	}))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	data, err := hex.DecodeString("0230c5d75d42b84f98800eceb47bc9cc4d803058900a50346a09ff61d56b6582")
	require.NoError(t, err)
	nid := types.BytesToNodeID(data)
	data, err = hex.DecodeString("98e47278c1f58acfd2b670a730f28898f74eb140482a07b91ff81f9ff0b7d9f4")
	require.NoError(t, err)
	atx := newatx(types.ATXID(types.BytesToHash(data)), nil, 3, 1, 0, nid)

	cfg := &checkpoint.RecoverConfig{
		GoldenAtx:      goldenAtx,
		PostDataDir:    t.TempDir(),
		DataDir:        t.TempDir(),
		DbFile:         "test.sql",
		PreserveOwnAtx: true,
		NodeID:         nid,
		Uri:            fmt.Sprintf("%s/snapshot-15", ts.URL),
		Restore:        types.LayerID(recoverLayer),
	}

	olddb, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, olddb)
	require.NoError(t, atxs.Add(olddb, newvatx(t, atx)))
	require.NoError(t, olddb.Close())

	preserve, err := checkpoint.Recover(ctx, logtest.New(t), afero.NewOsFs(), cfg)
	require.NoError(t, err)
	require.Nil(t, preserve)

	newdb, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, newdb)
	t.Cleanup(func() { require.NoError(t, newdb.Close()) })
	verifyDbContent(t, newdb)
	restore, err := recovery.CheckpointInfo(newdb)
	require.NoError(t, err)
	require.EqualValues(t, recoverLayer, restore)

	// sqlite create .sql and optionally .sql-shm and .sql-wal files.
	files, err := filepath.Glob(fmt.Sprintf("%s/backup.*/%s*", cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 1)
}
