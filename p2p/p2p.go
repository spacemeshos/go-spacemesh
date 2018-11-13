package p2p

import (
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

// Service is a wrapper for service.Service to expose the Service interface to `p2p` clients
type Service service.Service

// New creates a new P2P service a.k.a `swarm` it tries to load node information from the disk.
func New(config config.Config) (Service, error) {
	return newSwarm(config, config.NewNode, true) // TODO ADD Persist param
}
