package hare

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

type MyInt struct {
	val uint32
}

func (m MyInt) Id() uint32 {
	return m.val
}

func TestRefCountTracker_Track(t *testing.T) {
	tracker := NewRefCountTracker(10)
	tracker.Track(MyInt{1})
	assert.Equal(t, 1, len(tracker.table))
	tracker.Track(MyInt{2})
	assert.Equal(t, 2, len(tracker.table))
}

func TestRefCountTracker_CountStatus(t *testing.T) {
	tracker := NewRefCountTracker(10)
	myInt := MyInt{1}
	assert.Equal(t, uint32(0), tracker.CountStatus(myInt))
	tracker.Track(myInt)
	assert.Equal(t, uint32(1), tracker.CountStatus(myInt))
	tracker.Track(myInt)
	assert.Equal(t, uint32(2), tracker.CountStatus(myInt))
}
