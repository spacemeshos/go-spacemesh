package hare

type id uint32

type RefCountTracker struct {
	table map[id]uint32
}

func NewRefCountTracker() *RefCountTracker {
	t := &RefCountTracker{}
	t.table = make(map[id]uint32)

	return t
}

func (tracker *RefCountTracker) CountStatus(id id) uint32 {
	count, exist := tracker.table[id]
	if !exist {
		return 0
	}

	return count
}

func (tracker *RefCountTracker) Track(id id) {
	if _, exist := tracker.table[id]; !exist {
		tracker.table[id] = 1
		return
	}

	tracker.table[id]++
}
