package oracle

import (
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

// todo: configure oracle test constants like committee size and honesty.

type BlockOracle interface {
	BlockEligible(layerID mesh.LayerID) (mesh.AtxId, []mesh.BlockEligibilityProof, error)
}

type HareOracle interface {
	Eligible(instanceID uint32, committeeSize int, pubKey string, proof []byte) bool
}

type localBlockOracle struct {
	committeeSize int
	oc            *eligibility.FixedRolacle
	nodeID        mesh.NodeId
}

func NewLocalOracle(committeeSize int, nodeID mesh.NodeId) *localBlockOracle {
	oc := eligibility.New()
	//oc.Register(true, pubKey)
	return &localBlockOracle{
		committeeSize: committeeSize,
		oc:            oc,
		nodeID:        nodeID,
	}
}

func (bo *localBlockOracle) Register(isHonest bool, pubkey string) {
	bo.oc.Register(isHonest, pubkey)
}

// Eligible checks whether we're eligible to mine a block in layer i
func (bo *localBlockOracle) BlockEligible(layerID mesh.LayerID) (mesh.AtxId, []mesh.BlockEligibilityProof, error) {
	eligible := bo.oc.Eligible(uint32(layerID), bo.committeeSize, bo.nodeID.Key, nil)
	var proofs []mesh.BlockEligibilityProof
	if eligible {
		proofs = []mesh.BlockEligibilityProof{}
	}
	return mesh.AtxId{}, proofs, nil
}

func (bo *localBlockOracle) Eligible(instanceID uint32, committeeSize int, pubKey string, proof []byte) bool {
	return bo.oc.Eligible(instanceID, committeeSize, pubKey, proof)
}

type hareOracle struct {
	oc *OracleClient
}

func NewHareOracleFromClient(oc *OracleClient) *hareOracle {
	return &hareOracle{
		oc,
	}
}

// Eligible checks eligibility for an identity in a round
func (bo *hareOracle) Eligible(id uint32, committeeSize int, pubKey string, proof []byte) bool {
	//note: we don't use the proof in the oracle server. we keep it just for the future syntax
	//todo: maybe replace k to be uint32 like hare wants, and don't use -1 for blocks
	return bo.oc.Eligible(id, committeeSize, pubKey)
}
