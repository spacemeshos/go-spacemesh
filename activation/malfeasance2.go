package activation

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
)

type MalfeasanceHandlerV2 struct {
	logger *zap.Logger

	malPublisher malfeasancePublisher
	edVerifier   *signing.EdVerifier
	validator    nipostValidatorV2

	smeshingMutex sync.Mutex
	signers       map[types.NodeID]*signing.EdSigner
}

func NewMalfeasanceHandlerV2(
	logger *zap.Logger,
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

func (p *MalfeasanceHandlerV2) Register(sig *signing.EdSigner) {
	p.smeshingMutex.Lock()
	defer p.smeshingMutex.Unlock()
	if _, exists := p.signers[sig.NodeID()]; exists {
		p.logger.Error("signing key already registered", log.ZShortStringer("id", sig.NodeID()))
		return
	}

	p.logger.Info("registered signing key", log.ZShortStringer("id", sig.NodeID()))
	p.signers[sig.NodeID()] = sig
}

// Publish publishes an ATX proof by encoding it and sending it to the malfeasance publisher.
func (p *MalfeasanceHandlerV2) Publish(ctx context.Context, nodeID types.NodeID, proof wire.Proof) error {
	proofNodeID, err := proof.Valid(ctx, p)
	if err != nil {
		return fmt.Errorf("publish ATX malfeasance proof: proof not valid: %w", err)
	}
	if proofNodeID != nodeID {
		return fmt.Errorf("publish ATX malfeasance proof: proof for %s does not match node ID %s", proofNodeID, nodeID)
	}

	p.smeshingMutex.Lock()
	_, exists := p.signers[nodeID]
	p.smeshingMutex.Unlock()

	if exists {
		// do not publish proofs against one self
		return fmt.Errorf("publish ATX malfeasance proof: node %s is managed by node", nodeID)
	}

	atxProof := &wire.ATXProof{
		Version:   0x01, // for now we only have one version
		ProofType: proof.Type(),

		Proof: codec.MustEncode(proof),
	}
	return p.malPublisher.PublishATXProof(ctx, nodeID, codec.MustEncode(atxProof))
}

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
