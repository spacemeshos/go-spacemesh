package monitoring

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestTracker_Avg(t *testing.T) {
	tr := NewTracker()
	tr.Track(1000)
	tr.Track(2000)
	tr.Track(3000)
	assert.Equal(t, uint64(3000), tr.max)
	assert.Equal(t, uint64(1000), tr.min)
	assert.Equal(t, float64(2000), tr.avg)
}
