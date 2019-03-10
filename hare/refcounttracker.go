package hare

type objectId uint64

// Tracks the number of references of any identifiable instance
type RefCountTracker struct {
	table map[objectId]uint32
}

func NewRefCountTracker() *RefCountTracker {
	t := &RefCountTracker{}
	t.table = make(map[objectId]uint32)

	return t
}

// Returns the number of references to the given id
func (tracker *RefCountTracker) CountStatus(id objectId) uint32 {
	count, exist := tracker.table[id]
	if !exist {
		return 0
	}

	return count
}

// Tracks count for the provided id
func (tracker *RefCountTracker) Track(id objectId) {
	if _, exist := tracker.table[id]; !exist {
		tracker.table[id] = 1
		return
	}

	tracker.table[id]++
}
