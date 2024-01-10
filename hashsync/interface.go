package hashsync

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
)

type requester interface {
	Run(context.Context) error
	InteractiveRequest(context.Context, p2p.Peer, []byte, server.InteractiveHandler, func(error)) error
}
