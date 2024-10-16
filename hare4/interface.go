package hare4

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/signing"
)

//go:generate mockgen -typed -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type streamRequester interface {
	Run(ctx context.Context) error
	StreamRequest(context.Context, p2p.Peer, []byte, server.StreamRequestCallback, ...string) error
}

type verifier interface {
	Verify(signing.Domain, types.NodeID, []byte, types.EdSignature) bool
}
