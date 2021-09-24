package blocks

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interface.go

// BeaconGetter gets a beacon value.
type BeaconGetter interface {
	GetBeacon(epochNumber types.EpochID) ([]byte, error)
}

type blockDB interface {
	GetBlock(ID types.BlockID) (*types.Block, error)
}
