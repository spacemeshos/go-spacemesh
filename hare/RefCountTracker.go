package hare

type RefCountTracker struct {
	table map[uint32]uint32
}

func NewRefCountTracker(size uint32) *RefCountTracker {
	t := &RefCountTracker{}
	t.table = make(map[uint32]uint32, size)

	return t
}

func (tracker *RefCountTracker) CountStatus(id BlockId) uint32 {
	return tracker.table[id.Id()]
}

func (tracker *RefCountTracker) Track(id Identifiable) {
	if _, exist := tracker.table[id.Id()]; !exist {
		tracker.table[id.Id()] = 1
		return
	}

	tracker.table[id.Id()]++
}

// TODO: probably not needed
func (tracker *RefCountTracker) buildSet(threshold uint32) *Set {
	s := NewEmptySet()
	/*for bid := range tracker.table {
		if bid > threshold {
			s.Add(bid)
		}
	}*/

	return s
}