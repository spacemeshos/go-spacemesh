package monitoring

import "math"

// Tracker tracks uint64 values and provides stats on the tracked values: min, max, avg
type Tracker struct {
	data []uint64
	max  uint64
	min  uint64
	avg  float64
}

// NewTracker returns a new empty tracker
func NewTracker() *Tracker {
	return &Tracker{
		data: make([]uint64, 0),
		max:  0,
		min:  math.MaxUint64,
		avg:  0,
	}
}

// Track the provided value
func (t *Tracker) Track(value uint64) {
	if value > t.max {
		t.max = value
	}

	if value < t.min {
		t.min = value
	}

	count := uint64(len(t.data))
	t.avg = (float64)(count*uint64(t.avg)+value) / (float64)(count+1)

	t.data = append(t.data, value)
}

// Max returns the maximal sample
func (t *Tracker) Max() uint64 {
	return t.max
}

// Min returns the minimal sample
func (t *Tracker) Min() uint64 {
	return t.min
}

// Avg return the average of all samples
func (t *Tracker) Avg() float64 {
	return t.avg
}

// IsEmpty returns true iff no sample has been recorder yet
func (t *Tracker) IsEmpty() bool {
	return len(t.data) == 0
}
