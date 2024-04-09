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

	"github.com/spacemeshos/poet/shared"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/atxsdata"
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
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
	"github.com/spacemeshos/go-spacemesh/sql/poets"
	"github.com/spacemeshos/go-spacemesh/sql/recovery"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

const recoverLayer uint32 = 18

var goldenAtx = types.ATXID{1}

func atxEqual(
	tb testing.TB,
	sAtx types.AtxSnapshot,
	vAtx *types.VerifiedActivationTx,
	commitAtx types.ATXID,
	vrfnonce types.VRFPostIndex,
) {
	require.True(tb, bytes.Equal(sAtx.ID, vAtx.ID().Bytes()))
	require.EqualValues(tb, sAtx.Epoch, vAtx.PublishEpoch)
	require.True(tb, bytes.Equal(sAtx.CommitmentAtx, commitAtx.Bytes()))
	require.EqualValues(tb, sAtx.VrfNonce, vrfnonce)
	require.Equal(tb, sAtx.NumUnits, vAtx.NumUnits)
	require.Equal(tb, sAtx.BaseTickHeight, vAtx.BaseTickHeight())
	require.Equal(tb, sAtx.TickCount, vAtx.TickCount())
	require.True(tb, bytes.Equal(sAtx.PublicKey, vAtx.SmesherID.Bytes()))
	require.Equal(tb, sAtx.Sequence, vAtx.Sequence)
	require.True(tb, bytes.Equal(sAtx.Coinbase, vAtx.Coinbase.Bytes()))
	require.True(tb, vAtx.Golden())
}

func accountEqual(tb testing.TB, cacct types.AccountSnapshot, acct *types.Account) {
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
	require.NoError(tb, json.Unmarshal([]byte(checkpointData), &expected))
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
		commitAtx, err := atxs.CommitmentATX(db, vatx.SmesherID)
		require.NoError(tb, err)
		vrfNonce, err := atxs.VRFNonce(db, vatx.SmesherID, vatx.PublishEpoch+1)
		require.NoError(tb, err)
		if _, ok := expAtx[id]; ok {
			atxEqual(tb, expAtx[id], vatx, commitAtx, vrfNonce)
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
		accountEqual(tb, cacct, acct)
		require.EqualValues(tb, recoverLayer-1, acct.Layer)
	}
	require.Empty(tb, extra)
}

func TestRecover(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(checkpointData))
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
				DataDir:        t.TempDir(),
				DbFile:         "test.sql",
				LocalDbFile:    "local.sql",
				PreserveOwnAtx: true,
				NodeIDs:        []types.NodeID{types.RandomNodeID()},
				Uri:            tc.uri,
				Restore:        types.LayerID(recoverLayer),
			}
			bsdir := filepath.Join(cfg.DataDir, bootstrap.DirName)
			require.NoError(t, fs.MkdirAll(bsdir, 0o700))
			db := sql.InMemory()
			localDB := localsql.InMemory()
			preserve, err := checkpoint.RecoverWithDb(ctx, logtest.New(t), db, localDB, fs, cfg)
			if tc.expErr != nil {
				require.ErrorIs(t, err, tc.expErr)
				return
			}
			require.NoError(t, err)
			require.Nil(t, preserve)
			newDB, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
			require.NoError(t, err)
			require.NotNil(t, newDB)
			defer newDB.Close()
			verifyDbContent(t, newDB)
			restore, err := recovery.CheckpointInfo(newDB)
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
		_, err := w.Write([]byte(checkpointData))
		require.NoError(t, err)
	}))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fs := afero.NewMemMapFs()
	cfg := &checkpoint.RecoverConfig{
		GoldenAtx:      goldenAtx,
		DataDir:        t.TempDir(),
		DbFile:         "test.sql",
		PreserveOwnAtx: true,
		NodeIDs:        []types.NodeID{types.RandomNodeID()},
		Uri:            fmt.Sprintf("%s/snapshot-15", ts.URL),
		Restore:        types.LayerID(recoverLayer),
	}
	bsdir := filepath.Join(cfg.DataDir, bootstrap.DirName)
	require.NoError(t, fs.MkdirAll(bsdir, 0o700))
	db := sql.InMemory()
	localDB := localsql.InMemory()
	types.SetEffectiveGenesis(0)
	require.NoError(t, recovery.SetCheckpoint(db, types.LayerID(recoverLayer)))
	preserve, err := checkpoint.RecoverWithDb(ctx, logtest.New(t), db, localDB, fs, cfg)
	require.NoError(t, err)
	require.Nil(t, preserve)
	require.EqualValues(t, recoverLayer-1, types.GetEffectiveGenesis())
	exist, err := afero.Exists(fs, bsdir)
	require.NoError(t, err)
	require.True(t, exist)
}

func validateAndPreserveData(
	tb testing.TB,
	db *sql.Database,
	deps []*types.VerifiedActivationTx,
	proofs []*types.PoetProofMessage,
) {
	lg := logtest.New(tb)
	poetDb := activation.NewPoetDb(db, lg)
	ctrl := gomock.NewController(tb)
	mclock := activation.NewMocklayerClock(ctrl)
	mfetch := smocks.NewMockFetcher(ctrl)
	mvalidator := activation.NewMocknipostValidator(ctrl)
	mreceiver := activation.NewMockAtxReceiver(ctrl)
	mtrtl := smocks.NewMockTortoise(ctrl)
	cdb := datastore.NewCachedDB(db, lg)
	atxHandler := activation.NewHandler(
		"",
		cdb,
		atxsdata.New(),
		signing.NewEdVerifier(),
		mclock,
		nil,
		mfetch,
		10,
		goldenAtx,
		mvalidator,
		mreceiver,
		mtrtl,
		lg,
	)
	mfetch.EXPECT().GetAtxs(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	for i, vatx := range deps {
		encoded, err := codec.Encode(wire.ActivationTxToWireV1(vatx.ActivationTx))
		require.NoError(tb, err)
		mclock.EXPECT().CurrentLayer().Return(vatx.PublishEpoch.FirstLayer())
		mfetch.EXPECT().RegisterPeerHashes(gomock.Any(), gomock.Any())
		mfetch.EXPECT().GetPoetProof(gomock.Any(), vatx.GetPoetProofRef())
		if vatx.InitialPost != nil {
			mvalidator.EXPECT().
				InitialNIPostChallenge(&vatx.ActivationTx.NIPostChallenge, gomock.Any(), goldenAtx).
				AnyTimes()
			mvalidator.EXPECT().Post(
				gomock.Any(),
				vatx.SmesherID,
				*vatx.CommitmentATX,
				vatx.InitialPost,
				gomock.Any(),
				vatx.NumUnits,
				gomock.Any(),
			)
			mvalidator.EXPECT().
				VRFNonce(vatx.SmesherID, *vatx.CommitmentATX, vatx.VRFNonce, gomock.Any(), vatx.NumUnits)
		} else {
			mvalidator.EXPECT().NIPostChallenge(&vatx.ActivationTx.NIPostChallenge, cdb, vatx.SmesherID)
		}

		mvalidator.EXPECT().PositioningAtx(vatx.PositioningATX, cdb, goldenAtx, vatx.PublishEpoch)
		mvalidator.EXPECT().
			NIPost(gomock.Any(), vatx.SmesherID, gomock.Any(), vatx.NIPost, gomock.Any(), vatx.NumUnits, gomock.Any()).
			Return(uint64(1111111), nil)
		mvalidator.EXPECT().IsVerifyingFullPost().AnyTimes().Return(true)
		mreceiver.EXPECT().OnAtx(gomock.Any())
		mtrtl.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
		require.NoError(tb, atxHandler.HandleSyncedAtx(context.Background(), vatx.ID().Hash32(), "self", encoded))
		err = poetDb.ValidateAndStore(context.Background(), proofs[i])
		require.ErrorContains(tb, err, fmt.Sprintf("failed to validate poet proof for poetID %s round 1337",
			hex.EncodeToString(proofs[i].PoetServiceID[:5])),
		)
	}
}

func newChainedAtx(
	tb testing.TB,
	prev, pos types.ATXID,
	commitAtx *types.ATXID,
	epoch uint32,
	seq, vrfNonce uint64,
	sig *signing.EdSigner,
) *types.VerifiedActivationTx {
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
	if vrfNonce != 0 {
		atx.VRFNonce = (*types.VRFPostIndex)(&vrfNonce)
	}
	atx.SmesherID = sig.NodeID()
	atx.SetEffectiveNumUnits(atx.NumUnits)
	atx.SetReceived(time.Now().Local())
	atx.Signature = sig.Sign(signing.ATX, wire.ActivationTxToWireV1(atx).SignedBytes())
	return newvAtx(tb, atx)
}

func createInterlinkedAtxChain(
	tb testing.TB,
	sig1 *signing.EdSigner,
	sig2 *signing.EdSigner,
) ([]*types.VerifiedActivationTx, []*types.PoetProofMessage) {
	// epoch 2
	sig1Atx1 := newChainedAtx(tb, types.EmptyATXID, goldenAtx, &goldenAtx, 2, 0, 113, sig1)
	// epoch 3
	sig1Atx2 := newChainedAtx(tb, sig1Atx1.ID(), sig1Atx1.ID(), nil, 3, 1, 0, sig1)
	// epoch 4
	sig1Atx3 := newChainedAtx(tb, sig1Atx2.ID(), sig1Atx2.ID(), nil, 4, 2, 0, sig1)
	commitAtxID := sig1Atx2.ID()
	sig2Atx1 := newChainedAtx(tb, types.EmptyATXID, sig1Atx2.ID(), &commitAtxID, 4, 0, 513, sig2)
	// epoch 5
	sig1Atx4 := newChainedAtx(tb, sig1Atx3.ID(), sig2Atx1.ID(), nil, 5, 3, 0, sig1)
	// epoch 6
	sig1Atx5 := newChainedAtx(tb, sig1Atx4.ID(), sig1Atx4.ID(), nil, 6, 4, 0, sig1)
	sig2Atx2 := newChainedAtx(tb, sig2Atx1.ID(), sig1Atx4.ID(), nil, 6, 1, 0, sig2)
	// epoch 7
	sig1Atx6 := newChainedAtx(tb, sig1Atx5.ID(), sig2Atx2.ID(), nil, 7, 5, 0, sig1)
	// epoch 8
	sig2Atx3 := newChainedAtx(tb, sig2Atx2.ID(), sig1Atx6.ID(), nil, 8, 2, 0, sig2)
	// epoch 9
	sig1Atx7 := newChainedAtx(tb, sig1Atx6.ID(), sig2Atx3.ID(), nil, 9, 6, 0, sig1)

	vAtxs := []*types.VerifiedActivationTx{
		sig1Atx1,
		sig1Atx2,
		sig1Atx3,
		sig2Atx1,
		sig1Atx4,
		sig1Atx5,
		sig2Atx2,
		sig1Atx6,
		sig2Atx3,
		sig1Atx7,
	}
	var proofs []*types.PoetProofMessage
	for range vAtxs {
		proofMessage := &types.PoetProofMessage{
			PoetProof: types.PoetProof{
				MerkleProof: shared.MerkleProof{
					Root:         types.RandomBytes(32),
					ProvenLeaves: [][]byte{{1}, {2}},
					ProofNodes:   [][]byte{{1}, {2}},
				},
				LeafCount: 1234,
			},
			PoetServiceID: types.RandomBytes(32),
			RoundID:       "1337",
		}
		proofs = append(proofs, proofMessage)
	}
	return vAtxs, proofs
}

func createAtxChain(tb testing.TB, sig *signing.EdSigner) ([]*types.VerifiedActivationTx, []*types.PoetProofMessage) {
	other, err := signing.NewEdSigner()
	require.NoError(tb, err)
	return createInterlinkedAtxChain(tb, other, sig)
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
	vAtxs := []*types.VerifiedActivationTx{othAtx1, othAtx2, othAtx3}
	var proofs []*types.PoetProofMessage
	for range vAtxs {
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
	return vAtxs, proofs
}

func atxIDs(atxs []*types.VerifiedActivationTx) []types.ATXID {
	ids := make([]types.ATXID, 0, len(atxs))
	for _, atx := range atxs {
		ids = append(ids, atx.ID())
	}
	return ids
}

func proofRefs(proofs []*types.PoetProofMessage) []types.PoetProofRef {
	refs := make([]types.PoetProofRef, 0, len(proofs))
	for _, proof := range proofs {
		ref, _ := proof.Ref()
		refs = append(refs, ref)
	}
	return refs
}

func TestRecover_OwnAtxNotInCheckpoint_Preserve(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(checkpointData))
		require.NoError(t, err)
	}))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig3, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig4, err := signing.NewEdSigner()
	require.NoError(t, err)

	cfg := &checkpoint.RecoverConfig{
		GoldenAtx:      goldenAtx,
		DataDir:        t.TempDir(),
		DbFile:         "test.sql",
		LocalDbFile:    "local.sql",
		PreserveOwnAtx: true,
		NodeIDs:        []types.NodeID{sig1.NodeID(), sig2.NodeID(), sig3.NodeID(), sig4.NodeID()},
		Uri:            fmt.Sprintf("%s/snapshot-15", ts.URL),
		Restore:        types.LayerID(recoverLayer),
	}

	oldDB, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, oldDB)

	vAtxs1, proofs1 := createAtxChain(t, sig1)
	vAtxs2, proofs2 := createAtxChain(t, sig2)
	vAtxs := append(vAtxs1, vAtxs2...)
	proofs := append(proofs1, proofs2...)
	vAtxs3, proofs3 := createInterlinkedAtxChain(t, sig3, sig4)
	vAtxs = append(vAtxs, vAtxs3...)
	proofs = append(proofs, proofs3...)
	validateAndPreserveData(t, oldDB, vAtxs, proofs)
	// the proofs are not valid, but save them anyway for the purpose of testing
	for i, vatx := range vAtxs {
		encoded, err := codec.Encode(proofs[i])
		require.NoError(t, err)

		err = poets.Add(
			oldDB,
			types.PoetProofRef(vatx.GetPoetProofRef()),
			encoded,
			proofs[i].PoetServiceID,
			proofs[i].RoundID,
		)
		require.NoError(t, err)
	}
	require.NoError(t, oldDB.Close())

	preserve, err := checkpoint.Recover(ctx, logtest.New(t), afero.NewOsFs(), cfg)
	require.NoError(t, err)
	require.NotNil(t, preserve)

	// the last atx of single chains is not included in the checkpoint because it is not part of sig1 and sig2's
	// atx chains. atxs have different timestamps for received time, so we compare just the IDs
	atxRef := atxIDs(append(vAtxs1[:len(vAtxs1)-1], vAtxs2[:len(vAtxs2)-1]...))
	atxRef = append(atxRef, atxIDs(vAtxs3)...)
	proofRef := proofRefs(append(proofs1[:len(vAtxs1)-1], proofs2[:len(vAtxs2)-1]...))
	proofRef = append(proofRef, proofRefs(proofs3)...)
	require.ElementsMatch(t, atxRef, atxIDs(preserve.Deps))
	require.ElementsMatch(t, proofRef, proofRefs(preserve.Proofs))

	newDB, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, newDB)
	t.Cleanup(func() { assert.NoError(t, newDB.Close()) })
	verifyDbContent(t, newDB)
	validateAndPreserveData(t, newDB, preserve.Deps, preserve.Proofs)
	// note that poet proofs are not saved to newDB due to verification errors

	restore, err := recovery.CheckpointInfo(newDB)
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
		_, err := w.Write([]byte(checkpointData))
		require.NoError(t, err)
	}))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig3, err := signing.NewEdSigner()
	require.NoError(t, err)

	cfg := &checkpoint.RecoverConfig{
		GoldenAtx:      goldenAtx,
		DataDir:        t.TempDir(),
		DbFile:         "test.sql",
		LocalDbFile:    "local.sql",
		PreserveOwnAtx: true,
		NodeIDs:        []types.NodeID{sig1.NodeID(), sig2.NodeID(), sig3.NodeID()},
		Uri:            fmt.Sprintf("%s/snapshot-15", ts.URL),
		Restore:        types.LayerID(recoverLayer),
	}

	oldDB, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, oldDB)

	vAtxs1, proofs1 := createAtxChain(t, sig1)
	vAtxs2, proofs2 := createInterlinkedAtxChain(t, sig2, sig3)
	vAtxs := append(vAtxs1, vAtxs2...)
	proofs := append(proofs1, proofs2...)
	validateAndPreserveData(t, oldDB, vAtxs, proofs)
	// the proofs are not valid, but save them anyway for the purpose of testing
	for i, vatx := range vAtxs {
		encoded, err := codec.Encode(proofs[i])
		require.NoError(t, err)
		require.NoError(
			t,
			poets.Add(
				oldDB,
				types.PoetProofRef(vatx.GetPoetProofRef()),
				encoded,
				proofs[i].PoetServiceID,
				proofs[i].RoundID,
			),
		)
	}
	require.NoError(t, oldDB.Close())

	// write pending nipost challenge to simulate a pending atx still waiting for poet proof.
	prevAtx1 := vAtxs1[len(vAtxs1)-2]
	posAtx1 := vAtxs1[len(vAtxs1)-1]

	prevAtx2 := vAtxs2[len(vAtxs2)-2]
	posAtx2 := vAtxs2[len(vAtxs2)-1]

	localDB, err := localsql.Open("file:" + filepath.Join(cfg.DataDir, cfg.LocalDbFile))
	require.NoError(t, err)
	require.NotNil(t, localDB)

	err = nipost.AddChallenge(localDB, sig1.NodeID(), &types.NIPostChallenge{
		PublishEpoch:   posAtx1.PublishEpoch + 1,
		Sequence:       prevAtx1.Sequence + 1,
		PrevATXID:      prevAtx1.ID(),
		PositioningATX: posAtx1.ID(),
	})
	require.NoError(t, err)

	err = nipost.AddChallenge(localDB, sig2.NodeID(), &types.NIPostChallenge{
		PublishEpoch:   posAtx2.PublishEpoch + 1,
		Sequence:       prevAtx2.Sequence + 1,
		PrevATXID:      prevAtx2.ID(),
		PositioningATX: posAtx2.ID(),
	})
	require.NoError(t, err)
	require.NoError(t, localDB.Close())

	preserve, err := checkpoint.Recover(ctx, logtest.New(t), afero.NewOsFs(), cfg)
	require.NoError(t, err)
	require.NotNil(t, preserve)

	// the two set of atxs have different received time. just compare IDs
	atxRef := atxIDs(append(vAtxs1, vAtxs2...))
	proofRef := proofRefs(append(proofs1, proofs2...))
	require.ElementsMatch(t, atxRef, atxIDs(preserve.Deps))
	require.ElementsMatch(t, proofRef, proofRefs(preserve.Proofs))

	newDB, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, newDB)
	t.Cleanup(func() { assert.NoError(t, newDB.Close()) })
	verifyDbContent(t, newDB)
	validateAndPreserveData(t, newDB, preserve.Deps, preserve.Proofs)
	// note that poet proofs are not saved to newDB due to verification errors

	restore, err := recovery.CheckpointInfo(newDB)
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
		_, err := w.Write([]byte(checkpointData))
		require.NoError(t, err)
	}))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)

	cfg := &checkpoint.RecoverConfig{
		GoldenAtx:      goldenAtx,
		DataDir:        t.TempDir(),
		DbFile:         "test.sql",
		LocalDbFile:    "local.sql",
		PreserveOwnAtx: true,
		NodeIDs:        []types.NodeID{sig1.NodeID(), sig2.NodeID()},
		Uri:            fmt.Sprintf("%s/snapshot-15", ts.URL),
		Restore:        types.LayerID(recoverLayer),
	}

	oldDB, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, oldDB)

	vAtxs, proofs := createAtxChainDepsOnly(t)
	validateAndPreserveData(t, oldDB, vAtxs, proofs)
	// the proofs are not valid, but save them anyway for the purpose of testing
	for i, vatx := range vAtxs {
		encoded, err := codec.Encode(proofs[i])
		require.NoError(t, err)
		require.NoError(
			t,
			poets.Add(
				oldDB,
				types.PoetProofRef(vatx.GetPoetProofRef()),
				encoded,
				proofs[i].PoetServiceID,
				proofs[i].RoundID,
			),
		)
	}

	require.NoError(t, oldDB.Close())

	localDB, err := localsql.Open("file:" + filepath.Join(cfg.DataDir, cfg.LocalDbFile))
	require.NoError(t, err)
	require.NotNil(t, localDB)

	post := types.Post{
		Indices: []byte{1, 2, 3},
	}
	commitmentAtx := types.RandomATXID()
	err = nipost.AddChallenge(localDB, sig1.NodeID(), &types.NIPostChallenge{
		PublishEpoch:   0, // will be updated later
		Sequence:       0,
		PrevATXID:      types.EmptyATXID, // initial has no previous ATX
		PositioningATX: types.EmptyATXID, // will be updated later
		InitialPost:    &post,
		CommitmentATX:  &commitmentAtx,
	})
	require.NoError(t, err)

	err = nipost.AddChallenge(localDB, sig2.NodeID(), &types.NIPostChallenge{
		PublishEpoch:   0, // will be updated later
		Sequence:       0,
		PrevATXID:      types.EmptyATXID, // initial has no previous ATX
		PositioningATX: types.EmptyATXID, // will be updated later
		InitialPost:    &post,
		CommitmentATX:  &commitmentAtx,
	})
	require.NoError(t, err)
	require.NoError(t, localDB.Close())

	preserve, err := checkpoint.Recover(ctx, logtest.New(t), afero.NewOsFs(), cfg)
	require.NoError(t, err)
	require.Nil(t, preserve)

	newDB, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, newDB)
	t.Cleanup(func() { assert.NoError(t, newDB.Close()) })
	verifyDbContent(t, newDB)
	restore, err := recovery.CheckpointInfo(newDB)
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
		_, err := w.Write([]byte(checkpointData))
		require.NoError(t, err)
	}))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	cfg := &checkpoint.RecoverConfig{
		GoldenAtx:      goldenAtx,
		DataDir:        t.TempDir(),
		DbFile:         "test.sql",
		LocalDbFile:    "local.sql",
		PreserveOwnAtx: true,
		NodeIDs:        []types.NodeID{sig.NodeID()},
		Uri:            fmt.Sprintf("%s/snapshot-15", ts.URL),
		Restore:        types.LayerID(recoverLayer),
	}

	oldDB, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, oldDB)
	vAtxs, proofs := createAtxChain(t, sig)
	// make the first one from the previous snapshot
	golden := vAtxs[0]
	require.NoError(t, atxs.AddCheckpointed(oldDB, &atxs.CheckpointAtx{
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
	validateAndPreserveData(t, oldDB, vAtxs[1:], proofs[1:])
	// the proofs are not valid, but save them anyway for the purpose of testing
	for i, proof := range proofs {
		if i == 0 {
			continue
		}
		encoded, err := codec.Encode(proof)
		require.NoError(t, err)
		require.NoError(
			t,
			poets.Add(
				oldDB,
				types.PoetProofRef(vAtxs[i].GetPoetProofRef()),
				encoded,
				proof.PoetServiceID,
				proof.RoundID,
			),
		)
	}
	require.NoError(t, oldDB.Close())

	preserve, err := checkpoint.Recover(ctx, logtest.New(t), afero.NewOsFs(), cfg)
	require.NoError(t, err)
	require.Nil(t, preserve)

	newDB, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, newDB)
	t.Cleanup(func() { assert.NoError(t, newDB.Close()) })
	verifyDbContent(t, newDB)
	restore, err := recovery.CheckpointInfo(newDB)
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
		_, err := w.Write([]byte(checkpointData))
		require.NoError(t, err)
	}))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	cfg := &checkpoint.RecoverConfig{
		GoldenAtx:      goldenAtx,
		DataDir:        t.TempDir(),
		DbFile:         "test.sql",
		LocalDbFile:    "local.sql",
		PreserveOwnAtx: false,
		NodeIDs:        []types.NodeID{sig.NodeID()},
		Uri:            fmt.Sprintf("%s/snapshot-15", ts.URL),
		Restore:        types.LayerID(recoverLayer),
	}

	oldDB, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, oldDB)
	vAtxs, proofs := createAtxChain(t, sig)
	validateAndPreserveData(t, oldDB, vAtxs, proofs)
	// the proofs are not valid, but save them anyway for the purpose of testing
	for i, vatx := range vAtxs {
		encoded, err := codec.Encode(proofs[i])
		require.NoError(t, err)
		require.NoError(
			t,
			poets.Add(
				oldDB,
				types.PoetProofRef(vatx.GetPoetProofRef()),
				encoded,
				proofs[i].PoetServiceID,
				proofs[i].RoundID,
			),
		)
	}
	require.NoError(t, oldDB.Close())

	preserve, err := checkpoint.Recover(ctx, logtest.New(t), afero.NewOsFs(), cfg)
	require.NoError(t, err)
	require.Nil(t, preserve)

	newDB, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, newDB)
	t.Cleanup(func() { assert.NoError(t, newDB.Close()) })
	verifyDbContent(t, newDB)
	restore, err := recovery.CheckpointInfo(newDB)
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
		_, err := w.Write([]byte(checkpointData))
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
	atx := newAtx(types.ATXID(types.BytesToHash(data)), nil, 3, 1, 0, nid)

	cfg := &checkpoint.RecoverConfig{
		GoldenAtx:      goldenAtx,
		DataDir:        t.TempDir(),
		DbFile:         "test.sql",
		LocalDbFile:    "local.sql",
		PreserveOwnAtx: true,
		NodeIDs:        []types.NodeID{nid},
		Uri:            fmt.Sprintf("%s/snapshot-15", ts.URL),
		Restore:        types.LayerID(recoverLayer),
	}

	oldDB, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, oldDB)
	require.NoError(t, atxs.Add(oldDB, newvAtx(t, atx)))
	require.NoError(t, oldDB.Close())

	preserve, err := checkpoint.Recover(ctx, logtest.New(t), afero.NewOsFs(), cfg)
	require.NoError(t, err)
	require.Nil(t, preserve)

	newDB, err := sql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, newDB)
	t.Cleanup(func() { assert.NoError(t, newDB.Close()) })
	verifyDbContent(t, newDB)
	restore, err := recovery.CheckpointInfo(newDB)
	require.NoError(t, err)
	require.EqualValues(t, recoverLayer, restore)

	// sqlite create .sql and optionally .sql-shm and .sql-wal files.
	files, err := filepath.Glob(fmt.Sprintf("%s/backup.*/%s*", cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 1)
}
