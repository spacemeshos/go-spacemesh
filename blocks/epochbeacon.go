package blocks

import (
	"encoding/binary"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// BeaconGetter gets a beacon value.
type BeaconGetter interface {
	Get(layerNumber types.LayerID) ([]byte, error)
}

// EpochBeaconProvider holds all the dependencies for generating an epoch beacon. There are currently none.
type EpochBeaconProvider struct{}

// Get returns a beacon given an epoch ID. The current implementation returns the epoch ID in byte format.
func (p *EpochBeaconProvider) Get(layerNumber types.LayerID) ([]byte, error) {
	ret := make([]byte, 32)
	binary.LittleEndian.PutUint64(ret, uint64(layerNumber))
	return ret, nil
}
