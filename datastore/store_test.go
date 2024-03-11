package datastore_test

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/proposals/store"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/activesets"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/poets"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
)

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(3)

	res := m.Run()
	os.Exit(res)
}

func TestMalfeasanceProof_Honest(t *testing.T) {
	db := sql.InMemory()
	cdb := datastore.NewCachedDB(db, logtest.New(t))
	require.Equal(t, 0, cdb.MalfeasanceCacheSize())

	nodeID1 := types.NodeID{1}
	got, err := cdb.GetMalfeasanceProof(nodeID1)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Nil(t, got)
	require.Equal(t, 1, cdb.MalfeasanceCacheSize())

	// secretly save the proof to database
	require.NoError(t, identities.SetMalicious(db, nodeID1, []byte("bad"), time.Now()))
	bad, err := identities.IsMalicious(db, nodeID1)
	require.NoError(t, err)
	require.True(t, bad)
	require.Equal(t, 1, cdb.MalfeasanceCacheSize())

	// but it will retrieve it from cache
	got, err = cdb.GetMalfeasanceProof(nodeID1)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.Nil(t, got)
	require.Equal(t, 1, cdb.MalfeasanceCacheSize())
	bad, err = cdb.IsMalicious(nodeID1)
	require.NoError(t, err)
	require.False(t, bad)

	// asking will cause the answer cached for honest nodes
	nodeID2 := types.NodeID{2}
	bad, err = cdb.IsMalicious(nodeID2)
	require.NoError(t, err)
	require.False(t, bad)
	require.Equal(t, 2, cdb.MalfeasanceCacheSize())

	// secretly save the proof to database
	require.NoError(t, identities.SetMalicious(db, nodeID2, []byte("bad"), time.Now()))
	bad, err = identities.IsMalicious(db, nodeID2)
	require.NoError(t, err)
	require.True(t, bad)
	require.Equal(t, 2, cdb.MalfeasanceCacheSize())

	// but an add will update the cache
	proof := &types.MalfeasanceProof{
		Layer: types.LayerID(11),
		Proof: types.Proof{
			Type: types.MultipleBallots,
			Data: &types.BallotProof{
				Messages: [2]types.BallotProofMsg{
					{},
					{},
				},
			},
		},
	}
	cdb.CacheMalfeasanceProof(nodeID2, proof)
	bad, err = cdb.IsMalicious(nodeID2)
	require.NoError(t, err)
	require.True(t, bad)
	require.Equal(t, 2, cdb.MalfeasanceCacheSize())
}

func TestMalfeasanceProof_Dishonest(t *testing.T) {
	db := sql.InMemory()
	cdb := datastore.NewCachedDB(db, logtest.New(t))
	require.Equal(t, 0, cdb.MalfeasanceCacheSize())

	// a bad guy
	proof := &types.MalfeasanceProof{
		Layer: types.LayerID(11),
		Proof: types.Proof{
			Type: types.MultipleBallots,
			Data: &types.BallotProof{
				Messages: [2]types.BallotProofMsg{
					{},
					{},
				},
			},
		},
	}

	nodeID1 := types.NodeID{1}
	cdb.CacheMalfeasanceProof(nodeID1, proof)
	require.Equal(t, 1, cdb.MalfeasanceCacheSize())

	got, err := cdb.GetMalfeasanceProof(nodeID1)
	require.NoError(t, err)
	require.EqualValues(t, proof, got)
}

func TestIdentityExists(t *testing.T) {
	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	exists, err := cdb.IdentityExists(signer.NodeID())
	require.NoError(t, err)
	require.False(t, exists)

	atx := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch: types.EpochID(22),
				Sequence:     11,
			},
			NumUnits: 11,
		},
	}
	require.NoError(t, activation.SignAndFinalizeAtx(signer, atx))
	atx.SetReceived(time.Now())
	atx.SetEffectiveNumUnits(atx.NumUnits)
	vAtx, err := atx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(cdb, vAtx))

	exists, err = cdb.IdentityExists(signer.NodeID())
	require.NoError(t, err)
	require.True(t, exists)
}

func TestBlobStore_GetATXBlob(t *testing.T) {
	db := sql.InMemory()
	bs := datastore.NewBlobStore(db, store.New())
	ctx := context.Background()

	atx := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PublishEpoch: types.EpochID(22),
				Sequence:     11,
			},
			NumUnits: 11,
		},
	}
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	require.NoError(t, activation.SignAndFinalizeAtx(signer, atx))
	atx.SetEffectiveNumUnits(atx.NumUnits)
	atx.SetReceived(time.Now())
	vAtx, err := atx.Verify(0, 1)
	require.NoError(t, err)

	has, err := bs.Has(datastore.ATXDB, atx.ID().Bytes())
	require.NoError(t, err)
	require.False(t, has)

	_, err = bs.Get(ctx, datastore.ATXDB, atx.ID().Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)

	require.NoError(t, atxs.Add(db, vAtx))

	has, err = bs.Has(datastore.ATXDB, atx.ID().Bytes())
	require.NoError(t, err)
	require.True(t, has)
	got, err := bs.Get(ctx, datastore.ATXDB, atx.ID().Bytes())
	require.NoError(t, err)

	var gotA types.ActivationTx
	require.NoError(t, codec.Decode(got, &gotA))
	require.NoError(t, gotA.Initialize())
	gotA.SetEffectiveNumUnits(gotA.NumUnits)
	gotA.SetReceived(atx.Received())
	require.Equal(t, *atx, gotA)

	_, err = bs.Get(ctx, datastore.BallotDB, atx.ID().Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestBlobStore_GetBallotBlob(t *testing.T) {
	db := sql.InMemory()
	bs := datastore.NewBlobStore(db, store.New())
	ctx := context.Background()

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	blt := types.RandomBallot()
	blt.Signature = sig.Sign(signing.BALLOT, blt.SignedBytes())
	blt.SmesherID = sig.NodeID()
	require.NoError(t, blt.Initialize())

	has, err := bs.Has(datastore.BallotDB, blt.ID().Bytes())
	require.NoError(t, err)
	require.False(t, has)
	_, err = bs.Get(ctx, datastore.BallotDB, blt.ID().Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)

	require.NoError(t, ballots.Add(db, blt))
	has, err = bs.Has(datastore.BallotDB, blt.ID().Bytes())
	require.NoError(t, err)
	require.True(t, has)
	got, err := bs.Get(ctx, datastore.BallotDB, blt.ID().Bytes())
	require.NoError(t, err)
	var gotB types.Ballot
	require.NoError(t, codec.Decode(got, &gotB))

	require.NoError(t, gotB.Initialize())
	require.Equal(t, *blt, gotB)

	_, err = bs.Get(ctx, datastore.BlockDB, blt.ID().Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestBlobStore_GetBlockBlob(t *testing.T) {
	db := sql.InMemory()
	bs := datastore.NewBlobStore(db, store.New())
	ctx := context.Background()

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

	_, err = bs.Get(ctx, datastore.BlockDB, blk.ID().Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)

	require.NoError(t, blocks.Add(db, &blk))
	has, err = bs.Has(datastore.BlockDB, blk.ID().Bytes())
	require.NoError(t, err)
	require.True(t, has)
	got, err := bs.Get(ctx, datastore.BlockDB, blk.ID().Bytes())
	require.NoError(t, err)
	var gotB types.Block
	require.NoError(t, codec.Decode(got, &gotB))
	gotB.Initialize()
	require.Equal(t, blk, gotB)

	_, err = bs.Get(ctx, datastore.ProposalDB, blk.ID().Bytes())
	require.ErrorIs(t, err, store.ErrNotFound)
}

func TestBlobStore_GetPoetBlob(t *testing.T) {
	db := sql.InMemory()
	bs := datastore.NewBlobStore(db, store.New())
	ctx := context.Background()

	ref := []byte("ref0")
	poet := []byte("proof0")
	sid := []byte("sid0")
	rid := "rid0"

	has, err := bs.Has(datastore.POETDB, ref)
	require.NoError(t, err)
	require.False(t, has)

	_, err = bs.Get(ctx, datastore.POETDB, ref)
	require.ErrorIs(t, err, sql.ErrNotFound)
	var poetRef types.PoetProofRef
	copy(poetRef[:], ref)
	require.NoError(t, poets.Add(db, poetRef, poet, sid, rid))
	has, err = bs.Has(datastore.POETDB, ref)
	require.NoError(t, err)
	require.True(t, has)
	got, err := bs.Get(ctx, datastore.POETDB, ref)
	require.NoError(t, err)
	require.True(t, bytes.Equal(poet, got))

	_, err = bs.Get(ctx, datastore.BlockDB, ref)
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestBlobStore_GetProposalBlob(t *testing.T) {
	db := sql.InMemory()
	proposals := store.New()
	bs := datastore.NewBlobStore(db, proposals)
	ctx := context.Background()

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
	_, err = bs.Get(ctx, datastore.ProposalDB, p.ID().Bytes())
	require.ErrorIs(t, err, store.ErrNotFound)

	require.NoError(t, proposals.Add(&p))
	has, err = bs.Has(datastore.ProposalDB, p.ID().Bytes())
	require.NoError(t, err)
	require.True(t, has)
	got, err := bs.Get(ctx, datastore.ProposalDB, p.ID().Bytes())
	require.NoError(t, err)
	var gotP types.Proposal
	require.NoError(t, codec.Decode(got, &gotP))
	require.NoError(t, gotP.Initialize())
	require.Equal(t, p, gotP)
}

func TestBlobStore_GetTXBlob(t *testing.T) {
	db := sql.InMemory()
	bs := datastore.NewBlobStore(db, store.New())
	ctx := context.Background()

	tx := &types.Transaction{}
	tx.Raw = []byte{1, 1, 1}
	tx.ID = types.TransactionID{1}

	has, err := bs.Has(datastore.TXDB, tx.ID.Bytes())
	require.NoError(t, err)
	require.False(t, has)

	_, err = bs.Get(ctx, datastore.TXDB, tx.ID.Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)

	require.NoError(t, transactions.Add(db, tx, time.Now()))
	has, err = bs.Has(datastore.TXDB, tx.ID.Bytes())
	require.NoError(t, err)
	require.True(t, has)
	got, err := bs.Get(ctx, datastore.TXDB, tx.ID.Bytes())
	require.NoError(t, err)
	require.Equal(t, tx.Raw, got)

	_, err = bs.Get(ctx, datastore.BlockDB, tx.ID.Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestBlobStore_GetMalfeasanceBlob(t *testing.T) {
	db := sql.InMemory()
	bs := datastore.NewBlobStore(db, store.New())
	ctx := context.Background()

	proof := &types.MalfeasanceProof{
		Layer: types.LayerID(11),
		Proof: types.Proof{
			Type: types.HareEquivocation,
			Data: &types.HareProof{
				Messages: [2]types.HareProofMsg{{}, {}},
			},
		},
	}
	encoded, err := codec.Encode(proof)
	require.NoError(t, err)
	nodeID := types.NodeID{1, 2, 3}

	has, err := bs.Has(datastore.Malfeasance, nodeID.Bytes())
	require.NoError(t, err)
	require.False(t, has)

	_, err = bs.Get(ctx, datastore.Malfeasance, nodeID.Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)

	require.NoError(t, identities.SetMalicious(db, nodeID, encoded, time.Now()))
	has, err = bs.Has(datastore.Malfeasance, nodeID.Bytes())
	require.NoError(t, err)
	require.True(t, has)
	got, err := bs.Get(ctx, datastore.Malfeasance, nodeID.Bytes())
	require.NoError(t, err)
	require.Equal(t, encoded, got)
}

func TestBlobStore_GetActiveSet(t *testing.T) {
	db := sql.InMemory()
	bs := datastore.NewBlobStore(db, store.New())
	ctx := context.Background()

	as := &types.EpochActiveSet{Epoch: 7}
	hash := types.ATXIDList(as.Set).Hash()

	has, err := bs.Has(datastore.ActiveSet, hash.Bytes())
	require.NoError(t, err)
	require.False(t, has)

	_, err = bs.Get(ctx, datastore.ActiveSet, hash.Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)

	require.NoError(t, activesets.Add(db, hash, as))
	has, err = bs.Has(datastore.ActiveSet, hash.Bytes())
	require.NoError(t, err)
	require.True(t, has)
	got, err := bs.Get(ctx, datastore.ActiveSet, hash.Bytes())
	require.NoError(t, err)
	require.Equal(t, codec.MustEncode(as), got)
}
