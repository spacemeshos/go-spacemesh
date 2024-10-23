package multipeer_test

import (
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/sync2/multipeer"
)

func TestSyncList(t *testing.T) {
	clk := clockwork.NewFakeClock()
	sl := multipeer.NewSyncList(clk, 3, 5*time.Minute)
	require.False(t, sl.Synced())
	sl.NoteSync()
	require.False(t, sl.Synced())
	clk.Advance(time.Minute)
	sl.NoteSync()
	require.False(t, sl.Synced())
	clk.Advance(time.Minute)
	sl.NoteSync()
	require.True(t, sl.Synced())
	clk.Advance(time.Minute)
	// 3 minutes have passed
	require.True(t, sl.Synced())
	clk.Advance(2*time.Minute + 30*time.Second)
	// 5 minutes 30 s have passed
	require.False(t, sl.Synced())
	sl.NoteSync()
	require.True(t, sl.Synced())
	// make sure the list is pruned and is not growing indefinitely
	require.Equal(t, 3, sl.Len())
}
