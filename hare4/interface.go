package hare4

import (
	"context"
	"io"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/signing"
)

//go:generate mockgen -typed -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type streamRequester interface {
	RunProto(ctx context.Context) error
	NewStream(context.Context, p2p.Peer) (io.ReadWriteCloser, error)
}

type verifier interface {
	Verify(signing.Domain, types.NodeID, []byte, types.EdSignature) bool
}
