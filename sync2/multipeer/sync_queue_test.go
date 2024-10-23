package multipeer_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/sync2/multipeer"
)

type hexRange [2]string

func TestSyncQueue(t *testing.T) {
	expPeerRanges := map[hexRange]bool{
		{
			"0000000000000000000000000000000000000000000000000000000000000000",
			"4000000000000000000000000000000000000000000000000000000000000000",
		}: false,
		{
			"4000000000000000000000000000000000000000000000000000000000000000",
			"8000000000000000000000000000000000000000000000000000000000000000",
		}: false,
		{
			"8000000000000000000000000000000000000000000000000000000000000000",
			"c000000000000000000000000000000000000000000000000000000000000000",
		}: false,
		{
			"c000000000000000000000000000000000000000000000000000000000000000",
			"0000000000000000000000000000000000000000000000000000000000000000",
		}: false,
	}
	sq := multipeer.NewSyncQueue(4, 32, 24)
	startTime := time.Now()
	pushed := make([]hexRange, 4)
	for i := 0; i < 4; i++ {
		sr := sq.PopRange()
		require.NotNil(t, sr)
		require.True(t, sr.LastSyncStarted.IsZero())
		require.False(t, sr.Done)
		require.Zero(t, sr.NumSyncers)
		k := hexRange{sr.X.String(), sr.Y.String()}
		processed, found := expPeerRanges[k]
		require.True(t, found)
		require.False(t, processed)
		expPeerRanges[k] = true
		t.Logf("push range %v at %v", k, sr.LastSyncStarted)
		if i != 1 {
			sr.LastSyncStarted = startTime
			sq.PushRange(sr) // pushed to the end
		} else {
			// use update for one of the items
			// instead of pushing with proper time
			sq.Update(sr, startTime)
		}
		if i == 0 {
			sq.PushRange(sr) // should do nothing
		}
		startTime = startTime.Add(10 * time.Second)
		pushed[i] = k
	}
	require.Len(t, sq, 4)
	for i := 0; i < 4; i++ {
		sr := sq.PopRange()
		k := hexRange{sr.X.String(), sr.Y.String()}
		t.Logf("pop range %v at %v", k, sr.LastSyncStarted)
		require.Equal(t, pushed[i], k)
	}
	require.Empty(t, sq)
}
