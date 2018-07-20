package ping

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/simulator"
	"github.com/spacemeshos/go-spacemesh/ping/pb"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestNew(t *testing.T) {
	p := New(&p2p.Mock{})
	assert.NotNil(t, p)
}

func Test_sendResponse(t *testing.T) {
	p := New(&p2p.Mock{})
	p.sendRequest("1", crypto.NewUUID(), &pb.Ping{Message: "LOL"})
}

func TestPing_Ping(t *testing.T) {
	p := New(&p2p.Mock{})
	_, err := p.Ping("", "")
	assert.Error(t, err)
}

func TestPing_Ping2(t *testing.T) {
	sim := simulator.New()
	node1 := sim.NewNode()
	node2 := sim.NewNode()

	p := New(node1)
	_ = New(node2)

	pr, err := p.Ping(node2.String(), "hello")

	assert.NoError(t, err)

	assert.Equal(t, pr, responses["hello"])
}
