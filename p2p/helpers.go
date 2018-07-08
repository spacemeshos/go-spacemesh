package p2p

import (
	"github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
	"github.com/stretchr/testify/assert"
	"testing"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
)

func defaultConfig() nodeconfig.Config {
	return nodeconfig.DefaultConfig()
}

func p2pTestInstance(t testing.TB, config nodeconfig.Config) Swarm {
	port, err := node.GetUnboundedPort()
	assert.NoError(t, err, "Error getting a port", err)
	config.TCPPort = port
	p, err := New(config, false)
	assert.NoError(t, err, "Error creating p2p stack, err: %v", err)
	return p
}
