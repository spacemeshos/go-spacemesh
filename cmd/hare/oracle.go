package main

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
)

type hareOracle struct {
	oc *oracleClient
}

func newHareOracleFromClient(oc *oracleClient) *hareOracle {
	return &hareOracle{oc: oc}
}

func (bo *hareOracle) IsIdentityActiveOnConsensusView(string, types.LayerID) (bool, error) {
	return true, nil
}

// Eligible checks eligibility for an identity in a round
func (bo *hareOracle) Eligible(layer types.LayerID, round int32, committeeSize int, id types.NodeId, sig []byte) (bool, error) {
	// note: we don't use the proof in the oracle server. we keep it just for the future syntax
	// todo: maybe replace k to be uint32 like hare wants, and don't use -1 for blocks
	return bo.oc.Eligible(layer, round, committeeSize, id, sig)
}

func (bo *hareOracle) Proof(types.LayerID, int32) ([]byte, error) {
	return []byte{}, nil
}
