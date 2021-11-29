package sim

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

func newState(logger log.Log, conf config) State {
	mdb := newMeshDB(logger, conf)
	return State{
		logger:  logger,
		MeshDB:  mdb,
		AtxDB:   newAtxDB(logger, mdb, conf),
		Beacons: newBeaconStore(),
	}
}

// State of the node.
type State struct {
	logger log.Log

	MeshDB  *mesh.DB
	AtxDB   *activation.DB
	Beacons *beaconStore
}

// OnBeacon callback to store generated beacon.
func (s *State) OnBeacon(eid types.EpochID, beacon []byte) {
	s.Beacons.StoreBeacon(eid, beacon)
}

// OnActivationTx callback to store activation transaction.
func (s *State) OnActivationTx(atx *types.ActivationTx) {
	if err := s.AtxDB.StoreAtx(context.TODO(), atx.PubLayerID.GetEpoch(), atx); err != nil {
		s.logger.With().Panic("failed to save atx", log.Err(err))
	}
}

// OnBlock callback to store block.
func (s *State) OnBlock(block *types.Block) {
	exist, _ := s.MeshDB.GetBlock(block.ID())
	if exist != nil {
		return
	}
	if err := s.MeshDB.AddBlock(block); err != nil {
		s.logger.With().Panic("failed to save block", log.Err(err))
	}
}

// OnInputVector callback to store input vector.
func (s *State) OnInputVector(lid types.LayerID, vector []types.BlockID) {
	if err := s.MeshDB.SaveLayerInputVectorByID(context.TODO(), lid, vector); err != nil {
		s.logger.With().Panic("failed to save input vector", log.Err(err))
	}
}

// OnCoinflip callback to store coinflip.
func (s *State) OnCoinflip(lid types.LayerID, coinflip bool) {
	s.MeshDB.RecordCoinflip(context.TODO(), lid, coinflip)
}
