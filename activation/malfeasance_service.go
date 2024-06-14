package activation

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

type MalfeasanceService struct {
	logger     *zap.Logger
	edVerifier *signing.EdVerifier
}

func NewMalfeasanceService(logger *zap.Logger, edVerifier *signing.EdVerifier) *MalfeasanceService {
	return &MalfeasanceService{
		logger:     logger,
		edVerifier: edVerifier,
	}
}

func (ms *MalfeasanceService) Validate(ctx context.Context, data []byte) ([]types.NodeID, error) {
	var decoded wire.ATXProof
	if err := codec.Decode(data, &decoded); err != nil {
		return nil, fmt.Errorf("decoding ATX malfeasance proof: %w", err)
	}

	var proof wire.Proof
	switch decoded.ProofType {
	case wire.DoublePublish:
		var p wire.ProofDoublePublish
		if err := codec.Decode(decoded.Proof, &p); err != nil {
			return nil, fmt.Errorf("decoding ATX double publish proof: %w", err)
		}
		proof = p
	case wire.DoubleMarry:
		var p wire.ProofDoubleMarry
		if err := codec.Decode(decoded.Proof, &p); err != nil {
			return nil, fmt.Errorf("decoding ATX double marry proof: %w", err)
		}
		proof = p
	default:
		return nil, fmt.Errorf("unknown ATX malfeasance proof type: %d", decoded.ProofType)
	}

	id, err := proof.Valid(ms.edVerifier)
	if err != nil {
		return nil, fmt.Errorf("validating ATX malfeasance proof: %w", err)
	}

	validIDs := make([]types.NodeID, 0, len(decoded.Certificates)+1)
	validIDs = append(validIDs, id) // id has already been proven to be malfeasant

	// check certificates provided with the proof
	// TODO(mafa): this only works if the main identity becomes malfeasant - try different approach with merkle proofs
	for _, cert := range decoded.Certificates {
		if id != cert.Target {
			continue
		}
		if !ms.edVerifier.Verify(signing.MARRIAGE, cert.Target, cert.ID.Bytes(), cert.Signature) {
			continue
		}
		validIDs = append(validIDs, cert.ID)
	}
	return validIDs, nil
}

func (ms *MalfeasanceService) Publish(ctx context.Context, proof wire.ATXProof) error {
	// TODO(mafa): this is called by the ATX handler in the activation package
	//
	// encode proof to []byte
	// bubble up to malfeasance handler to encode as `MalfeasanceProofV2` and publish
	return errors.New("not implemented")
}
