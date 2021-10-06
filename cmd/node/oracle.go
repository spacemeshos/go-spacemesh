package node

import (
	"context"
	"fmt"

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

func (bo *localOracle) Validate(ctx context.Context, layer types.LayerID, round uint32, committeeSize int, id types.NodeID, sig []byte, eligibilityCount uint16) (bool, error) {
	validated, err := bo.oc.Validate(ctx, layer, round, committeeSize, id, sig, eligibilityCount)
	if err != nil {
		return validated, fmt.Errorf("validate: %w", err)
	}

	return validated, nil
}

func (bo *localOracle) CalcEligibility(ctx context.Context, layer types.LayerID, round uint32, committeeSize int, id types.NodeID, sig []byte) (uint16, error) {
	v, err := bo.oc.CalcEligibility(ctx, layer, round, committeeSize, id, sig)
	if err != nil {
		return v, fmt.Errorf("calc eligibility : %w", err)
	}

	return v, nil
}

func (bo *localOracle) Proof(ctx context.Context, layer types.LayerID, round uint32) ([]byte, error) {
	proof, err := bo.oc.Proof(ctx, layer, round)
	if err != nil {
		return proof, fmt.Errorf("generate proof: %w", err)
	}

	return proof, nil
}

func (bo *localOracle) IsEpochBeaconReady(ctx context.Context, epoch types.EpochID) bool {
	return true
}

func newLocalOracle(rolacle *eligibility.FixedRolacle, committeeSize int, nodeID types.NodeID) *localOracle {
	return &localOracle{
		committeeSize: committeeSize,
		oc:            rolacle,
		nodeID:        nodeID,
	}
}
