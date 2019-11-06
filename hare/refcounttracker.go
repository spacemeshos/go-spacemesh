package hare

type objectId uint64

// RefCountTracker tracks the number of references of any object id.
type RefCountTracker struct {
	table map[objectId]uint32
}

// NewRefCountTracker creates a new reference count tracker.
func NewRefCountTracker() *RefCountTracker {
	t := &RefCountTracker{}
	t.table = make(map[objectId]uint32)

	return t
}

// CountStatus returns the number of references to the given id.
func (tracker *RefCountTracker) CountStatus(id objectId) uint32 {
	count, exist := tracker.table[id]
	if !exist {
		return 0
	}

	return count
}

// Track increases the count for the given object id.
func (tracker *RefCountTracker) Track(id objectId) {
	if _, exist := tracker.table[id]; !exist {
		tracker.table[id] = 1
		return
	}

	tracker.table[id]++
}
