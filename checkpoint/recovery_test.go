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
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/bootstrap"
	"github.com/spacemeshos/go-spacemesh/checkpoint"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
	"github.com/spacemeshos/go-spacemesh/sql/poets"
	"github.com/spacemeshos/go-spacemesh/sql/recovery"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

const recoverLayer uint32 = 18

var goldenAtx = types.ATXID{1}

func atxEqual(
	tb testing.TB,
	sAtx types.AtxSnapshot,
	vAtx *types.ActivationTx,
	commitAtx types.ATXID,
	vrfnonce types.VRFPostIndex,
) {
	tb.Helper()
	require.True(tb, bytes.Equal(sAtx.ID, vAtx.ID().Bytes()))
	require.EqualValues(tb, sAtx.Epoch, vAtx.PublishEpoch)
	require.True(tb, bytes.Equal(sAtx.CommitmentAtx, commitAtx.Bytes()))
	require.EqualValues(tb, sAtx.VrfNonce, vrfnonce)
	require.Equal(tb, sAtx.NumUnits, vAtx.NumUnits)
	require.Equal(tb, sAtx.BaseTickHeight, vAtx.BaseTickHeight)
	require.Equal(tb, sAtx.TickCount, vAtx.TickCount)
	require.True(tb, bytes.Equal(sAtx.PublicKey, vAtx.SmesherID.Bytes()))
	require.Equal(tb, sAtx.Sequence, vAtx.Sequence)
	require.True(tb, bytes.Equal(sAtx.Coinbase, vAtx.Coinbase.Bytes()))
	require.True(tb, vAtx.Golden())
}

func accountEqual(tb testing.TB, cacct types.AccountSnapshot, acct *types.Account) {
	tb.Helper()
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

func verifyDbContent(tb testing.TB, db sql.StateDatabase) {
	tb.Helper()
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
	var extra []*types.ActivationTx
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

func checkpointServer(t testing.TB) string {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /snapshot-15", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(checkpointData))
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts.URL
}

func TestRecover(t *testing.T) {
	t.Parallel()
	url := checkpointServer(t)

	tt := []struct {
		name   string
		uri    string
		expErr error
	}{
		{
			name: "http",
			uri:  fmt.Sprintf("%s/snapshot-15", url),
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
		t.Run(tc.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			cfg := &checkpoint.RecoverConfig{
				GoldenAtx:   goldenAtx,
				DataDir:     t.TempDir(),
				DbFile:      "test.sql",
				LocalDbFile: "local.sql",
				NodeIDs:     []types.NodeID{types.RandomNodeID()},
				Uri:         tc.uri,
				Restore:     types.LayerID(recoverLayer),
			}
			bsdir := filepath.Join(cfg.DataDir, bootstrap.DirName)
			require.NoError(t, fs.MkdirAll(bsdir, 0o700))
			db := statesql.InMemory()
			localDB := localsql.InMemory()
			data, err := checkpoint.RecoverWithDb(context.Background(), zaptest.NewLogger(t), db, localDB, fs, cfg)
			if tc.expErr != nil {
				require.ErrorIs(t, err, tc.expErr)
				return
			}
			require.NoError(t, err)
			require.Nil(t, data)
			newDB, err := statesql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
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
	t.Parallel()
	url := checkpointServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fs := afero.NewMemMapFs()
	cfg := &checkpoint.RecoverConfig{
		GoldenAtx: goldenAtx,
		DataDir:   t.TempDir(),
		DbFile:    "test.sql",
		NodeIDs:   []types.NodeID{types.RandomNodeID()},
		Uri:       fmt.Sprintf("%s/snapshot-15", url),
		Restore:   types.LayerID(recoverLayer),
	}
	bsdir := filepath.Join(cfg.DataDir, bootstrap.DirName)
	require.NoError(t, fs.MkdirAll(bsdir, 0o700))
	db := statesql.InMemory()
	localDB := localsql.InMemory()
	types.SetEffectiveGenesis(0)
	require.NoError(t, recovery.SetCheckpoint(db, types.LayerID(recoverLayer)))
	preserve, err := checkpoint.RecoverWithDb(ctx, zaptest.NewLogger(t), db, localDB, fs, cfg)
	require.NoError(t, err)
	require.Nil(t, preserve)
	require.EqualValues(t, recoverLayer-1, types.GetEffectiveGenesis())
	exist, err := afero.Exists(fs, bsdir)
	require.NoError(t, err)
	require.True(t, exist)
}

func TestRecover_URIMustBeSet(t *testing.T) {
	t.Parallel()
	cfg := &checkpoint.RecoverConfig{}
	d, err := checkpoint.Recover(context.Background(), zaptest.NewLogger(t), afero.NewMemMapFs(), cfg)
	require.ErrorContains(t, err, "uri not set")
	require.Nil(t, d)
}

func TestRecover_RestoreLayerCannotBeZero(t *testing.T) {
	t.Parallel()
	cfg := &checkpoint.RecoverConfig{
		Uri: "http://nowhere/snapshot-15",
	}
	_, err := checkpoint.Recover(context.Background(), zaptest.NewLogger(t), afero.NewMemMapFs(), cfg)
	require.ErrorContains(t, err, "restore layer not set")
}

func validateAndPreserveData(
	tb testing.TB,
	db sql.StateDatabase,
	deps []*checkpoint.AtxDep,
) {
	tb.Helper()
	lg := zaptest.NewLogger(tb)
	ctrl := gomock.NewController(tb)
	mclock := activation.NewMocklayerClock(ctrl)
	mfetch := smocks.NewMockFetcher(ctrl)
	mvalidator := activation.NewMocknipostValidator(ctrl)
	mreceiver := activation.NewMockatxReceiver(ctrl)
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
		goldenAtx,
		mvalidator,
		mreceiver,
		mtrtl,
		lg,
	)
	mfetch.EXPECT().GetAtxs(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	for _, dep := range deps {
		var atx wire.ActivationTxV1
		require.NoError(tb, codec.Decode(dep.Blob, &atx))
		mclock.EXPECT().CurrentLayer().Return(atx.PublishEpoch.FirstLayer())
		mfetch.EXPECT().RegisterPeerHashes(gomock.Any(), gomock.Any())
		mfetch.EXPECT().GetPoetProof(gomock.Any(), gomock.Any())
		if atx.PrevATXID == types.EmptyATXID {
			mvalidator.EXPECT().
				InitialNIPostChallengeV1(&atx.NIPostChallengeV1, gomock.Any(), goldenAtx).
				AnyTimes()
			mvalidator.EXPECT().Post(
				gomock.Any(),
				atx.SmesherID,
				*atx.CommitmentATXID,
				wire.PostFromWireV1(atx.InitialPost),
				gomock.Any(),
				atx.NumUnits,
				gomock.Any(),
			)
			mvalidator.EXPECT().VRFNonce(
				atx.SmesherID,
				*atx.CommitmentATXID,
				*atx.VRFNonce,
				atx.NIPost.PostMetadata.LabelsPerUnit,
				atx.NumUnits,
			)
		} else {
			mvalidator.EXPECT().NIPostChallengeV1(
				&atx.NIPostChallengeV1,
				gomock.Cond(func(prev *types.ActivationTx) bool {
					return prev.ID() == atx.PrevATXID
				}),
				atx.SmesherID,
			)
		}

		mvalidator.EXPECT().PositioningAtx(atx.PositioningATXID, cdb, goldenAtx, atx.PublishEpoch)
		mvalidator.EXPECT().
			NIPost(gomock.Any(), atx.SmesherID, gomock.Any(), gomock.Any(), gomock.Any(), atx.NumUnits, gomock.Any()).
			Return(uint64(1111111), nil)
		mvalidator.EXPECT().IsVerifyingFullPost().AnyTimes().Return(true)
		mreceiver.EXPECT().OnAtx(gomock.Any())
		mtrtl.EXPECT().OnAtx(gomock.Any(), gomock.Any(), gomock.Any())
		require.NoError(tb, atxHandler.HandleSyncedAtx(context.Background(), atx.ID().Hash32(), "self", dep.Blob))
	}
}

func newChainedAtx(
	prev, pos types.ATXID,
	commitAtx *types.ATXID,
	poetProofRef types.PoetProofRef,
	epoch uint32,
	seq, vrfNonce uint64,
	sig *signing.EdSigner,
) *checkpoint.AtxDep {
	watx := &wire.ActivationTxV1{
		InnerActivationTxV1: wire.InnerActivationTxV1{
			NIPostChallengeV1: wire.NIPostChallengeV1{
				PublishEpoch:     types.EpochID(epoch),
				Sequence:         seq,
				PrevATXID:        prev,
				PositioningATXID: pos,
				CommitmentATXID:  commitAtx,
			},
			NIPost: &wire.NIPostV1{
				PostMetadata: &wire.PostMetadataV1{
					Challenge: poetProofRef[:],
				},
			},
			NumUnits: 2,
			Coinbase: types.Address{1, 2, 3},
		},
		SmesherID: sig.NodeID(),
	}
	if prev == types.EmptyATXID {
		watx.InitialPost = &wire.PostV1{}
		nodeID := sig.NodeID()
		watx.NodeID = &nodeID
	}
	if vrfNonce != 0 {
		watx.VRFNonce = (*uint64)(&vrfNonce)
	}
	watx.Signature = sig.Sign(signing.ATX, watx.SignedBytes())

	return &checkpoint.AtxDep{
		ID:           watx.ID(),
		PublishEpoch: watx.PublishEpoch,
		Blob:         codec.MustEncode(watx),
	}
}

func randomPoetProof(tb testing.TB) (*types.PoetProofMessage, types.PoetProofRef) {
	tb.Helper()
	proof := &types.PoetProofMessage{
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
	ref, err := proof.Ref()
	require.NoError(tb, err)
	return proof, ref
}

func createInterlinkedAtxChain(
	tb testing.TB,
	sig1 *signing.EdSigner,
	sig2 *signing.EdSigner,
) ([]*checkpoint.AtxDep, []*types.PoetProofMessage) {
	var proofs []*types.PoetProofMessage
	poetRef := func() types.PoetProofRef {
		proof, ref := randomPoetProof(tb)
		proofs = append(proofs, proof)
		return ref
	}

	// epoch 2
	sig1Atx1 := newChainedAtx(types.EmptyATXID, goldenAtx, &goldenAtx, poetRef(), 2, 0, 113, sig1)
	// epoch 3
	sig1Atx2 := newChainedAtx(sig1Atx1.ID, sig1Atx1.ID, nil, poetRef(), 3, 1, 0, sig1)
	// epoch 4
	sig1Atx3 := newChainedAtx(sig1Atx2.ID, sig1Atx2.ID, nil, poetRef(), 4, 2, 0, sig1)
	commitAtxID := sig1Atx2.ID
	sig2Atx1 := newChainedAtx(types.EmptyATXID, sig1Atx2.ID, &commitAtxID, poetRef(), 4, 0, 513, sig2)
	// epoch 5
	sig1Atx4 := newChainedAtx(sig1Atx3.ID, sig2Atx1.ID, nil, poetRef(), 5, 3, 0, sig1)
	// epoch 6
	sig1Atx5 := newChainedAtx(sig1Atx4.ID, sig1Atx4.ID, nil, poetRef(), 6, 4, 0, sig1)
	sig2Atx2 := newChainedAtx(sig2Atx1.ID, sig1Atx4.ID, nil, poetRef(), 6, 1, 0, sig2)
	// epoch 7
	sig1Atx6 := newChainedAtx(sig1Atx5.ID, sig2Atx2.ID, nil, poetRef(), 7, 5, 0, sig1)
	// epoch 8
	sig2Atx3 := newChainedAtx(sig2Atx2.ID, sig1Atx6.ID, nil, poetRef(), 8, 2, 0, sig2)
	// epoch 9
	sig1Atx7 := newChainedAtx(sig1Atx6.ID, sig2Atx3.ID, nil, poetRef(), 9, 6, 0, sig1)

	vAtxs := []*checkpoint.AtxDep{
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

	return vAtxs, proofs
}

func createAtxChain(tb testing.TB, sig *signing.EdSigner) ([]*checkpoint.AtxDep, []*types.PoetProofMessage) {
	tb.Helper()
	other, err := signing.NewEdSigner()
	require.NoError(tb, err)
	return createInterlinkedAtxChain(tb, other, sig)
}

func createAtxChainDepsOnly(tb testing.TB) ([]*checkpoint.AtxDep, []*types.PoetProofMessage) {
	tb.Helper()
	other, err := signing.NewEdSigner()
	require.NoError(tb, err)

	var proofs []*types.PoetProofMessage
	poetRef := func() types.PoetProofRef {
		proof, ref := randomPoetProof(tb)
		proofs = append(proofs, proof)
		return ref
	}

	// epoch 2
	othAtx1 := newChainedAtx(types.EmptyATXID, goldenAtx, &goldenAtx, poetRef(), 2, 0, 113, other)
	// epoch 3
	othAtx2 := newChainedAtx(othAtx1.ID, othAtx1.ID, nil, poetRef(), 3, 1, 0, other)
	// epoch 4
	othAtx3 := newChainedAtx(othAtx2.ID, othAtx2.ID, nil, poetRef(), 4, 2, 0, other)
	atxDeps := []*checkpoint.AtxDep{othAtx1, othAtx2, othAtx3}

	return atxDeps, proofs
}

func atxIDs(atxs []*checkpoint.AtxDep) []types.ATXID {
	ids := make([]types.ATXID, 0, len(atxs))
	for _, atx := range atxs {
		ids = append(ids, atx.ID)
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
	t.Parallel()
	url := checkpointServer(t)
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
		GoldenAtx:   goldenAtx,
		DataDir:     t.TempDir(),
		DbFile:      "test.sql",
		LocalDbFile: "local.sql",
		NodeIDs:     []types.NodeID{sig1.NodeID(), sig2.NodeID(), sig3.NodeID(), sig4.NodeID()},
		Uri:         fmt.Sprintf("%s/snapshot-15", url),
		Restore:     types.LayerID(recoverLayer),
	}

	oldDB, err := statesql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, oldDB)

	vAtxs1, proofs1 := createAtxChain(t, sig1)
	vAtxs2, proofs2 := createAtxChain(t, sig2)
	vAtxs := append(vAtxs1, vAtxs2...)
	proofs := append(proofs1, proofs2...)
	vAtxs3, proofs3 := createInterlinkedAtxChain(t, sig3, sig4)
	vAtxs = append(vAtxs, vAtxs3...)
	proofs = append(proofs, proofs3...)

	validateAndPreserveData(t, oldDB, vAtxs)
	// the proofs are not valid, but save them anyway for the purpose of testing
	for _, proof := range proofs {
		encoded, err := codec.Encode(proof)
		require.NoError(t, err)

		ref, err := proof.Ref()
		require.NoError(t, err)

		err = poets.Add(
			oldDB,
			ref,
			encoded,
			proof.PoetServiceID,
			proof.RoundID,
		)
		require.NoError(t, err)
	}
	require.NoError(t, oldDB.Close())

	preserve, err := checkpoint.Recover(ctx, zaptest.NewLogger(t), afero.NewOsFs(), cfg)
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

	newDB, err := statesql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, newDB)
	t.Cleanup(func() { assert.NoError(t, newDB.Close()) })
	verifyDbContent(t, newDB)
	validateAndPreserveData(t, newDB, preserve.Deps)

	restore, err := recovery.CheckpointInfo(newDB)
	require.NoError(t, err)
	require.EqualValues(t, recoverLayer, restore)

	// sqlite create .sql, .sql-shm and .sql-wal files.
	files, err := filepath.Glob(fmt.Sprintf("%s/backup.*/%s*", cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 1)
}

func TestRecover_OwnAtxNotInCheckpoint_Preserve_IncludePending(t *testing.T) {
	t.Parallel()
	url := checkpointServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig3, err := signing.NewEdSigner()
	require.NoError(t, err)

	cfg := &checkpoint.RecoverConfig{
		GoldenAtx:   goldenAtx,
		DataDir:     t.TempDir(),
		DbFile:      "test.sql",
		LocalDbFile: "local.sql",
		NodeIDs:     []types.NodeID{sig1.NodeID(), sig2.NodeID(), sig3.NodeID()},
		Uri:         fmt.Sprintf("%s/snapshot-15", url),
		Restore:     types.LayerID(recoverLayer),
	}

	oldDB, err := statesql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, oldDB)

	vAtxs1, proofs1 := createAtxChain(t, sig1)
	vAtxs2, proofs2 := createInterlinkedAtxChain(t, sig2, sig3)
	vAtxs := append(vAtxs1, vAtxs2...)
	proofs := append(proofs1, proofs2...)
	validateAndPreserveData(t, oldDB, vAtxs)
	// the proofs are not valid, but save them anyway for the purpose of testing
	for _, proof := range proofs {
		encoded, err := codec.Encode(proof)
		require.NoError(t, err)

		ref, err := proof.Ref()
		require.NoError(t, err)

		require.NoError(
			t,
			poets.Add(
				oldDB,
				ref,
				encoded,
				proof.PoetServiceID,
				proof.RoundID,
			),
		)
	}
	require.NoError(t, oldDB.Close())

	// write pending nipost challenge to simulate a pending atx still waiting for poet proof.
	var prevAtx1 wire.ActivationTxV1
	require.NoError(t, codec.Decode(vAtxs1[len(vAtxs1)-2].Blob, &prevAtx1))
	var posAtx1 wire.ActivationTxV1
	require.NoError(t, codec.Decode(vAtxs1[len(vAtxs1)-1].Blob, &posAtx1))

	var prevAtx2 wire.ActivationTxV1
	require.NoError(t, codec.Decode(vAtxs2[len(vAtxs1)-2].Blob, &prevAtx2))
	var posAtx2 wire.ActivationTxV1
	require.NoError(t, codec.Decode(vAtxs2[len(vAtxs1)-1].Blob, &posAtx2))

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

	preserve, err := checkpoint.Recover(ctx, zaptest.NewLogger(t), afero.NewOsFs(), cfg)
	require.NoError(t, err)
	require.NotNil(t, preserve)

	// the two set of atxs have different received time. just compare IDs
	atxRef := atxIDs(append(vAtxs1, vAtxs2...))
	proofRef := proofRefs(append(proofs1, proofs2...))
	require.ElementsMatch(t, atxRef, atxIDs(preserve.Deps))
	require.ElementsMatch(t, proofRef, proofRefs(preserve.Proofs))

	newDB, err := statesql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, newDB)
	t.Cleanup(func() { assert.NoError(t, newDB.Close()) })
	verifyDbContent(t, newDB)
	validateAndPreserveData(t, newDB, preserve.Deps)

	restore, err := recovery.CheckpointInfo(newDB)
	require.NoError(t, err)
	require.EqualValues(t, recoverLayer, restore)

	// sqlite create .sql, .sql-shm and .sql-wal files.
	files, err := filepath.Glob(fmt.Sprintf("%s/backup.*/%s*", cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 1)
}

func TestRecover_OwnAtxNotInCheckpoint_Preserve_Still_Initializing(t *testing.T) {
	t.Parallel()
	url := checkpointServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)

	cfg := &checkpoint.RecoverConfig{
		GoldenAtx:   goldenAtx,
		DataDir:     t.TempDir(),
		DbFile:      "test.sql",
		LocalDbFile: "local.sql",
		NodeIDs:     []types.NodeID{sig1.NodeID(), sig2.NodeID()},
		Uri:         fmt.Sprintf("%s/snapshot-15", url),
		Restore:     types.LayerID(recoverLayer),
	}

	oldDB, err := statesql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, oldDB)

	vAtxs, proofs := createAtxChainDepsOnly(t)
	validateAndPreserveData(t, oldDB, vAtxs)
	// the proofs are not valid, but save them anyway for the purpose of testing
	for _, proof := range proofs {
		encoded, err := codec.Encode(proof)
		require.NoError(t, err)
		require.NoError(
			t,
			poets.Add(
				oldDB,
				types.PoetProofRef(types.CalcHash32(encoded)),
				encoded,
				proof.PoetServiceID,
				proof.RoundID,
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

	preserve, err := checkpoint.Recover(ctx, zaptest.NewLogger(t), afero.NewOsFs(), cfg)
	require.NoError(t, err)
	require.Nil(t, preserve)

	newDB, err := statesql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
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
	t.Parallel()
	url := checkpointServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	cfg := &checkpoint.RecoverConfig{
		GoldenAtx:   goldenAtx,
		DataDir:     t.TempDir(),
		DbFile:      "test.sql",
		LocalDbFile: "local.sql",
		NodeIDs:     []types.NodeID{sig.NodeID()},
		Uri:         fmt.Sprintf("%s/snapshot-15", url),
		Restore:     types.LayerID(recoverLayer),
	}

	oldDB, err := statesql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, oldDB)
	vAtxs, proofs := createAtxChain(t, sig)
	// make the first one from the previous snapshot
	var golden wire.ActivationTxV1
	require.NoError(t, codec.Decode(vAtxs[0].Blob, &golden))
	require.NoError(t, atxs.AddCheckpointed(oldDB, &atxs.CheckpointAtx{
		ID:            golden.ID(),
		Epoch:         golden.PublishEpoch,
		CommitmentATX: *golden.CommitmentATXID,
		VRFNonce:      types.VRFPostIndex(*golden.VRFNonce),
		NumUnits:      golden.NumUnits,
		SmesherID:     golden.SmesherID,
		Sequence:      golden.Sequence,
		Coinbase:      golden.Coinbase,
		Units:         map[types.NodeID]uint32{golden.SmesherID: golden.NumUnits},
	}))
	validateAndPreserveData(t, oldDB, vAtxs[1:])
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
				types.PoetProofRef(types.CalcHash32(encoded)),
				encoded,
				proof.PoetServiceID,
				proof.RoundID,
			),
		)
	}
	require.NoError(t, oldDB.Close())

	preserve, err := checkpoint.Recover(ctx, zaptest.NewLogger(t), afero.NewOsFs(), cfg)
	require.NoError(t, err)
	require.Nil(t, preserve)

	newDB, err := statesql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
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
	t.Parallel()
	url := checkpointServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	cfg := &checkpoint.RecoverConfig{
		GoldenAtx:   goldenAtx,
		DataDir:     t.TempDir(),
		DbFile:      "test.sql",
		LocalDbFile: "local.sql",
		NodeIDs:     []types.NodeID{sig.NodeID()},
		Uri:         fmt.Sprintf("%s/snapshot-15", url),
		Restore:     types.LayerID(recoverLayer),
	}

	oldDB, err := statesql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, oldDB)
	vAtxs, proofs := createAtxChain(t, sig)
	validateAndPreserveData(t, oldDB, vAtxs)
	// the proofs are not valid, but save them anyway for the purpose of testing
	for _, proof := range proofs {
		encoded, err := codec.Encode(proof)
		require.NoError(t, err)
		require.NoError(
			t,
			poets.Add(
				oldDB,
				types.PoetProofRef(types.CalcHash32(encoded)),
				encoded,
				proof.PoetServiceID,
				proof.RoundID,
			),
		)
	}
	require.NoError(t, oldDB.Close())

	preserve, err := checkpoint.Recover(ctx, zaptest.NewLogger(t), afero.NewOsFs(), cfg)
	require.NoError(t, err)
	require.Nil(t, preserve)

	newDB, err := statesql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
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
	t.Parallel()
	url := checkpointServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	nid, err := hex.DecodeString("0230c5d75d42b84f98800eceb47bc9cc4d803058900a50346a09ff61d56b6582")
	require.NoError(t, err)
	atxid, err := hex.DecodeString("98e47278c1f58acfd2b670a730f28898f74eb140482a07b91ff81f9ff0b7d9f4")
	require.NoError(t, err)
	atx := &types.ActivationTx{SmesherID: types.NodeID(nid)}
	atx.SetID(types.ATXID(atxid))

	cfg := &checkpoint.RecoverConfig{
		GoldenAtx:   goldenAtx,
		DataDir:     t.TempDir(),
		DbFile:      "test.sql",
		LocalDbFile: "local.sql",
		NodeIDs:     []types.NodeID{types.BytesToNodeID(nid)},
		Uri:         fmt.Sprintf("%s/snapshot-15", url),
		Restore:     types.LayerID(recoverLayer),
	}

	oldDB, err := statesql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
	require.NoError(t, err)
	require.NotNil(t, oldDB)
	require.NoError(t, atxs.Add(oldDB, atx, types.AtxBlob{}))
	require.NoError(t, oldDB.Close())

	preserve, err := checkpoint.Recover(ctx, zaptest.NewLogger(t), afero.NewOsFs(), cfg)
	require.NoError(t, err)
	require.Nil(t, preserve)

	newDB, err := statesql.Open("file:" + filepath.Join(cfg.DataDir, cfg.DbFile))
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
