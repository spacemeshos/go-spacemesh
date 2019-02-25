package hare

type objectId uint32

type RefCountTracker struct {
	table map[objectId]uint32
}

func NewRefCountTracker() *RefCountTracker {
	t := &RefCountTracker{}
	t.table = make(map[objectId]uint32)

	return t
}

func (tracker *RefCountTracker) CountStatus(id objectId) uint32 {
	count, exist := tracker.table[id]
	if !exist {
		return 0
	}

	return count
}

func (tracker *RefCountTracker) Track(id objectId) {
	if _, exist := tracker.table[id]; !exist {
		tracker.table[id] = 1
		return
	}

	tracker.table[id]++
}
