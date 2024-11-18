package activation

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/codec"
)

// ATXMalfeasancePublisher is the publisher for ATX proofs.
type ATXMalfeasancePublisher struct {
	malPublisher malfeasancePublisher
}

func NewATXMalfeasancePublisher(
	malPublisher malfeasancePublisher,
) *ATXMalfeasancePublisher {
	return &ATXMalfeasancePublisher{
		malPublisher: malPublisher,
	}
}

// Publish publishes an ATX proof by encoding it and sending it to the malfeasance publisher.
func (p *ATXMalfeasancePublisher) Publish(ctx context.Context, proof wire.Proof) error {
	atxProof := &wire.ATXProof{
		Version:   0x01, // for now we only have one version
		ProofType: proof.Type(),

		Proof: codec.MustEncode(proof),
	}

	return p.malPublisher.PublishATXProof(ctx, codec.MustEncode(atxProof))
}
