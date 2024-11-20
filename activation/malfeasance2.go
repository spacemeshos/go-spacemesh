package activation

import (
	"context"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

type MalfeasanceHandlerV2 struct {
	malPublisher malfeasancePublisher
	edVerifier   *signing.EdVerifier
	validator    nipostValidatorV2
}

func NewMalfeasanceHandlerV2(
	malPublisher malfeasancePublisher,
	edVerifier *signing.EdVerifier,
	validator nipostValidatorV2,
) *MalfeasanceHandlerV2 {
	return &MalfeasanceHandlerV2{
		malPublisher: malPublisher,
		edVerifier:   edVerifier,
		validator:    validator,
	}
}

// Publish publishes an ATX proof by encoding it and sending it to the malfeasance publisher.
func (p *MalfeasanceHandlerV2) Publish(ctx context.Context, nodeID types.NodeID, proof wire.Proof) error {
	atxProof := &wire.ATXProof{
		Version:   0x01, // for now we only have one version
		ProofType: proof.Type(),

		Proof: codec.MustEncode(proof),
	}

	return p.malPublisher.PublishATXProof(ctx, nodeID, codec.MustEncode(atxProof))
}

func (mh *MalfeasanceHandlerV2) PostIndex(
	ctx context.Context,
	smesherID types.NodeID,
	commitment types.ATXID,
	post *types.Post,
	challenge []byte,
	numUnits uint32,
	idx int,
) error {
	return mh.validator.PostV2(ctx, smesherID, commitment, post, challenge, numUnits, PostIndex(idx))
}

func (mh *MalfeasanceHandlerV2) Signature(d signing.Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
	return mh.edVerifier.Verify(d, nodeID, m, sig)
}

// TODO(mafa): call this validate in the malfeasance handler in `malfeasance` package for publish/gossip:
//   - do not publishing proofs for identities managed by node
//   - validate and persist before publishing
//   - do not handle incoming proofs from peer == `self`
func (mh *MalfeasanceHandlerV2) Validate(ctx context.Context, data []byte) (types.NodeID, error) {
	var atxProof wire.ATXProof
	if err := codec.Decode(data, &atxProof); err != nil {
		return types.EmptyNodeID, fmt.Errorf("decoding ATX malfeasance proof: %w", err)
	}

	proof, err := atxProof.Decode()
	if err != nil {
		return types.EmptyNodeID, fmt.Errorf("decoding ATX malfeasance proof: %w", err)
	}

	id, err := proof.Valid(ctx, mh)
	if err != nil {
		return types.EmptyNodeID, fmt.Errorf("validating ATX malfeasance proof: %w", err)
	}
	return id, nil
}

// TODO(mafa): this roughly how the general publisher looks like
//
// func Publish(ctx context.Context, smesherID types.NodeID, data []byte) error {
// 	// Combine IDs from the present equivocation set for atx.SmesherID and IDs in atx.Marriages.
// 	set, err := identities.EquivocationSet(mh.cdb, nodeID)
// 	if err != nil {
// 		return fmt.Errorf("getting equivocation set: %w", err)
// 	}
// 	for _, id := range set {
// 		if err := identities.SetMalicious(mh.cdb, id, encoded, time.Now()); err != nil {
// 			return fmt.Errorf("adding malfeasance proof: %w", err)
// 		}

// 		mh.cdb.CacheMalfeasanceProof(id, proof)
// 		mh.tortoise.OnMalfeasance(id)
// 	}

// 	if !mh.syncer.ListenToATXGossip() {
//      // we are not gossiping proofs when we are not listening to ATX gossip
// 		return nil
// 	}

// 	gossip := mwire.MalfeasanceProofV2{
// 		Layer:     mh.clock.CurrentLayer(),
// 		ProofType: mwire.InvalidActivation,
// 		Proof:     data,
// 	}

// 	if err := mh.publisher.Publish(ctx, pubsub.MalfeasanceProof, codec.MustEncode(&gossip)); err != nil {
// 		mh.logger.Error("failed to broadcast malfeasance proof", zap.Error(err))
// 		return fmt.Errorf("broadcast atx malfeasance proof: %w", err)
// 	}
// 	return nil
// }
