package multipeer

import (
	"container/heap"
	"time"

	"github.com/spacemeshos/go-spacemesh/sync2/types"
)

type syncRange struct {
	x, y            types.KeyBytes
	lastSyncStarted time.Time
	done            bool
	numSyncers      int
	index           int
}

type syncQueue []*syncRange

// Len implements heap.Interface.
func (sq syncQueue) Len() int { return len(sq) }

// Less implements heap.Interface.
func (sq syncQueue) Less(i, j int) bool {
	// We want Pop to give us syncRange for which which sync has started the
	// earliest. Items which are not being synced are considered "most earliest"
	return sq[i].lastSyncStarted.Before(sq[j].lastSyncStarted)
}

// Swap implements heap.Interface.
func (sq syncQueue) Swap(i, j int) {
	sq[i], sq[j] = sq[j], sq[i]
	sq[i].index = i
	sq[j].index = j
}

// Push implements heap.Interface.
func (sq *syncQueue) Push(i any) {
	n := len(*sq)
	sr := i.(*syncRange)
	sr.index = n
	*sq = append(*sq, sr)
}

// Pop implements heap.Interface.
func (sq *syncQueue) Pop() any {
	old := *sq
	n := len(old)
	sr := old[n-1]
	old[n-1] = nil // avoid memory leak
	sr.index = -1  // not in the queue anymore
	*sq = old[0 : n-1]
	return sr
}

func newSyncQueue(numPeers, keyLen, maxDepth int) syncQueue {
	delim := getDelimiters(numPeers, keyLen, maxDepth)
	y := make(types.KeyBytes, keyLen)
	sq := make(syncQueue, numPeers)
	for n := range sq {
		x := y.Clone()
		if n < numPeers-1 {
			y = delim[n]
		} else {
			y = make(types.KeyBytes, keyLen)
		}
		sq[n] = &syncRange{
			x: x,
			y: y,
		}
	}
	heap.Init(&sq)
	return sq
}

func (sq *syncQueue) empty() bool {
	return len(*sq) == 0
}

func (sq *syncQueue) popRange() *syncRange {
	if sq.empty() {
		return nil
	}
	sr := heap.Pop(sq).(*syncRange)
	sr.index = -1
	return sr
}

func (sq *syncQueue) pushRange(sr *syncRange) {
	if sr.done {
		panic("BUG: pushing a finished syncRange into the queue")
	}
	if sr.index == -1 {
		heap.Push(sq, sr)
	}
}

func (sq *syncQueue) update(sr *syncRange, lastSyncStarted time.Time) {
	sr.lastSyncStarted = lastSyncStarted
	if sr.index == -1 {
		sq.pushRange(sr)
	} else {
		heap.Fix(sq, sr.index)
	}
}
