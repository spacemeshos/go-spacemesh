package gossip

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/stretchr/testify/assert"
	"testing"
)

//todo : more unit tests

func TestNeighborhood_Broadcast(t *testing.T) {
	n := NewNeighborhood(config.DefaultConfig().SwarmConfig, nil, nil, log.New("tesT", "", ""))
	err := n.Broadcast([]byte("msg"))
	assert.Error(t, err)
}
