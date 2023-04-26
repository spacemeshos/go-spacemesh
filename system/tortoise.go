package system

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/tortoise/result"
)

//go:generate mockgen -package=mocks -destination=./mocks/tortoise.go -source=./tortoise.go

// Tortoise is an interface provided by tortoise implementation.
type Tortoise interface {
	OnBlock(*types.Block)
	OnHareOutput(types.LayerID, types.BlockID)
	TallyVotes(context.Context, types.LayerID)
	Updates() map[types.LayerID]map[types.BlockID]bool
	ChangedResults() []result.Layer
	Result(types.LayerID) ([]result.Layer, error)
}
