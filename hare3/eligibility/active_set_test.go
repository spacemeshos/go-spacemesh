package eligibility

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	gomock "go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	"github.com/spacemeshos/go-spacemesh/system/mocks"
)

func TestResetCache(t *testing.T) {
	db := statesql.InMemoryTest(t)

	ctrl := gomock.NewController(t)
	mBeacon := NewMockBeaconProvider(ctrl)
	mSyncer := mocks.NewMockSyncStateProvider(ctrl)

	cache, err := NewActiveSetCache(mBeacon, db, atxsdata.New(), zaptest.NewLogger(t))
	require.NoError(t, err)
	cache.SetSync(mSyncer)
	cache.cache.Add(1, nil)

	mSyncer.EXPECT().IsSynced(gomock.Any()).Return(false)
	cache.resetCacheOnSynced(context.Background())
	require.True(t, cache.cache.Contains(1))

	mSyncer.EXPECT().IsSynced(gomock.Any()).Return(false)
	cache.resetCacheOnSynced(context.Background())
	require.True(t, cache.cache.Contains(1))

	mSyncer.EXPECT().IsSynced(gomock.Any()).Return(true)
	cache.resetCacheOnSynced(context.Background())
	require.Equal(t, 0, cache.cache.Len())

	cache.cache.Add(1, nil)

	mSyncer.EXPECT().IsSynced(gomock.Any()).Return(true)
	cache.resetCacheOnSynced(context.Background())
	require.True(t, cache.cache.Contains(1))
}
