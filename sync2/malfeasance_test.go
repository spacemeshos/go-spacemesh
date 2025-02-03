package sync2_test

import (
	"context"
	"testing"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sync2"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync/mocks"
)

func TestMalfeasanceHandler(t *testing.T) {
	ctrl := gomock.NewController(t)
	allNodes := make([]types.NodeID, 10)
	logger := zaptest.NewLogger(t)
	peer := p2p.Peer("foobar")
	for i := range allNodes {
		allNodes[i] = types.RandomNodeID()
	}
	f := NewMockFetcher(ctrl)
	clock := clockwork.NewFakeClock()
	h := sync2.NewMalfeasanceHandler(logger, f, testCfg, clock)
	baseSet := mocks.NewMockOrderedSet(ctrl)
	for _, id := range allNodes {
		baseSet.EXPECT().Has(rangesync.KeyBytes(id.Bytes()))
		f.EXPECT().RegisterPeerHashes(peer, []types.Hash32{types.Hash32(id)})
	}
	toFetch := make(map[types.NodeID]bool)
	for _, id := range allNodes {
		toFetch[id] = true
	}
	var batches []int
	f.EXPECT().GetMalfeasanceProofsWithCallback(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, nodes []types.NodeID, callback func(types.NodeID, error)) error {
			batches = append(batches, len(nodes))
			for _, id := range nodes {
				require.True(t, toFetch[id], "already fetched or bad ID")
				delete(toFetch, id)
				callback(id, nil)
			}
			return nil
		}).Times(3)
	require.NoError(t, h.Commit(context.Background(), peer, baseSet, byteSeqResult(allNodes)))
	require.Empty(t, toFetch)
	require.Equal(t, []int{4, 4, 2}, batches)
}
