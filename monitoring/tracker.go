package monitoring

import "math"

type Tracker struct {
	data []uint64
	max  uint64
	min  uint64
	avg  float64
}

func NewTracker() *Tracker {
	return &Tracker{
		data: make([]uint64, 0),
		max:  0,
		min:  math.MaxUint64,
		avg:  0,
	}
}

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

func (t *Tracker) Max() uint64 {
	return t.max
}

func (t *Tracker) Min() uint64 {
	return t.min
}

func (t *Tracker) Avg() float64 {
	return t.avg
}

func (t *Tracker) IsEmpty() bool {
	return len(t.data) == 0
}
