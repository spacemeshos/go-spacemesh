package multipeer

import (
	"container/list"
	"sync"
	"time"

	"github.com/jonboulle/clockwork"
)

// syncList keeps track of recent full syncs and reports whether the node is synced, that
// is, the specified number of syncs has happened within the specified duration of time
type syncList struct {
	mtx          sync.Mutex
	clock        clockwork.Clock
	minSyncCount int
	duration     time.Duration
	syncs        list.List
}

func newSyncList(clock clockwork.Clock, minSyncCount int, duration time.Duration) *syncList {
	return &syncList{
		clock:        clock,
		minSyncCount: minSyncCount,
		duration:     duration,
	}
}

func (sl *syncList) prune(now time.Time) {
	t := now.Add(-sl.duration)
	for sl.syncs.Len() != 0 {
		el := sl.syncs.Back()
		if t.After(el.Value.(time.Time)) {
			sl.syncs.Remove(el)
		} else {
			break
		}
	}
}

func (sl *syncList) noteSync() {
	sl.mtx.Lock()
	defer sl.mtx.Unlock()
	now := sl.clock.Now()
	sl.prune(now)
	sl.syncs.PushFront(now)
}

func (sl *syncList) synced() bool {
	sl.mtx.Lock()
	defer sl.mtx.Unlock()
	sl.prune(sl.clock.Now())
	return sl.syncs.Len() >= sl.minSyncCount
}
