package system

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/types/result"
)

//go:generate mockgen -package=mocks -destination=./mocks/tortoise.go -source=./tortoise.go

// Tortoise is an interface provided by tortoise implementation.
type Tortoise interface {
	OnBlock(types.BlockHeader)
	OnHareOutput(types.LayerID, types.BlockID)
	OnWeakCoin(types.LayerID, bool)
	TallyVotes(context.Context, types.LayerID)
	LatestComplete() types.LayerID
	Updates() []result.Layer
	Results(from, to types.LayerID) ([]result.Layer, error)
}
