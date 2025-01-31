package beacon

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type mockBeaconService struct {
	beacons map[types.EpochID]types.Beacon
	calls   int
}

func newMockBeaconService() *mockBeaconService {
	return &mockBeaconService{
		beacons: make(map[types.EpochID]types.Beacon),
	}
}

func (m *mockBeaconService) Beacon(_ context.Context, epoch types.EpochID) (types.Beacon, error) {
	m.calls++
	if b, ok := m.beacons[epoch]; ok {
		return b, nil
	}
	return types.Beacon{}, errors.New("beacon not found")
}

func TestBeaconCache(t *testing.T) {
	t.Run("caches successful responses", func(t *testing.T) {
		mock := newMockBeaconService()
		cache := NewBeaconCache(mock)

		expected := types.Beacon{1, 2, 3, 4}
		mock.beacons[1] = expected

		// First call should hit the service
		got, err := cache.Beacon(context.Background(), 1)
		require.NoError(t, err)
		require.Equal(t, expected, got)
		require.Equal(t, 1, mock.calls)

		// Second call should use cache
		got, err = cache.Beacon(context.Background(), 1)
		require.NoError(t, err)
		require.Equal(t, expected, got)
		require.Equal(t, 1, mock.calls) // calls count shouldn't increase
	})

	t.Run("doesn't cache errors", func(t *testing.T) {
		mock := newMockBeaconService()
		cache := NewBeaconCache(mock)

		// First call should fail
		_, err := cache.Beacon(context.Background(), 1)
		require.Error(t, err)
		require.Equal(t, 1, mock.calls)

		// Second call should try again
		_, err = cache.Beacon(context.Background(), 1)
		require.Error(t, err)
		require.Equal(t, 2, mock.calls)
	})

	t.Run("evicts old entries", func(t *testing.T) {
		mock := newMockBeaconService()
		cache := NewBeaconCache(mock)

		// Add beacons for epochs 0-6
		for e := range types.EpochID(7) {
			mock.beacons[e] = types.Beacon{byte(e), 0, 0, 0}
			_, err := cache.Beacon(context.Background(), e)
			require.NoError(t, err)
		}

		// Epoch 2 should be evicted (keeping last 4: 3,4,5,6)
		initialCalls := mock.calls
		_, err := cache.Beacon(context.Background(), 2)
		require.NoError(t, err)
		require.Equal(t, initialCalls+1, mock.calls, "should need to fetch evicted epoch")

		// Epoch 5 should still be cached
		_, err = cache.Beacon(context.Background(), 5)
		require.NoError(t, err)
		require.Equal(t, initialCalls+1, mock.calls, "should not need to fetch cached epoch")
	})
}
