package main

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type hareOracle struct {
	oc *oracleClient
}

func newHareOracleFromClient(oc *oracleClient) *hareOracle {
	return &hareOracle{oc: oc}
}

func (bo *hareOracle) IsIdentityActiveOnConsensusView(context.Context, string, types.LayerID) (bool, error) {
	return true, nil
}

func (bo *hareOracle) Validate(ctx context.Context, layer types.LayerID, round uint32, committeeSize int, id types.NodeID, sig []byte, eligibilityCount uint16) (bool, error) {
	if eligibilityCount != 1 {
		return false, nil
	}
	return bo.oc.Eligible(layer, round, committeeSize, id, sig)
}

func (bo *hareOracle) CalcEligibility(_ context.Context, layer types.LayerID, round uint32, committeeSize int, id types.NodeID, sig []byte) (uint16, error) {
	eligible, err := bo.oc.Eligible(layer, round, committeeSize, id, sig)
	if eligible {
		return 1, nil
	}
	return 0, err
}

func (bo *hareOracle) Proof(context.Context, types.LayerID, uint32) ([]byte, error) {
	return []byte{}, nil
}

func (bo *hareOracle) IsEpochBeaconReady(ctx context.Context, epoch types.EpochID) bool {
	return true
}
