package hare

import (
	"bytes"
	"fmt"
)

// RefCountTracker tracks the number of references of any object id.
type RefCountTracker struct {
	table map[interface{}]uint32
}

// NewRefCountTracker creates a new reference count tracker.
func NewRefCountTracker() *RefCountTracker {
	return &RefCountTracker{table: make(map[interface{}]uint32)}
}

// CountStatus returns the number of references to the given id.
func (tracker *RefCountTracker) CountStatus(id interface{}) uint32 {
	return tracker.table[id]
}

// Track increases the count for the given object id.
func (tracker *RefCountTracker) Track(id interface{}, count uint32) {
	tracker.table[id] += count
}

func (tracker *RefCountTracker) String() string {
	if len(tracker.table) == 0 {
		return "(no tracked values)"
	}
	b := new(bytes.Buffer)
	for id, count := range tracker.table {
		b.WriteString(fmt.Sprintf("%v: %d, ", id, count))
	}
	return b.String()[:b.Len()-2]
}
