package activation

import (
	"context"
	"errors"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

type MalfeasanceService struct {
	logger *zap.Logger
}

func NewMalfeasanceService(logger *zap.Logger) *MalfeasanceService {
	return &MalfeasanceService{logger: logger}
}

func (ms *MalfeasanceService) Validate(ctx context.Context, data []byte) ([]types.NodeID, error) {
	// TODO(mafa): this is called by the handler in the malfeasance package if the proof is of type `InvalidActivation`
	//
	// decode to wire.ATXProof
	// decode []byte to proof based on proof type
	// call proof.Validate()
	// if proof is valid, check certificates
	// return node IDs for all valid certificates
	return nil, errors.New("not implemented")
}

func (ms *MalfeasanceService) Publish(ctx context.Context, proof wire.ATXProof) error {
	// TODO(mafa): this is called by the ATX handler in the activation package
	//
	// encode proof to []byte
	// bubble up to malfeasance handler to encode as `MalfeasanceProofV2` and publish
	return errors.New("not implemented")
}
