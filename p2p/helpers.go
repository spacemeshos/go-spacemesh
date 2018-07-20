package p2p

import (
	"testing"

	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/stretchr/testify/assert"
)

func defaultConfig() config.Config {
	return config.DefaultConfig()
}

func p2pTestInstance(t testing.TB, config config.Config) *swarm {
	port, err := node.GetUnboundedPort()
	assert.NoError(t, err, "Error getting a port", err)
	config.TCPPort = port
	p, err := newSwarm(config, false)
	assert.NoError(t, err, "Error creating p2p stack, err: %v", err)
	return p
}
