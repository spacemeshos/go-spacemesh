package activation

import (
	"context"
	"errors"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type MalfeasanceService struct {
	logger *zap.Logger
}

func NewMalfeasanceService(logger *zap.Logger) *MalfeasanceService {
	return &MalfeasanceService{logger: logger}
}

func (ms *MalfeasanceService) Validate(ctx context.Context, data []byte) (types.NodeID, error) {
	return types.EmptyNodeID, errors.New("not implemented")
}
