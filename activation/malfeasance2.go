package activation

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

// MalfeasancePublisher is the publisher for ATX proofs.
type MalfeasancePublisher struct{}

func (p *MalfeasancePublisher) Publish(ctx context.Context, id types.NodeID, proof *wire.ATXProof) error {
	// TODO(mafa): implement me
	return nil
}
