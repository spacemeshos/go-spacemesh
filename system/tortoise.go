package system

import (
	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/types/result"
)

//go:generate mockgen -typed -package=mocks -destination=./mocks/tortoise.go -source=./tortoise.go

// Tortoise is an interface provided by tortoise implementation.
type Tortoise interface {
	OnBlock(types.BlockHeader)
	OnHareOutput(types.LayerID, types.BlockID)
	OnWeakCoin(types.LayerID, bool)
	TallyVotes(types.LayerID)
	LatestComplete() types.LayerID
	Updates() []result.Layer
	OnApplied(types.LayerID, types.Hash32) bool
	OnMalfeasance(types.NodeID)
	OnAtx(types.EpochID, types.ATXID, *atxsdata.ATX)
	GetMissingActiveSet(types.EpochID, []types.ATXID) []types.ATXID
}
