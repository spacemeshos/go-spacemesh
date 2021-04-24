package hare

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

type MyInt struct {
	val uint32
}

func (m MyInt) ID() uint32 {
	return m.val
}

func TestRefCountTracker_Track(t *testing.T) {
	tracker := NewRefCountTracker()
	mi1 := MyInt{1}
	tracker.Track(mi1.ID(), 1)
	assert.Equal(t, 1, len(tracker.table))
	mi2 := MyInt{2}
	tracker.Track(mi2.ID(), 2)
	assert.Equal(t, 2, len(tracker.table))
}

func TestRefCountTracker_CountStatus(t *testing.T) {
	tracker := NewRefCountTracker()
	myInt := MyInt{1}
	assert.EqualValues(t, 0, tracker.CountStatus(myInt.ID()))
	tracker.Track(myInt.ID(), 1)
	assert.EqualValues(t, 1, tracker.CountStatus(myInt.ID()))
	tracker.Track(myInt.ID(), 2)
	assert.EqualValues(t, 3, tracker.CountStatus(myInt.ID()))
}
