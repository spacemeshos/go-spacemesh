package system

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/tortoise.go -source=./tortoise.go

// Tortoise is an interface provided by tortoise implementation.
type Tortoise interface {
	OnBallot(*types.Ballot)
	OnBlock(*types.Block)
	OnHareOutput(types.LayerID, types.BlockID)
	HandleIncomingLayer(context.Context, types.LayerID) types.LayerID
}
