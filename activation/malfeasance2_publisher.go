package activation

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/common/types"
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

func (p *ATXMalfeasancePublisher) Publish(ctx context.Context, id types.NodeID, proof wire.Proof) error {
	// TODO(mafa): implement me
	return nil
}
