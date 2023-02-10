package hare

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type MyInt struct {
	val uint32
}

func (m MyInt) ID() uint32 {
	return m.val
}

func TestRefCountTracker_Track(t *testing.T) {
	et := NewEligibilityTracker(5)
	tracker := NewRefCountTracker(preRound, et, 5)
	mi1 := MyInt{1}
	tracker.Track(mi1.ID(), []byte{1})
	require.Equal(t, 1, len(tracker.table))
	mi2 := MyInt{2}
	tracker.Track(mi2.ID(), []byte{2})
	require.Equal(t, 2, len(tracker.table))
}

func TestRefCountTracker_CountStatus_ValueNotTracked(t *testing.T) {
	et := NewEligibilityTracker(5)
	et.Track([]byte{1}, preRound, 1, true)
	tracker := NewRefCountTracker(preRound, et, 5)
	myInt := MyInt{1}
	require.Equal(t, CountInfo{}, *tracker.CountStatus(myInt.ID()))
}

func TestRefCountTracker_CountStatus_NoEligibility(t *testing.T) {
	et := NewEligibilityTracker(5)
	tracker := NewRefCountTracker(preRound, et, 5)
	myInt := MyInt{1}
	require.Equal(t, CountInfo{}, *tracker.CountStatus(myInt.ID()))
	tracker.Track(myInt.ID(), []byte{1})
	require.Equal(t, CountInfo{}, *tracker.CountStatus(myInt.ID()))
	tracker.Track(myInt.ID(), []byte{2})
	require.Equal(t, CountInfo{}, *tracker.CountStatus(myInt.ID()))
}

func TestRefCountTracker_CountStatus_WrongEligibility(t *testing.T) {
	et := NewEligibilityTracker(5)
	tracker := NewRefCountTracker(preRound, et, 5)
	myInt := MyInt{1}
	tracker.Track(myInt.ID(), []byte{1})
	et.Track([]byte{1}, commitRound, 1, true)
	tracker.Track(myInt.ID(), []byte{2})
	et.Track([]byte{2}, commitRound, 2, true)
	require.Equal(t, CountInfo{}, *tracker.CountStatus(myInt.ID()))
}

func TestRefCountTracker_CountStatus_Eligibility(t *testing.T) {
	et := NewEligibilityTracker(5)
	tracker := NewRefCountTracker(preRound, et, 5)
	myInt := MyInt{1}
	tracker.Track(myInt.ID(), []byte{1})
	et.Track([]byte{1}, preRound, 1, false)
	tracker.Track(myInt.ID(), []byte{2})
	et.Track([]byte{2}, preRound, 2, true)
	expected := CountInfo{hCount: 2, dhCount: 1, keCount: 0, numHonest: 1, numDishonest: 1, numKE: 0}
	require.Equal(t, expected, *tracker.CountStatus(myInt.ID()))

	// add a known equivocator without tracking the value
	et.Track([]byte{3}, preRound, 5, false)
	expected = CountInfo{hCount: 2, dhCount: 1, keCount: 5, numHonest: 1, numDishonest: 1, numKE: 1}
	require.Equal(t, expected, *tracker.CountStatus(myInt.ID()))
}
