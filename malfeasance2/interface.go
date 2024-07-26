package malfeasance2

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -typed -package=malfeasance2 -destination=./mocks.go -source=./interface.go

type tortoise interface {
	OnMalfeasance(types.NodeID)
}

type MalfeasanceHandler interface {
	Validate(ctx context.Context, data []byte) (types.NodeID, error)
}
