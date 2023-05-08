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
	OnBeacon(types.EpochID, types.Beacon)
	TallyVotes(context.Context, types.LayerID)
	Updates() map[types.LayerID]map[types.BlockID]bool
	LatestComplete() types.LayerID
	Results(from, to types.LayerID) ([]result.Layer, error)
}
