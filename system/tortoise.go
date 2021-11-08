package system

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/tortoise.go -source=./tortoise.go

// Tortoise interface for receiving layers.
type Tortoise interface {
	HandleIncomingLayer(context.Context, types.LayerID) (types.LayerID, types.LayerID, bool)
	BaseBlock(context.Context) (types.BlockID, [][]types.BlockID, error)
	Persist(ctx context.Context) error
	Stop()
}
