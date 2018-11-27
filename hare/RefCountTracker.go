package hare

type RefCountTracker struct {
	table map[BlockId]uint32
}

func NewRefCountTracker() *RefCountTracker {
	t := &RefCountTracker{}
	t.table = make(map[BlockId]uint32, 0)

	return t
}

func (tracker *RefCountTracker) Track(id *BlockId) {
	if _, exist := tracker.table[*id]; !exist {
		tracker.table[*id] = 1
		return
	}

	tracker.table[*id]++
}

func (tracker *RefCountTracker) buildSet(threshold uint32) *Set {
	s := NewEmptySet()
	/*for bid := range tracker.table {
		if bid > threshold {
			s.Add(bid)
		}
	}*/

	return s
}