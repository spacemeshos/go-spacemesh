package eligibility

import (
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestMinerWeight(t *testing.T) {
	t.Run("caches successful responses", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mock := NewMockweights(ctrl)
		cache := NewCachedWeights(mock)

		nodeID := types.NodeID{1, 2, 3}
		expectedWeight := uint64(100)

		mock.EXPECT().
			MinerWeight(gomock.Any(), types.EpochID(1), nodeID).
			Return(expectedWeight, nil).
			Times(1)

		// First call should hit the service
		got, err := cache.MinerWeight(t.Context(), 1, nodeID)
		require.NoError(t, err)
		require.Equal(t, expectedWeight, got)

		// Second call should use cache
		got, err = cache.MinerWeight(t.Context(), 1, nodeID)
		require.NoError(t, err)
		require.Equal(t, expectedWeight, got)
	})

	t.Run("doesn't cache errors", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mock := NewMockweights(ctrl)
		cache := NewCachedWeights(mock)

		nodeID := types.NodeID{1, 2, 3}
		expectedErr := errors.New("weight not found")

		mock.EXPECT().
			MinerWeight(gomock.Any(), types.EpochID(1), nodeID).
			Return(uint64(0), expectedErr).
			Times(2)

		// First call should fail
		_, err := cache.MinerWeight(t.Context(), 1, nodeID)
		require.Error(t, err)
		require.ErrorIs(t, err, expectedErr)

		// Second call should try again
		_, err = cache.MinerWeight(t.Context(), 1, nodeID)
		require.Error(t, err)
		require.ErrorIs(t, err, expectedErr)
	})

	t.Run("handles parallel requests correctly", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mock := NewMockweights(ctrl)
		cache := NewCachedWeights(mock)

		const numNodes = 1000

		epochs := []types.EpochID{1, 2}
		nodes := make([]types.NodeID, numNodes)
		for i := range nodes {
			binary.LittleEndian.PutUint64(nodes[i][:], uint64(i))
		}

		var eg errgroup.Group

		// Launch parallel requests for all combinations
		for _, epoch := range epochs {
			for i, node := range nodes {
				expectedWeight := uint64(epoch)*numNodes + uint64(i)
				mock.EXPECT().MinerWeight(gomock.Any(), epoch, node).Return(expectedWeight, nil)
				eg.Go(func() error {
					w, _ := cache.MinerWeight(t.Context(), epoch, node)
					if w != expectedWeight {
						return fmt.Errorf(
							"wrong weight for epoch %d node %v: got %d, want %d ",
							epoch,
							node,
							w,
							expectedWeight,
						)
					}
					return nil
				})
			}
		}
		require.NoError(t, eg.Wait())

		// Verify all values are properly cached
		for _, epoch := range epochs {
			for i, node := range nodes {
				expectedWeight := uint64(epoch)*numNodes + uint64(i)
				w, err := cache.MinerWeight(t.Context(), epoch, node)
				require.NoError(t, err)
				require.Equal(t, expectedWeight, w)
			}
		}
	})
	t.Run("evicts old epochs", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mock := NewMockweights(ctrl)
		cache := NewCachedWeights(mock)

		nodeID := types.NodeID{1, 2, 3}
		weight := uint64(100)

		// Set up initial cache for epoch 1
		mock.EXPECT().MinerWeight(gomock.Any(), types.EpochID(1), nodeID).Return(weight, nil)
		_, err := cache.MinerWeight(t.Context(), 1, nodeID)
		require.NoError(t, err)

		// Access epoch 3 which should trigger eviction of epoch 1
		mock.EXPECT().MinerWeight(gomock.Any(), types.EpochID(3), nodeID).Return(weight, nil)
		_, err = cache.MinerWeight(t.Context(), 3, nodeID)
		require.NoError(t, err)

		// Epoch 1 should be evicted, requiring a new service call
		mock.EXPECT().MinerWeight(gomock.Any(), types.EpochID(1), nodeID).Return(weight, nil)
		_, err = cache.MinerWeight(t.Context(), 1, nodeID)
		require.NoError(t, err)
	})
}

func TestTotalWeight(t *testing.T) {
	t.Run("caches successful responses", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mock := NewMockweights(ctrl)
		cache := NewCachedWeights(mock)

		expectedWeight := uint64(1000)

		mock.EXPECT().TotalWeight(gomock.Any(), types.EpochID(1)).Return(expectedWeight, nil)

		// First call should hit the service
		got, err := cache.TotalWeight(t.Context(), 1)
		require.NoError(t, err)
		require.Equal(t, expectedWeight, got)

		// Second call should use cache
		got, err = cache.TotalWeight(t.Context(), 1)
		require.NoError(t, err)
		require.Equal(t, expectedWeight, got)
	})

	t.Run("doesn't cache errors", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mock := NewMockweights(ctrl)
		cache := NewCachedWeights(mock)

		expectedErr := errors.New("total weight not found")

		mock.EXPECT().
			TotalWeight(gomock.Any(), types.EpochID(1)).
			Return(uint64(0), expectedErr).
			Times(2)

		// First call should fail
		_, err := cache.TotalWeight(t.Context(), 1)
		require.Error(t, err)
		require.ErrorIs(t, err, expectedErr)

		// Second call should try again
		_, err = cache.TotalWeight(t.Context(), 1)
		require.Error(t, err)
		require.ErrorIs(t, err, expectedErr)
	})

	t.Run("uses singleflight for concurrent requests", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mock := NewMockweights(ctrl)
		cache := NewCachedWeights(mock)

		expectedWeight := uint64(1000)
		mock.EXPECT().TotalWeight(gomock.Any(), types.EpochID(1)).Return(expectedWeight, nil)

		var eg errgroup.Group
		for range 100 {
			eg.Go(func() error {
				got, err := cache.TotalWeight(t.Context(), 1)
				if err != nil {
					return err
				}
				if got != expectedWeight {
					return fmt.Errorf("got wrong weight, want: %d, got: %d", expectedWeight, got)
				}
				return nil
			})
		}
		require.NoError(t, eg.Wait())
	})

	t.Run("respects LRU cache size", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		mock := NewMockweights(ctrl)
		cache := NewCachedWeights(mock)

		weight := uint64(1000)

		// Fill cache with epochs 1 and 2
		mock.EXPECT().TotalWeight(gomock.Any(), types.EpochID(1)).Return(weight, nil)
		mock.EXPECT().TotalWeight(gomock.Any(), types.EpochID(2)).Return(weight, nil)

		_, err := cache.TotalWeight(t.Context(), 1)
		require.NoError(t, err)
		_, err = cache.TotalWeight(t.Context(), 2)
		require.NoError(t, err)

		// Add epoch 3, should evict epoch 1
		mock.EXPECT().TotalWeight(gomock.Any(), types.EpochID(3)).Return(weight, nil)
		_, err = cache.TotalWeight(t.Context(), 3)
		require.NoError(t, err)

		// Epoch 1 should require new service call
		mock.EXPECT().TotalWeight(gomock.Any(), types.EpochID(1)).Return(weight, nil)
		_, err = cache.TotalWeight(t.Context(), 1)
		require.NoError(t, err)
	})
}
