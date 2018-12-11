package p2p

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/stretchr/testify/assert"
	"testing"
)

const DefaultBootCount = 3

func createP2pInstance(t *testing.T, config config.Config) *swarm {
	port, err := node.GetUnboundedPort()
	assert.Nil(t, err)
	config.TCPPort = port
	p, err := newSwarm(context.TODO(), config, true, true)
	assert.Nil(t, err)
	assert.NotNil(t, p)
	return p
}

type P2PSwarm struct {
	boot  []*swarm
	Swarm []*swarm
}

func NewP2PSwarm(t *testing.T, size int) *P2PSwarm {
	boot := make([]*swarm, DefaultBootCount)
	swarm := make([]*swarm, size)

	bootcfg := config.DefaultConfig()
	bootcfg.SwarmConfig.Bootstrap = false
	bootcfg.SwarmConfig.Gossip = false

	// start boot
	for i := 0; i < len(boot); i++ {
		boot[i] = createP2pInstance(t, bootcfg)
		boot[i].Start()
	}

	cfg := config.DefaultConfig()
	cfg.SwarmConfig.Bootstrap = true
	cfg.SwarmConfig.Gossip = false
	cfg.SwarmConfig.BootstrapNodes = StringIdentifiers(boot)

	// start all
	for i := 0; i < len(swarm); i++ {
		swarm[i] = createP2pInstance(t, cfg)
		go swarm[i].Start()
	}

	// wait all
	for i := 0; i < len(swarm); i++ {
		swarm[i].waitForBoot()
	}

	return &P2PSwarm{boot, swarm}
}

func StringIdentifiers(boot []*swarm) []string {
	s := make([]string, len(boot))
	for i := 0; i < len(s); i++ {
		s[i] = node.StringFromNode(boot[i].lNode.Node)
	}

	return s
}
