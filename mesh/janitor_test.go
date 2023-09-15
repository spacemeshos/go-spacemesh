package mesh

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/mesh/mocks"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/activesets"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/sql/proposals"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
)

func TestPrune(t *testing.T) {
	db := sql.InMemory()
	current := types.LayerID(10)
	mc := mocks.NewMocklayerClock(gomock.NewController(t))
	done := make(chan struct{})
	count := 0
	mc.EXPECT().CurrentLayer().DoAndReturn(func() types.LayerID {
		if count == 0 {
			close(done)
		}
		count++
		return current
	}).AnyTimes()
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
	ctx, cancel := context.WithCancel(context.Background())
	var eg errgroup.Group
	eg.Go(func() error {
		Prune(ctx, logtest.New(t).Zap(), db, mc, confidenceDist, time.Millisecond)
		return nil
	})
	require.Eventually(t, func() bool {
		select {
		case <-done:
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
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)
	cancel()
	eg.Wait()
}

func TestExtractActiveSet(t *testing.T) {
	db := sql.InMemory()
	current := types.LayerID(20)
	blts := make([]*types.Ballot, 0, current)
	hashes := []types.Hash32{types.RandomHash(), types.RandomHash()}
	actives := [][]types.ATXID{types.RandomActiveSet(11), types.RandomActiveSet(19)}
	for lid := types.EpochID(2).FirstLayer(); lid < current; lid++ {
		blt := types.NewExistingBallot(types.RandomBallotID(), types.RandomEdSignature(), types.NodeID{1}, lid)
		if lid%3 == 0 {
			blt.EpochData = &types.EpochData{
				ActiveSetHash: hashes[0],
			}
			blt.ActiveSet = actives[0]
		}
		if lid%3 == 1 {
			blt.EpochData = &types.EpochData{
				ActiveSetHash: hashes[1],
			}
			blt.ActiveSet = actives[1]
		}
		require.NoError(t, ballots.Add(db, &blt))
		blts = append(blts, &blt)
	}
	require.NoError(t, ExtractActiveSet(db))
	for _, b := range blts {
		got, err := ballots.Get(db, b.ID())
		require.NoError(t, err)
		require.Empty(t, got.ActiveSet)
	}
	for i, h := range hashes {
		got, err := activesets.Get(db, h)
		require.NoError(t, err)
		require.Equal(t, actives[i], got.Set)
	}
}
