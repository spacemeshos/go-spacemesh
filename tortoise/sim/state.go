package sim

import (
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

// State of the node.
type State struct {
	logger log.Log

	MeshDB  *mesh.DB
	AtxDB   *activation.DB
	Beacons *beaconStore
}

// OnBeacon ...
func (s *State) OnBeacon(eid types.EpochID, beacon []byte) {}

// OnActivationTx ...
func (s *State) OnActivationTx(atx *types.ActivationTx) {}

// OnBlock ...
func (s *State) OnBlock(block *types.Block) {}

// OnInputVector ...
func (s *State) OnInputVector(lid types.LayerID, vector []types.BlockID) {}

// OnCoinflip ...
func (s *State) OnCoinflip(lid types.LayerID, coinflip bool) {}
