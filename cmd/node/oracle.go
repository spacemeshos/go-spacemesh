package node

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/eligibility"
)

// todo: configure oracle test constants like committee size and honesty.

type localOracle struct {
	committeeSize int
	oc            *eligibility.FixedRolacle
	nodeID        types.NodeID
}

func (bo *localOracle) IsIdentityActiveOnConsensusView(context.Context, string, types.LayerID) (bool, error) {
	return true, nil
}

func (bo *localOracle) Register(isHonest bool, pubkey string) {
	bo.oc.Register(isHonest, pubkey)
}

func (bo *localOracle) Eligible(ctx context.Context, layer types.LayerID, round int32, committeeSize int, id types.NodeID, sig []byte) (bool, error) {
	return bo.oc.Eligible(ctx, layer, round, committeeSize, id, sig)
}

func (bo *localOracle) Proof(ctx context.Context, layer types.LayerID, round int32) ([]byte, error) {
	return bo.oc.Proof(ctx, layer, round)
}

func newLocalOracle(rolacle *eligibility.FixedRolacle, committeeSize int, nodeID types.NodeID) *localOracle {
	return &localOracle{
		committeeSize: committeeSize,
		oc:            rolacle,
		nodeID:        nodeID,
	}
}
