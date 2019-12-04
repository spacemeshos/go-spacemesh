package gossip

import (
	"github.com/stretchr/testify/require"
	"testing"
)

var (
	name1 = "name1"
	name2 = "name2"
	name3 = "name3"
)

func TestPriorityQ_Set(t *testing.T) {
	r := require.New(t)
	pq := NewPriorityQ(10)
	r.NoError(pq.Set(name1, 0, 10))
}
