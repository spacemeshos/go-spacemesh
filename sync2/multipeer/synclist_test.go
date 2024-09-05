package multipeer

import (
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/require"
)

func TestSyncList(t *testing.T) {
	clk := clockwork.NewFakeClock()
	sl := newSyncList(clk, 3, 5*time.Minute)
	require.False(t, sl.synced())
	sl.noteSync()
	require.False(t, sl.synced())
	clk.Advance(time.Minute)
	sl.noteSync()
	require.False(t, sl.synced())
	clk.Advance(time.Minute)
	sl.noteSync()
	require.True(t, sl.synced())
	clk.Advance(time.Minute)
	// 3 minutes have passed
	require.True(t, sl.synced())
	clk.Advance(2*time.Minute + 30*time.Second)
	// 5 minutes 30 s have passed
	require.False(t, sl.synced())
	sl.noteSync()
	require.True(t, sl.synced())
	// make sure the list is pruned and is not growing indefinitely
	require.Equal(t, 3, sl.syncs.Len())
}
