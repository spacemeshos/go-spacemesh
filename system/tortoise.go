package system

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/tortoise.go -source=./tortoise.go

// Tortoise is an interface provided by tortoise implementation.
type Tortoise interface {
	OnBlock(*types.Block)
	OnHareOutput(types.LayerID, types.BlockID)
	TallyVotes(context.Context, types.LayerID)
	LatestComplete() types.LayerID
}
