// Package p2p provides a networking api for creating p2p protocols by enabling sending direct messages to
// a set of provided neighbors or broadcasting a message to all of them. the discovery, connectivity and encryption is
// completely transparent to consumers. NOTE: gossip protocols must take care of their own message validation.
package p2p

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

// Service is a wrapper for service.Service to expose the Service interface to `p2p` package clients
type Service service.Service

// New creates a new P2P service a.k.a `Switch` it loads existing node information from the disk or creates a new one.
func New(ctx context.Context, config config.Config, logger log.Log, path string) (*Switch, error) {
	return newSwarm(ctx, config, logger, path) // TODO ADD Persist param
}
