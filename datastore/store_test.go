package datastore_test

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	mwire "github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/proposals/store"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/activesets"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/malfeasance"
	"github.com/spacemeshos/go-spacemesh/sql/poets"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
)

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(3)

	res := m.Run()
	os.Exit(res)
}

func TestMalfeasanceProof_Dishonest(t *testing.T) {
	db := statesql.InMemoryTest(t)
	cdb := datastore.NewCachedDB(db, zaptest.NewLogger(t))
	t.Cleanup(func() { require.NoError(t, cdb.Close()) })

	proof := types.RandomBytes(100)

	nodeID1 := types.NodeID{1}
	cdb.CacheMalfeasanceProof(nodeID1, proof)

	got, err := cdb.MalfeasanceProof(nodeID1)
	require.NoError(t, err)
	require.EqualValues(t, proof, got)
}

func TestIterateMalfeasanceProofs(t *testing.T) {
	db := statesql.InMemoryTest(t)
	cdb := datastore.NewCachedDB(db, zaptest.NewLogger(t))
	t.Cleanup(func() { require.NoError(t, cdb.Close()) })

	proofs := map[types.NodeID][]byte{
		{1}: types.RandomBytes(100),
		{2}: types.RandomBytes(100),
		{3}: types.RandomBytes(100),
	}
	for id, proof := range proofs {
		require.NoError(t, identities.SetMalicious(db, id, proof, time.Now()))
	}

	gotProofs := make(map[types.NodeID][]byte)
	require.NoError(t, cdb.IterateMalfeasanceProofs(func(id types.NodeID, proof []byte) error {
		gotProofs[id] = proof
		return nil
	}))
	require.Equal(t, proofs, gotProofs)

	// stop early
	gotProofs = make(map[types.NodeID][]byte)
	callbackErr := errors.New("stop")
	err := cdb.IterateMalfeasanceProofs(func(id types.NodeID, proof []byte) error {
		gotProofs[id] = proof
		return callbackErr
	})
	require.ErrorIs(t, err, callbackErr)
}

func TestBlobStore_GetATXBlob(t *testing.T) {
	db := statesql.InMemoryTest(t)
	bs := datastore.NewBlobStore(db, store.New())

	atx := &types.ActivationTx{
		PublishEpoch: types.EpochID(22),
		Sequence:     11,
		NumUnits:     11,
		SmesherID:    types.RandomNodeID(),
	}
	atx.SetID(types.RandomATXID())
	atx.SetReceived(time.Now().Local())

	has, err := bs.Has(datastore.ATXDB, atx.ID().Bytes())
	require.NoError(t, err)
	require.False(t, has)

	var blob sql.Blob
	err = bs.LoadBlob(t.Context(), datastore.ATXDB, atx.ID().Bytes(), &blob)
	require.ErrorIs(t, err, datastore.ErrNotFound)

	atxBlob := types.AtxBlob{Blob: types.RandomBytes(100)}
	require.NoError(t, atxs.Add(db, atx, atxBlob))

	has, err = bs.Has(datastore.ATXDB, atx.ID().Bytes())
	require.NoError(t, err)
	require.True(t, has)
	err = bs.LoadBlob(t.Context(), datastore.ATXDB, atx.ID().Bytes(), &blob)
	require.NoError(t, err)
	require.Equal(t, atxBlob.Blob, blob.Bytes)
}

func TestBlobStore_GetBallotBlob(t *testing.T) {
	db := statesql.InMemoryTest(t)
	bs := datastore.NewBlobStore(db, store.New())

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	blt := types.RandomBallot()
	blt.Signature = sig.Sign(signing.BALLOT, blt.SignedBytes())
	blt.SmesherID = sig.NodeID()
	require.NoError(t, blt.Initialize())

	has, err := bs.Has(datastore.BallotDB, blt.ID().Bytes())
	require.NoError(t, err)
	require.False(t, has)

	var blob sql.Blob
	err = bs.LoadBlob(t.Context(), datastore.BallotDB, blt.ID().Bytes(), &blob)
	require.ErrorIs(t, err, datastore.ErrNotFound)

	require.NoError(t, ballots.Add(db, blt))
	has, err = bs.Has(datastore.BallotDB, blt.ID().Bytes())
	require.NoError(t, err)
	require.True(t, has)

	err = bs.LoadBlob(t.Context(), datastore.BallotDB, blt.ID().Bytes(), &blob)
	require.NoError(t, err)
	var gotB types.Ballot
	require.NoError(t, codec.Decode(blob.Bytes, &gotB))

	require.NoError(t, gotB.Initialize())
	require.Equal(t, *blt, gotB)
}

func TestBlobStore_GetBlockBlob(t *testing.T) {
	db := statesql.InMemoryTest(t)
	bs := datastore.NewBlobStore(db, store.New())

	blk := types.Block{
		InnerBlock: types.InnerBlock{
			LayerIndex: types.LayerID(11),
			TxIDs:      types.RandomTXSet(3),
		},
	}
	blk.Initialize()

	has, err := bs.Has(datastore.BlockDB, blk.ID().Bytes())
	require.NoError(t, err)
	require.False(t, has)

	var blob sql.Blob
	err = bs.LoadBlob(t.Context(), datastore.BlockDB, blk.ID().Bytes(), &blob)
	require.ErrorIs(t, err, datastore.ErrNotFound)

	require.NoError(t, blocks.Add(db, &blk))
	has, err = bs.Has(datastore.BlockDB, blk.ID().Bytes())
	require.NoError(t, err)
	require.True(t, has)

	err = bs.LoadBlob(t.Context(), datastore.BlockDB, blk.ID().Bytes(), &blob)
	require.NoError(t, err)
	var gotB types.Block
	require.NoError(t, codec.Decode(blob.Bytes, &gotB))
	gotB.Initialize()
	require.Equal(t, blk, gotB)
}

func TestBlobStore_GetPoetBlob(t *testing.T) {
	db := statesql.InMemoryTest(t)
	bs := datastore.NewBlobStore(db, store.New())

	ref := []byte("ref0")
	poet := []byte("proof0")
	sid := []byte("sid0")
	rid := "rid0"

	has, err := bs.Has(datastore.POETDB, ref)
	require.NoError(t, err)
	require.False(t, has)

	require.ErrorIs(t, bs.LoadBlob(t.Context(), datastore.POETDB, ref, &sql.Blob{}), datastore.ErrNotFound)
	var poetRef types.PoetProofRef
	copy(poetRef[:], ref)
	require.NoError(t, poets.Add(db, poetRef, poet, sid, rid))

	has, err = bs.Has(datastore.POETDB, ref)
	require.NoError(t, err)
	require.True(t, has)

	var blob sql.Blob
	require.NoError(t, bs.LoadBlob(t.Context(), datastore.POETDB, poetRef[:], &blob))
	require.Equal(t, poet, blob.Bytes)
}

func TestBlobStore_GetProposalBlob(t *testing.T) {
	db := statesql.InMemoryTest(t)
	proposals := store.New()
	bs := datastore.NewBlobStore(db, proposals)

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	blt := types.RandomBallot()
	blt.Signature = signer.Sign(signing.BALLOT, blt.SignedBytes())
	p := types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: *blt,
			TxIDs:  types.RandomTXSet(11),
		},
	}
	p.Signature = signer.Sign(signing.PROPOSAL, p.SignedBytes())
	p.SmesherID = signer.NodeID()
	require.NoError(t, p.Initialize())

	has, err := bs.Has(datastore.ProposalDB, p.ID().Bytes())
	require.NoError(t, err)
	require.False(t, has)

	var blob sql.Blob
	err = bs.LoadBlob(t.Context(), datastore.ProposalDB, p.ID().Bytes(), &blob)
	require.ErrorIs(t, err, datastore.ErrNotFound)

	require.NoError(t, proposals.Add(&p))
	has, err = bs.Has(datastore.ProposalDB, p.ID().Bytes())
	require.NoError(t, err)
	require.True(t, has)

	err = bs.LoadBlob(t.Context(), datastore.ProposalDB, p.ID().Bytes(), &blob)
	require.NoError(t, err)
	var gotP types.Proposal
	require.NoError(t, codec.Decode(blob.Bytes, &gotP))
	require.NoError(t, gotP.Initialize())
	require.Equal(t, p, gotP)
}

func TestBlobStore_GetTXBlob(t *testing.T) {
	db := statesql.InMemoryTest(t)
	bs := datastore.NewBlobStore(db, store.New())

	tx := &types.Transaction{}
	tx.Raw = []byte{1, 1, 1}
	tx.ID = types.TransactionID{1}

	has, err := bs.Has(datastore.TXDB, tx.ID.Bytes())
	require.NoError(t, err)
	require.False(t, has)

	var blob sql.Blob
	err = bs.LoadBlob(t.Context(), datastore.TXDB, tx.ID.Bytes(), &blob)
	require.ErrorIs(t, err, datastore.ErrNotFound)

	require.NoError(t, transactions.Add(db, tx, time.Now()))
	has, err = bs.Has(datastore.TXDB, tx.ID.Bytes())
	require.NoError(t, err)
	require.True(t, has)

	err = bs.LoadBlob(t.Context(), datastore.TXDB, tx.ID.Bytes(), &blob)
	require.NoError(t, err)
	require.Equal(t, tx.Raw, blob.Bytes)
}

func TestBlobStore_GetLegacyMalfeasanceBlob(t *testing.T) {
	db := statesql.InMemoryTest(t)
	bs := datastore.NewBlobStore(db, store.New())

	proof := &mwire.MalfeasanceProof{
		Layer: types.LayerID(11),
		Proof: mwire.Proof{
			Type: mwire.HareEquivocation,
			Data: &mwire.HareProof{
				Messages: [2]mwire.HareProofMsg{{}, {}},
			},
		},
	}
	encoded, err := codec.Encode(proof)
	require.NoError(t, err)
	nodeID := types.NodeID{1, 2, 3}

	has, err := bs.Has(datastore.LegacyMalfeasance, nodeID.Bytes())
	require.NoError(t, err)
	require.False(t, has)

	var blob sql.Blob
	err = bs.LoadBlob(t.Context(), datastore.LegacyMalfeasance, nodeID.Bytes(), &blob)
	require.ErrorIs(t, err, datastore.ErrNotFound)

	require.NoError(t, identities.SetMalicious(db, nodeID, encoded, time.Now()))
	has, err = bs.Has(datastore.LegacyMalfeasance, nodeID.Bytes())
	require.NoError(t, err)
	require.True(t, has)

	err = bs.LoadBlob(t.Context(), datastore.LegacyMalfeasance, nodeID.Bytes(), &blob)
	require.NoError(t, err)
	require.Equal(t, encoded, blob.Bytes)
}

func TestBlobStore_GetMalfeasanceBlob(t *testing.T) {
	db := statesql.InMemoryTest(t)
	bs := datastore.NewBlobStore(db, store.New())

	ctrl := gomock.NewController(t)
	mMal := datastore.NewMockMalfeasanceProvider(ctrl)
	bs.SetMalfeasanceProvider(mMal)

	proofBytes := types.RandomBytes(100)
	nodeID := types.NodeID{1, 2, 3}

	has, err := bs.Has(datastore.Malfeasance, nodeID.Bytes())
	require.NoError(t, err)
	require.False(t, has)

	mMal.EXPECT().ProofByID(gomock.Any(), nodeID).Return(nil, sql.ErrNotFound)
	var blob sql.Blob
	err = bs.LoadBlob(t.Context(), datastore.Malfeasance, nodeID.Bytes(), &blob)
	require.ErrorIs(t, err, datastore.ErrNotFound)

	require.NoError(t, malfeasance.AddProof(db, nodeID, nil, proofBytes, 1, time.Now()))
	has, err = bs.Has(datastore.Malfeasance, nodeID.Bytes())
	require.NoError(t, err)
	require.True(t, has)

	mMal.EXPECT().ProofByID(gomock.Any(), nodeID).Return(proofBytes, nil)
	err = bs.LoadBlob(t.Context(), datastore.Malfeasance, nodeID.Bytes(), &blob)
	require.NoError(t, err)
	require.Equal(t, proofBytes, blob.Bytes)
}

func TestBlobStore_GetActiveSet(t *testing.T) {
	db := statesql.InMemoryTest(t)
	bs := datastore.NewBlobStore(db, store.New())

	as := &types.EpochActiveSet{Epoch: 7}
	hash := types.ATXIDList(as.Set).Hash()

	has, err := bs.Has(datastore.ActiveSet, hash.Bytes())
	require.NoError(t, err)
	require.False(t, has)

	var blob sql.Blob
	err = bs.LoadBlob(t.Context(), datastore.ActiveSet, hash.Bytes(), &blob)
	require.ErrorIs(t, err, datastore.ErrNotFound)

	require.NoError(t, activesets.Add(db, hash, as))
	has, err = bs.Has(datastore.ActiveSet, hash.Bytes())
	require.NoError(t, err)
	require.True(t, has)

	err = bs.LoadBlob(t.Context(), datastore.ActiveSet, hash.Bytes(), &blob)
	require.NoError(t, err)
	require.Equal(t, codec.MustEncode(as), blob.Bytes)
}
