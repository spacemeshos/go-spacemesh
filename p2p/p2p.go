package p2p

import (
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

type Service service.Service

// New creates a new P2P service a.k.a `swarm` it tries to load node information from the disk.
func New(config config.Config) (*swarm, error) {
	return newSwarm(config, true)
}
