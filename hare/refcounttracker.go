package hare

type RefCountTracker struct {
	table map[uint32]uint32
}

func NewRefCountTracker(size uint32) *RefCountTracker {
	t := &RefCountTracker{}
	t.table = make(map[uint32]uint32, size)

	return t
}

func (tracker *RefCountTracker) CountStatus(id Identifiable) uint32 {
	count, exist := tracker.table[id.Id()]
	if !exist {
		return 0
	}

	return count
}

func (tracker *RefCountTracker) Track(id Identifiable) {
	if _, exist := tracker.table[id.Id()]; !exist {
		tracker.table[id.Id()] = 1
		return
	}

	tracker.table[id.Id()]++
}