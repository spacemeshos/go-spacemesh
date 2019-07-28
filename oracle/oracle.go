package oracle

import (
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/types"
)

// todo: configure oracle test constants like committee size and honesty.

type BlockOracle interface {
	BlockEligible(layerID types.LayerID) (types.AtxId, []types.BlockEligibilityProof, error)
}

type HareOracle interface {
	Eligible(instanceID uint32, committeeSize int, pubKey string, proof []byte) bool
}

type localOracle struct {
	committeeSize int
	oc            *eligibility.FixedRolacle
	nodeID        types.NodeId
}

func (bo *localOracle) IsIdentityActiveOnConsensusView(edId string, layer types.LayerID) (bool, error) {
	return true, nil
}

func (bo *localOracle) Register(isHonest bool, pubkey string) {
	bo.oc.Register(isHonest, pubkey)
}

func (bo *localOracle) Eligible(layer types.LayerID, round int32, committeeSize int, id types.NodeId, sig []byte) (bool, error) {
	return bo.oc.Eligible(layer, round, committeeSize, id, sig)
}

func (bo *localOracle) Proof(id types.NodeId, layer types.LayerID, round int32) ([]byte, error) {
	return bo.oc.Proof(id, layer, round)
}

func NewLocalOracle(rolacle *eligibility.FixedRolacle, committeeSize int, nodeID types.NodeId) *localOracle {
	//oc.Register(true, pubKey)
	return &localOracle{
		committeeSize: committeeSize,
		oc:            rolacle,
		nodeID:        nodeID,
	}
}

type hareOracle struct {
	oc *OracleClient
}

func NewHareOracleFromClient(oc *OracleClient) *hareOracle {
	return &hareOracle{
		oc,
	}
}

func (bo *hareOracle) IsIdentityActiveOnConsensusView(edId string, layer types.LayerID) (bool, error) {
	return bo.oc.IsIdentityActive(edId, layer)
}

// Eligible checks eligibility for an identity in a round
func (bo *hareOracle) Eligible(layer types.LayerID, round int32, committeeSize int, id types.NodeId, sig []byte) (bool, error) {
	//note: we don't use the proof in the oracle server. we keep it just for the future syntax
	//todo: maybe replace k to be uint32 like hare wants, and don't use -1 for blocks
	return bo.oc.Eligible(layer, round, committeeSize, id, sig)
}

func (bo *hareOracle) Proof(id types.NodeId, layer types.LayerID, round int32) ([]byte, error) {
	return bo.oc.Proof(id, layer, round)
}
