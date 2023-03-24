package datastore_test

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/poets"
	"github.com/spacemeshos/go-spacemesh/sql/proposals"
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
	require.NoError(t, identities.SetMalicious(db, nodeID1, []byte("bad")))
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
	require.NoError(t, identities.SetMalicious(db, nodeID2, []byte("bad")))
	bad, err = identities.IsMalicious(db, nodeID2)
	require.NoError(t, err)
	require.True(t, bad)
	require.Equal(t, 2, cdb.MalfeasanceCacheSize())

	// but an add will update the cache
	proof := &types.MalfeasanceProof{
		Layer: types.NewLayerID(11),
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
	require.NoError(t, cdb.AddMalfeasanceProof(nodeID2, proof, nil))
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
		Layer: types.NewLayerID(11),
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
	require.NoError(t, cdb.AddMalfeasanceProof(nodeID1, proof, nil))
	require.Equal(t, 1, cdb.MalfeasanceCacheSize())

	got, err := cdb.GetMalfeasanceProof(nodeID1)
	require.NoError(t, err)
	require.EqualValues(t, proof, got)

	got, err = identities.GetMalfeasanceProof(db, nodeID1)
	require.NoError(t, err)
	require.EqualValues(t, proof, got)

	nodeID2 := types.NodeID{2}
	// secretly save the proof to database for a different id
	encoded, err := codec.Encode(proof)
	require.NoError(t, err)
	require.NoError(t, identities.SetMalicious(db, nodeID2, encoded))
	bad, err := identities.IsMalicious(db, nodeID2)
	require.NoError(t, err)
	require.True(t, bad)
	require.Equal(t, 1, cdb.MalfeasanceCacheSize())

	// just asking for boolean will not cause it to cache
	bad, err = cdb.IsMalicious(nodeID2)
	require.NoError(t, err)
	require.True(t, bad)
	require.Equal(t, 1, cdb.MalfeasanceCacheSize())

	// but asking for real proof data will cause it to cache
	got, err = cdb.GetMalfeasanceProof(nodeID2)
	require.NoError(t, err)
	require.EqualValues(t, proof, got)
	require.Equal(t, 2, cdb.MalfeasanceCacheSize())
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
				PubLayerID: types.NewLayerID(22),
				Sequence:   11,
			},
			NumUnits: 11,
		},
	}
	require.NoError(t, activation.SignAndFinalizeAtx(signer, atx))
	atx.SetReceived(time.Now())
	atx.SetEffectiveNumUnits(atx.NumUnits)
	nodeID := signer.NodeID()
	atx.SetNodeID(&nodeID)
	vAtx, err := atx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(cdb, vAtx))

	exists, err = cdb.IdentityExists(signer.NodeID())
	require.NoError(t, err)
	require.True(t, exists)
}

func TestStore_GetAtxByNodeID(t *testing.T) {
	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(t))

	atx3 := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: types.EpochID(3).FirstLayer(),
				Sequence:   11,
			},
			NumUnits: 11,
		},
	}
	atx4 := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: types.EpochID(4).FirstLayer(),
				Sequence:   12,
			},
			NumUnits: 11,
		},
	}
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	nodeID := signer.NodeID()
	for _, atx := range []*types.ActivationTx{atx3, atx4} {
		require.NoError(t, activation.SignAndFinalizeAtx(signer, atx))
		atx.SetEffectiveNumUnits(atx.NumUnits)
		atx.SetReceived(time.Now())
		atx.SetNodeID(&nodeID)
		vAtx, err := atx.Verify(0, 1)
		require.NoError(t, err)
		require.NoError(t, atxs.Add(cdb, vAtx))
	}

	got, err := cdb.GetEpochAtx(types.EpochID(3), signer.NodeID())
	require.NoError(t, err)
	require.Equal(t, atx3.ID(), got.ID)

	got, err = cdb.GetLastAtx(signer.NodeID())
	require.NoError(t, err)
	require.Equal(t, atx4.ID(), got.ID)
}

func TestBlobStore_GetATXBlob(t *testing.T) {
	db := sql.InMemory()
	bs := datastore.NewBlobStore(db)

	atx := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: types.NewLayerID(22),
				Sequence:   11,
			},
			NumUnits: 11,
		},
	}
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	require.NoError(t, activation.SignAndFinalizeAtx(signer, atx))
	atx.SetEffectiveNumUnits(atx.NumUnits)
	atx.SetReceived(time.Now())
	extractedNodeID := signer.NodeID()
	atx.SetNodeID(&extractedNodeID)
	vAtx, err := atx.Verify(0, 1)
	require.NoError(t, err)

	_, err = bs.Get(datastore.ATXDB, atx.ID().Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.NoError(t, atxs.Add(db, vAtx))
	got, err := bs.Get(datastore.ATXDB, atx.ID().Bytes())
	require.NoError(t, err)

	var gotA types.ActivationTx
	require.NoError(t, codec.Decode(got, &gotA))
	require.NoError(t, gotA.CalcAndSetID())
	extract, err := signing.NewPubKeyExtractor()
	require.NoError(t, err)
	extractedNodeID, err = extract.ExtractNodeID(signing.ATX, atx.SignedBytes(), atx.Signature)
	require.NoError(t, err)
	gotA.SetNodeID(&extractedNodeID)
	gotA.SetEffectiveNumUnits(gotA.NumUnits)
	gotA.SetReceived(atx.Received())
	require.Equal(t, *atx, gotA)

	_, err = bs.Get(datastore.BallotDB, atx.ID().Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestBlobStore_GetBallotBlob(t *testing.T) {
	db := sql.InMemory()
	bs := datastore.NewBlobStore(db)

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	blt := types.RandomBallot()
	blt.Signature = sig.Sign(signing.BALLOT, blt.SignedBytes())
	blt.SetSmesherID(sig.NodeID())
	require.NoError(t, blt.Initialize())

	_, err = bs.Get(datastore.BallotDB, blt.ID().Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.NoError(t, ballots.Add(db, blt))
	got, err := bs.Get(datastore.BallotDB, blt.ID().Bytes())
	require.NoError(t, err)
	var gotB types.Ballot
	require.NoError(t, codec.Decode(got, &gotB))
	extract, err := signing.NewPubKeyExtractor()
	require.NoError(t, err)
	nodeID, err := extract.ExtractNodeID(signing.BALLOT, gotB.SignedBytes(), gotB.Signature)
	require.NoError(t, err)
	gotB.SetSmesherID(nodeID)
	require.NoError(t, gotB.Initialize())
	require.Equal(t, *blt, gotB)

	_, err = bs.Get(datastore.BlockDB, blt.ID().Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestBlobStore_GetBlockBlob(t *testing.T) {
	db := sql.InMemory()
	bs := datastore.NewBlobStore(db)

	blk := types.Block{
		InnerBlock: types.InnerBlock{
			LayerIndex: types.NewLayerID(11),
			TxIDs:      types.RandomTXSet(3),
		},
	}
	blk.Initialize()

	_, err := bs.Get(datastore.BlockDB, blk.ID().Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.NoError(t, blocks.Add(db, &blk))
	got, err := bs.Get(datastore.BlockDB, blk.ID().Bytes())
	require.NoError(t, err)
	var gotB types.Block
	require.NoError(t, codec.Decode(got, &gotB))
	gotB.Initialize()
	require.Equal(t, blk, gotB)
	_, err = bs.Get(datastore.ProposalDB, blk.ID().Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestBlobStore_GetPoetBlob(t *testing.T) {
	db := sql.InMemory()
	bs := datastore.NewBlobStore(db)

	ref := []byte("ref0")
	poet := []byte("proof0")
	sid := []byte("sid0")
	rid := "rid0"

	_, err := bs.Get(datastore.POETDB, ref)
	require.ErrorIs(t, err, sql.ErrNotFound)
	var poetRef types.PoetProofRef
	copy(poetRef[:], ref)
	require.NoError(t, poets.Add(db, poetRef, poet, sid, rid))
	got, err := bs.Get(datastore.POETDB, ref)
	require.NoError(t, err)
	require.True(t, bytes.Equal(poet, got))

	_, err = bs.Get(datastore.BlockDB, ref)
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestBlobStore_GetProposalBlob(t *testing.T) {
	db := sql.InMemory()
	bs := datastore.NewBlobStore(db)

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
	p.Signature = signer.Sign(signing.BALLOT, p.SignedBytes())
	p.SetSmesherID(signer.NodeID())
	require.NoError(t, p.Initialize())

	_, err = bs.Get(datastore.ProposalDB, p.ID().Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.NoError(t, ballots.Add(db, blt))
	require.NoError(t, proposals.Add(db, &p))
	got, err := bs.Get(datastore.ProposalDB, p.ID().Bytes())
	require.NoError(t, err)
	var gotP types.Proposal
	require.NoError(t, codec.Decode(got, &gotP))
	extract, err := signing.NewPubKeyExtractor()
	require.NoError(t, err)
	nodeID, err := extract.ExtractNodeID(signing.BALLOT, gotP.SignedBytes(), gotP.Signature)
	require.NoError(t, err)
	gotP.SetSmesherID(nodeID)
	require.NoError(t, gotP.Initialize())
	require.Equal(t, p, gotP)
	_, err = bs.Get(datastore.BlockDB, p.ID().Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestBlobStore_GetTXBlob(t *testing.T) {
	db := sql.InMemory()
	bs := datastore.NewBlobStore(db)

	tx := &types.Transaction{}
	tx.Raw = []byte{1, 1, 1}
	tx.ID = types.TransactionID{1}

	_, err := bs.Get(datastore.TXDB, tx.ID.Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.NoError(t, transactions.Add(db, tx, time.Now()))
	got, err := bs.Get(datastore.TXDB, tx.ID.Bytes())
	require.NoError(t, err)
	require.Equal(t, tx.Raw, got)

	_, err = bs.Get(datastore.BlockDB, tx.ID.Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestBlobStore_GetMalfeasanceBlob(t *testing.T) {
	db := sql.InMemory()
	bs := datastore.NewBlobStore(db)

	proof := &types.MalfeasanceProof{
		Layer: types.NewLayerID(11),
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

	_, err = bs.Get(datastore.Malfeasance, nodeID.Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.NoError(t, identities.SetMalicious(db, nodeID, encoded))
	got, err := bs.Get(datastore.Malfeasance, nodeID.Bytes())
	require.NoError(t, err)
	require.Equal(t, encoded, got)
}
