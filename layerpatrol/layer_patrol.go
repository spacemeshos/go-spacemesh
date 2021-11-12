// Package layerpatrol keeps parties informed about the general progress of each layer.
package layerpatrol

import (
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

const bufferSize = uint32(100)

// LayerPatrol keeps progress of each layer.
type LayerPatrol struct {
	mu          sync.Mutex
	oldestLayer types.LayerID
	runByHare   map[types.LayerID]struct{}
}

// New returns an instance of LayerPatrol.
func New() *LayerPatrol {
	return &LayerPatrol{
		runByHare: make(map[types.LayerID]struct{}),
	}
}

// SetHareInCharge sets the layer's validation to be triggered by hare (as opposed to syncer).
func (lp *LayerPatrol) SetHareInCharge(layerID types.LayerID) {
	lp.mu.Lock()
	defer lp.mu.Unlock()

	if layerID.Uint32() > bufferSize {
		lp.oldestLayer = layerID.Sub(bufferSize)
	}
	delete(lp.runByHare, lp.oldestLayer)
	lp.runByHare[layerID] = struct{}{}
}

// IsHareInCharge returns true if the hare is set to handle the validation of the specified layer.
func (lp *LayerPatrol) IsHareInCharge(layerID types.LayerID) bool {
	lp.mu.Lock()
	defer lp.mu.Unlock()
	_, ok := lp.runByHare[layerID]
	return ok
}
