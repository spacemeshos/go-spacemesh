package malsync

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/sql/localsql"
)

func TestMalfeasanceSyncState(t *testing.T) {
	db := localsql.InMemoryTest(t)
	timestamp, err := GetSyncState(db)
	require.NoError(t, err)
	require.Equal(t, time.Time{}, timestamp)
	ts := time.Now()
	for i := 0; i < 3; i++ {
		require.NoError(t, UpdateSyncState(db, ts))
		timestamp, err = GetSyncState(db)
		require.NoError(t, err)
		require.Equal(t, ts.Truncate(time.Second), timestamp)
		ts = ts.Add(3 * time.Minute)
	}
	require.NoError(t, Clear(db))
	timestamp, err = GetSyncState(db)
	require.NoError(t, err)
	require.Equal(t, time.Time{}, timestamp)
}
