package atxsync

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
)

func TestSyncState(t *testing.T) {
	db := localsql.InMemory()
	for epoch := types.EpochID(1); epoch < types.EpochID(5); epoch++ {
		state, err := GetSyncState(db, epoch)
		require.NoError(t, err)
		require.Empty(t, state)

		states := map[types.ATXID]int{}
		const size = 100
		for i := 0; i < size; i++ {
			id := types.ATXID{}
			binary.BigEndian.PutUint64(id[:], uint64(i))
			states[id] = 0
		}
		require.NoError(t, SaveSyncState(db, epoch, states, 1))

		state, err = GetSyncState(db, epoch)
		require.NoError(t, err)

		require.Len(t, state, size)
		const max = 10
		for i := 0; i < size; i++ {
			id := types.ATXID{}
			binary.BigEndian.PutUint64(id[:], uint64(i))
			requests, exists := state[id]
			require.True(t, exists)
			require.Equal(t, 0, requests)
			if i < size/2 {
				state[id] = 1
			} else {
				state[id] = max
			}
		}

		require.NoError(t, SaveSyncState(db, epoch, state, max))

		updated, err := GetSyncState(db, epoch)
		require.NoError(t, err)
		for i := 0; i < size; i++ {
			id := types.ATXID{}
			binary.BigEndian.PutUint64(id[:], uint64(i))
			if i >= size/2 {
				delete(state, id)
			}
		}
		require.Equal(t, state, updated)

	}
}

func TestRequestTime(t *testing.T) {
	db := localsql.InMemory()
	for epoch := types.EpochID(1); epoch < types.EpochID(5); epoch++ {
		timestamp, err := GetRequestTime(db, epoch)
		require.ErrorIs(t, err, sql.ErrNotFound)
		require.True(t, timestamp.IsZero())

		for step := time.Duration(0); step < 10*time.Second; step += time.Second {
			now := time.Now().Add(step)
			require.NoError(t, SaveRequestTime(db, epoch, now))

			timestamp, err = GetRequestTime(db, epoch)
			require.NoError(t, err)
			// now is truncated to a multiple of seconds, as we discard nanonesconds when saving request time
			require.Equal(t, now.Truncate(time.Second), timestamp)
		}
	}
}

func TestClear(t *testing.T) {
	db := localsql.InMemory()
	// add some state
	for epoch := types.EpochID(1); epoch < types.EpochID(5); epoch++ {
		states := map[types.ATXID]int{}
		const size = 100
		for i := 0; i < size; i++ {
			id := types.ATXID{}
			binary.BigEndian.PutUint64(id[:], uint64(i))
			states[id] = 0
		}
		require.NoError(t, SaveSyncState(db, epoch, states, 1))
		require.NoError(t, SaveRequestTime(db, epoch, time.Now()))
	}
	require.NoError(t, Clear(db))
	for epoch := types.EpochID(1); epoch < types.EpochID(5); epoch++ {
		state, err := GetSyncState(db, epoch)
		require.NoError(t, err)
		require.Empty(t, state)
	}
}
