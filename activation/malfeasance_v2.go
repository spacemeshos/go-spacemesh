package activation

import (
	"context"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/codec"
)

type MalfeasanceHandlerV2 struct {
	syncer    syncer
	clock     layerClock
	publisher malfeasancePublisher
}

func NewMalfeasanceHandlerV2(
	syncer syncer,
	layerClock layerClock,
	malfeasancePublisher malfeasancePublisher,
) *MalfeasanceHandlerV2 {
	return &MalfeasanceHandlerV2{
		syncer:    syncer,
		clock:     layerClock,
		publisher: malfeasancePublisher,
	}
}

func (mh *MalfeasanceHandlerV2) publishATXProof(ctx context.Context, proof *wire.ATXProof) error {
	// TODO(mafa): populate proof with additional certificates

	return mh.publisher.Publish(ctx, codec.MustEncode(proof))
}

func (mp *MalfeasanceHandlerV2) PublishDoublePublishProof(ctx context.Context, atx1, atx2 *wire.ActivationTxV2) error {
	proof, err := wire.NewDoublePublishProof(atx1, atx2)
	if err != nil {
		return fmt.Errorf("create double publish proof: %w", err)
	}

	// TODO(mafa): update node state about malfeasance of the given identity/identities

	if !mp.syncer.ListenToATXGossip() {
		// we are not gossiping proofs when we are not listening to ATX gossip
		return nil
	}

	atxProof := &wire.ATXProof{
		Layer:     mp.clock.CurrentLayer(),
		ProofType: wire.DoublePublish,
		Proof:     codec.MustEncode(proof),
	}
	return mp.publishATXProof(ctx, atxProof)
}
