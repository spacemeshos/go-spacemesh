package prune

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/sql/proposals"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
)

func TestPrune(t *testing.T) {
	db := sql.InMemory()
	current := types.LayerID(10)
	mc := NewMocklayerClock(gomock.NewController(t))
	mc.EXPECT().CurrentLayer().Return(current).AnyTimes()
	lyrProps := make([]*types.Proposal, 0, current)
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
		require.NoError(t, proposals.Add(db, p))
		require.NoError(t, certificates.Add(db, lid, &types.Certificate{BlockID: types.RandomBlockID()}))
		for _, tid := range p.TxIDs {
			require.NoError(t, transactions.AddToProposal(db, tid, lid, p.ID()))
		}
		lyrProps = append(lyrProps, p)
	}
	confidenceDist := uint32(3)

	// Act
	prune(logtest.New(t).Zap(), db, mc, confidenceDist)

	// Verify
	oldest := current - types.LayerID(confidenceDist)
	for lid := types.LayerID(0); lid < oldest; lid++ {
		_, err := certificates.CertifiedBlock(db, lid)
		require.ErrorIs(t, err, sql.ErrNotFound)
		_, err = proposals.GetByLayer(db, lid)
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
		pps, err := proposals.GetByLayer(db, lid)
		require.NoError(t, err)
		require.NotEmpty(t, pps)
		for _, tid := range lyrProps[lid].TxIDs {
			exists, err := transactions.HasProposalTX(db, lyrProps[lid].ID(), tid)
			require.NoError(t, err)
			require.True(t, exists)
		}
	}
}
