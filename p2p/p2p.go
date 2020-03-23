// package p2p provides an API to create a p2p networking service.
// it makes implementing protocols on top of it almost agnostic of the underlying network layer.
package p2p

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

// Service is a wrapper for service.Service to expose the Service interface to `p2p` package clients
type Service service.Service

// New creates a new P2P service a.k.a `swarm` it tries to load node information from the disk.
func New(ctx context.Context, config config.Config, logger log.Log, path string) (*swarm, error) {
	return newSwarm(ctx, config, logger, path) // TODO ADD Persist param
}
