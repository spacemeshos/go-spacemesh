package prune

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/activesets"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
)

func TestPrune(t *testing.T) {
	types.SetLayersPerEpoch(3)

	db := sql.InMemory()
	current := types.LayerID(10)

	lyrProps := make([]*types.Proposal, 0, current)
	sets := map[types.EpochID][]types.Hash32{}
	for lid := types.LayerID(0); lid < current; lid++ {
		blt := types.NewExistingBallot(types.RandomBallotID(), types.RandomEdSignature(), types.NodeID{1}, lid)
		require.NoError(t, ballots.Add(db, &blt))
		p := &types.Proposal{
			InnerProposal: types.InnerProposal{
				Ballot: blt,
				TxIDs:  []types.TransactionID{types.RandomTransactionID(), types.RandomTransactionID()},
			},
			Signature: types.RandomEdSignature(),
		}
		p.SetID(types.RandomProposalID())
		require.NoError(t, certificates.Add(db, lid, &types.Certificate{BlockID: types.RandomBlockID()}))
		for _, tid := range p.TxIDs {
			require.NoError(t, transactions.AddToProposal(db, tid, lid, p.ID()))
		}
		lyrProps = append(lyrProps, p)

		set := &types.EpochActiveSet{
			Epoch: lid.GetEpoch(),
		}
		setid := types.Hash32{byte(lid)}
		sets[lid.GetEpoch()] = append(sets[lid.GetEpoch()], setid)
		require.NoError(t, activesets.Add(db, setid, set))
	}
	confidenceDist := uint32(3)

	pruner := New(db, confidenceDist, current.GetEpoch()-1, WithLogger(zaptest.NewLogger(t)))
	// Act
	require.NoError(t, pruner.Prune(current))

	// Verify
	oldest := current - types.LayerID(confidenceDist)
	for lid := types.LayerID(0); lid < oldest; lid++ {
		_, err := certificates.CertifiedBlock(db, lid)
		require.ErrorIs(t, err, sql.ErrNotFound)
		for _, tid := range lyrProps[lid].TxIDs {
			exists, err := transactions.HasProposalTX(db, lyrProps[lid].ID(), tid)
			require.NoError(t, err)
			require.False(t, exists)
		}
	}
	for lid := oldest; lid < current; lid++ {
		got, err := certificates.CertifiedBlock(db, lid)
		require.NoError(t, err)
		require.NotEqual(t, types.EmptyBlockID, got)
		for _, tid := range lyrProps[lid].TxIDs {
			exists, err := transactions.HasProposalTX(db, lyrProps[lid].ID(), tid)
			require.NoError(t, err)
			require.True(t, exists)
		}
	}
	for epoch, epochSets := range sets {
		for _, id := range epochSets {
			_, err := activesets.Get(db, id)
			if epoch >= current.GetEpoch()-1 {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, sql.ErrNotFound)
			}
		}
	}
}
