package beacon

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func TestBeaconCache(t *testing.T) {
	t.Run("caches successful responses", func(t *testing.T) {
		mock := NewMockbeaconService(gomock.NewController(t))
		cache := NewBeaconCache(mock)

		expected := types.Beacon{1, 2, 3, 4}
		mock.EXPECT().Beacon(gomock.Any(), types.EpochID(1)).Return(expected, nil)

		// First call should hit the service
		got, err := cache.Beacon(t.Context(), 1)
		require.NoError(t, err)
		require.Equal(t, expected, got)

		// Second call should use cache
		got, err = cache.Beacon(t.Context(), 1)
		require.NoError(t, err)
		require.Equal(t, expected, got)
	})

	t.Run("doesn't cache errors", func(t *testing.T) {
		mock := NewMockbeaconService(gomock.NewController(t))
		cache := NewBeaconCache(mock)

		expectedErr := errors.New("beacon not found")
		mock.EXPECT().
			Beacon(gomock.Any(), types.EpochID(1)).
			Return(types.Beacon{}, expectedErr).
			Times(2)

		// First call should fail
		_, err := cache.Beacon(t.Context(), 1)
		require.ErrorIs(t, err, expectedErr)

		// Second call should try again
		_, err = cache.Beacon(t.Context(), 1)
		require.ErrorIs(t, err, expectedErr)
	})

	t.Run("evicts old entries", func(t *testing.T) {
		mock := NewMockbeaconService(gomock.NewController(t))
		cache := NewBeaconCache(mock)

		// Add beacons for epochs 0-6
		for e := types.EpochID(0); e < 7; e++ {
			mock.EXPECT().Beacon(gomock.Any(), e).Return(types.Beacon{byte(e), 0, 0, 0}, nil)
			_, err := cache.Beacon(t.Context(), e)
			require.NoError(t, err)
		}

		// Epoch 2 should be evicted (keeping last 4: 3,4,5,6)
		mock.EXPECT().Beacon(gomock.Any(), types.EpochID(2)).Return(types.Beacon{2, 0, 0, 0}, nil)
		got, err := cache.Beacon(t.Context(), 2)
		require.NoError(t, err)
		require.Equal(t, types.Beacon{2, 0, 0, 0}, got)

		// Epoch 5 should still be cached
		got, err = cache.Beacon(t.Context(), 5)
		require.NoError(t, err)
		require.Equal(t, types.Beacon{5, 0, 0, 0}, got)
	})
}
