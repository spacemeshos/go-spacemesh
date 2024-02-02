package malfeasance

import (
	"context"

	"github.com/spacemeshos/post/shared"
	"github.com/spacemeshos/post/verifying"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

//go:generate mockgen -typed -package=malfeasance -destination=./mocks.go -source=./interface.go

type SigVerifier interface {
	Verify(signing.Domain, types.NodeID, []byte, types.EdSignature) bool
}

type tortoise interface {
	OnMalfeasance(types.NodeID)
}

type postVerifier interface {
	Verify(ctx context.Context, p *shared.Proof, m *shared.ProofMetadata, opts ...verifying.OptionFunc) error
}
