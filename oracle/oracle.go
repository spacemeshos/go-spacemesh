package oracle

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/eligibility"
)

// todo: configure oracle test constants like committee size and honesty.

type LocalOracle struct {
	committeeSize int
	oc            *eligibility.FixedRolacle
	nodeID        types.NodeId
}

func (bo *LocalOracle) IsIdentityActiveOnConsensusView(string, types.LayerID) (bool, error) {
	return true, nil
}

func (bo *LocalOracle) Register(isHonest bool, pubkey string) {
	bo.oc.Register(isHonest, pubkey)
}

func (bo *LocalOracle) Eligible(layer types.LayerID, round int32, committeeSize int, id types.NodeId, sig []byte) (bool, error) {
	return bo.oc.Eligible(layer, round, committeeSize, id, sig)
}

func (bo *LocalOracle) Proof(layer types.LayerID, round int32) ([]byte, error) {
	return bo.oc.Proof(layer, round)
}

func NewLocalOracle(rolacle *eligibility.FixedRolacle, committeeSize int, nodeID types.NodeId) *LocalOracle {
	return &LocalOracle{
		committeeSize: committeeSize,
		oc:            rolacle,
		nodeID:        nodeID,
	}
}

type HareOracle struct {
	oc *Client
}

func NewHareOracleFromClient(oc *Client) *HareOracle {
	return &HareOracle{oc: oc}
}

func (bo *HareOracle) IsIdentityActiveOnConsensusView(string, types.LayerID) (bool, error) {
	return true, nil
}

// Eligible checks eligibility for an identity in a round
func (bo *HareOracle) Eligible(layer types.LayerID, round int32, committeeSize int, id types.NodeId, sig []byte) (bool, error) {
	// note: we don't use the proof in the oracle server. we keep it just for the future syntax
	// todo: maybe replace k to be uint32 like hare wants, and don't use -1 for blocks
	return bo.oc.Eligible(layer, round, committeeSize, id, sig)
}

func (bo *HareOracle) Proof(types.LayerID, int32) ([]byte, error) {
	return []byte{}, nil
}
