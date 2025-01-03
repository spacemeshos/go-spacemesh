package events_test

import (
	"cmp"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/events"
)

func TestInsertEventsAndIterate(t *testing.T) {
	db := localsql.InMemoryTest(t)

	type event struct {
		id        types.NodeID
		timestamp time.Time
		event     []byte
	}
	var allEvents []event
	for i := range 10 {
		e := event{
			id:        types.RandomNodeID(),
			timestamp: time.Unix(int64(i), 0),
			event:     types.RandomBytes(10),
		}
		allEvents = append(allEvents, e)
		require.NoError(t, events.InsertEvent(db, e.id, e.timestamp, e.event))
	}

	slices.SortFunc(allEvents, func(a, b event) int { return cmp.Compare(a.timestamp.Unix(), b.timestamp.Unix()) })
	var counter int
	events.IterateAllEvents(db, func(id types.NodeID, time time.Time, e []byte) bool {
		got := event{
			id:        id,
			timestamp: time,
			event:     e,
		}
		require.Equal(t, allEvents[counter], got)
		counter += 1
		return true
	})
	require.Equal(t, len(allEvents), counter)

	for _, e := range allEvents {
		var count int
		events.IterateEventsForID(db, e.id, func(timestamp time.Time, eventBytes []byte) bool {
			require.Equal(t, e.timestamp, timestamp)
			require.Equal(t, e.event, eventBytes)
			count += 1
			return true
		})
		require.Equal(t, 1, count)
	}
}
