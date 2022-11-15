package datastore

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/poets"
	"github.com/spacemeshos/go-spacemesh/sql/proposals"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
)

func TestBlobStore_GetATXBlob(t *testing.T) {
	types.SetLayersPerEpoch(3)
	db := sql.InMemory()
	bs := NewBlobStore(db)

	signer := signing.NewEdSigner()
	atx := &types.ActivationTx{
		InnerActivationTx: types.InnerActivationTx{
			NIPostChallenge: types.NIPostChallenge{
				PubLayerID: types.NewLayerID(22),
				Sequence:   11,
			},
			NumUnits: 11,
		},
	}
	data, err := atx.InnerBytes()
	require.NoError(t, err)
	atx.Sig = signer.Sign(data)
	require.NoError(t, atx.CalcAndSetID())
	require.NoError(t, atx.CalcAndSetNodeID())

	_, err = bs.Get(ATXDB, atx.ID().Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)
	vAtx, err := atx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(db, vAtx, time.Now()))
	got, err := bs.Get(ATXDB, atx.ID().Bytes())
	require.NoError(t, err)

	var gotA types.ActivationTx
	require.NoError(t, codec.Decode(got, &gotA))
	require.NoError(t, gotA.CalcAndSetID())
	require.NoError(t, gotA.CalcAndSetNodeID())
	require.Equal(t, *atx, gotA)

	_, err = bs.Get(BallotDB, atx.ID().Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestBlobStore_GetBallotBlob(t *testing.T) {
	db := sql.InMemory()
	bs := NewBlobStore(db)

	blt := types.RandomBallot()
	blt.Signature = signing.NewEdSigner().Sign(blt.SignedBytes())
	require.NoError(t, blt.Initialize())

	_, err := bs.Get(BallotDB, blt.ID().Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.NoError(t, ballots.Add(db, blt))
	got, err := bs.Get(BallotDB, blt.ID().Bytes())
	require.NoError(t, err)
	var gotB types.Ballot
	require.NoError(t, codec.Decode(got, &gotB))
	require.NoError(t, gotB.Initialize())
	require.Equal(t, *blt, gotB)

	_, err = bs.Get(BlockDB, blt.ID().Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestBlobStore_GetBlockBlob(t *testing.T) {
	db := sql.InMemory()
	bs := NewBlobStore(db)

	blk := types.Block{
		InnerBlock: types.InnerBlock{
			LayerIndex: types.NewLayerID(11),
			TxIDs:      types.RandomTXSet(3),
		},
	}
	blk.Initialize()

	_, err := bs.Get(BlockDB, blk.ID().Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.NoError(t, blocks.Add(db, &blk))
	got, err := bs.Get(BlockDB, blk.ID().Bytes())
	require.NoError(t, err)
	var gotB types.Block
	require.NoError(t, codec.Decode(got, &gotB))
	gotB.Initialize()
	require.Equal(t, blk, gotB)
	_, err = bs.Get(ProposalDB, blk.ID().Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestBlobStore_GetPoetBlob(t *testing.T) {
	db := sql.InMemory()
	bs := NewBlobStore(db)

	ref := []byte("ref0")
	poet := []byte("proof0")
	sid := []byte("sid0")
	rid := "rid0"

	_, err := bs.Get(POETDB, ref)
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.NoError(t, poets.Add(db, ref, poet, sid, rid))
	got, err := bs.Get(POETDB, ref)
	require.NoError(t, err)
	require.True(t, bytes.Equal(poet, got))

	_, err = bs.Get(BlockDB, ref)
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestBlobStore_GetProposalBlob(t *testing.T) {
	db := sql.InMemory()
	bs := NewBlobStore(db)

	signer := signing.NewEdSigner()
	blt := types.RandomBallot()
	blt.Signature = signer.Sign(blt.SignedBytes())
	p := types.Proposal{
		InnerProposal: types.InnerProposal{
			Ballot: *blt,
			TxIDs:  types.RandomTXSet(11),
		},
	}
	p.Signature = signer.Sign(p.Bytes())
	require.NoError(t, p.Initialize())

	_, err := bs.Get(ProposalDB, p.ID().Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.NoError(t, ballots.Add(db, blt))
	require.NoError(t, proposals.Add(db, &p))
	got, err := bs.Get(ProposalDB, p.ID().Bytes())
	require.NoError(t, err)
	var gotP types.Proposal
	require.NoError(t, codec.Decode(got, &gotP))
	require.NoError(t, gotP.Initialize())
	require.Equal(t, p, gotP)

	_, err = bs.Get(BlockDB, p.ID().Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)
}

func TestBlobStore_GetTXBlob(t *testing.T) {
	db := sql.InMemory()
	bs := NewBlobStore(db)

	tx := &types.Transaction{}
	tx.Raw = []byte{1, 1, 1}
	tx.ID = types.TransactionID{1}

	_, err := bs.Get(TXDB, tx.ID.Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)
	require.NoError(t, transactions.Add(db, tx, time.Now()))
	got, err := bs.Get(TXDB, tx.ID.Bytes())
	require.NoError(t, err)
	require.Equal(t, tx.Raw, got)

	_, err = bs.Get(BlockDB, tx.ID.Bytes())
	require.ErrorIs(t, err, sql.ErrNotFound)
}
