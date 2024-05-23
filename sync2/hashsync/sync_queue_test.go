package hashsync

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type hexRange [2]string

func TestSyncQueue(t *testing.T) {
	expPeerRanges := map[hexRange]bool{
		{
			"0x0000000000000000000000000000000000000000000000000000000000000000",
			"0x4000000000000000000000000000000000000000000000000000000000000000",
		}: false,
		{
			"0x4000000000000000000000000000000000000000000000000000000000000000",
			"0x8000000000000000000000000000000000000000000000000000000000000000",
		}: false,
		{
			"0x8000000000000000000000000000000000000000000000000000000000000000",
			"0xc000000000000000000000000000000000000000000000000000000000000000",
		}: false,
		{
			"0xc000000000000000000000000000000000000000000000000000000000000000",
			"0x0000000000000000000000000000000000000000000000000000000000000000",
		}: false,
	}
	sq := newSyncQueue(4)
	startTime := time.Now()
	pushed := make([]hexRange, 4)
	for i := 0; i < 4; i++ {
		sr := sq.popRange()
		require.NotNil(t, sr)
		require.True(t, sr.lastSyncStarted.IsZero())
		require.False(t, sr.done)
		require.Zero(t, sr.numSyncers)
		k := hexRange{sr.x.Hex(), sr.y.Hex()}
		processed, found := expPeerRanges[k]
		require.True(t, found)
		require.False(t, processed)
		expPeerRanges[k] = true
		t.Logf("push range %v at %v", k, sr.lastSyncStarted)
		if i != 1 {
			sr.lastSyncStarted = startTime
			sq.pushRange(sr) // pushed to the end
		} else {
			// use update for one of the items
			// instead of pushing with proper time
			sq.update(sr, startTime)
		}
		if i == 0 {
			sq.pushRange(sr) // should do nothing
		}
		startTime = startTime.Add(10 * time.Second)
		pushed[i] = k
	}
	require.Len(t, sq, 4)
	for i := 0; i < 4; i++ {
		sr := sq.popRange()
		k := hexRange{sr.x.Hex(), sr.y.Hex()}
		t.Logf("pop range %v at %v", k, sr.lastSyncStarted)
		require.Equal(t, pushed[i], k)
	}
	require.Empty(t, sq)
}
