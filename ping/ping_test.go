package ping

import (
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestPing_Ping(t *testing.T) {
	sim := service.NewSimulator()
	node1 := sim.NewNode()
	node2 := sim.NewNode()

	p := New(node1)
	p2 := New(node2)

	pr, err := p.Ping(node2.PublicKey(), "hello")
	assert.NoError(t, err)
	assert.Equal(t, pr, responses["hello"])

	AddResponse("TEST", "T3ST")

	pr, err = p2.Ping(node1.PublicKey(), "TEST")
	assert.NoError(t, err)
	assert.Equal(t, pr, "T3ST")
}
