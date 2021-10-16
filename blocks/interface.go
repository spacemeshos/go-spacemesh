package blocks

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interface.go

// BeaconGetter gets a beacon value.
type BeaconGetter interface {
	GetBeacon(types.EpochID) ([]byte, error)
}

type beaconCollector interface {
	ReportBeaconFromBlock(types.EpochID, types.BlockID, []byte, uint64)
}

type blockDB interface {
	GetBlock(types.BlockID) (*types.Block, error)
}
