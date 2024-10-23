package multipeer

import (
	"container/heap"
	"time"

	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

// syncRange represents a range of keys to be synchronized, along with the status
// information.
type syncRange struct {
	X, Y            rangesync.KeyBytes
	LastSyncStarted time.Time
	Done            bool
	NumSyncers      int
	Index           int
}

// syncQueue is a priority queue for syncRanges.
type syncQueue []*syncRange

// Len implements heap.Interface.
func (sq syncQueue) Len() int { return len(sq) }

// Less implements heap.Interface.
func (sq syncQueue) Less(i, j int) bool {
	// We want Pop to give us syncRange for which which sync has started the
	// earliest. Items which are not being synced are considered "most earliest"
	return sq[i].LastSyncStarted.Before(sq[j].LastSyncStarted)
}

// Swap implements heap.Interface.
func (sq syncQueue) Swap(i, j int) {
	sq[i], sq[j] = sq[j], sq[i]
	sq[i].Index = i
	sq[j].Index = j
}

// Push implements heap.Interface.
func (sq *syncQueue) Push(i any) {
	n := len(*sq)
	sr := i.(*syncRange)
	sr.Index = n
	*sq = append(*sq, sr)
}

// Pop implements heap.Interface.
func (sq *syncQueue) Pop() any {
	old := *sq
	n := len(old)
	sr := old[n-1]
	old[n-1] = nil // avoid memory leak
	sr.Index = -1  // not in the queue anymore
	*sq = old[0 : n-1]
	return sr
}

func newSyncQueue(numPeers, keyLen, maxDepth int) syncQueue {
	delim := getDelimiters(numPeers, keyLen, maxDepth)
	y := make(rangesync.KeyBytes, keyLen)
	sq := make(syncQueue, numPeers)
	for n := range sq {
		x := y.Clone()
		if n < numPeers-1 {
			y = delim[n]
		} else {
			y = make(rangesync.KeyBytes, keyLen)
		}
		sq[n] = &syncRange{
			X: x,
			Y: y,
		}
	}
	heap.Init(&sq)
	return sq
}

func (sq *syncQueue) empty() bool {
	return len(*sq) == 0
}

func (sq *syncQueue) PopRange() *syncRange {
	if sq.empty() {
		return nil
	}
	sr := heap.Pop(sq).(*syncRange)
	sr.Index = -1
	return sr
}

func (sq *syncQueue) PushRange(sr *syncRange) {
	if sr.Done {
		panic("BUG: pushing a finished syncRange into the queue")
	}
	if sr.Index == -1 {
		heap.Push(sq, sr)
	}
}

func (sq *syncQueue) Update(sr *syncRange, lastSyncStarted time.Time) {
	sr.LastSyncStarted = lastSyncStarted
	if sr.Index == -1 {
		sq.PushRange(sr)
	} else {
		heap.Fix(sq, sr.Index)
	}
}
