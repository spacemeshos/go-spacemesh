package oracle

import (
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/types"
)

// todo: configure oracle test constants like committee size and honesty.

type BlockOracle interface {
	BlockEligible(layerID types.LayerID) ([]types.BlockEligibilityProof, error)
}

type HareOracle interface {
	Eligible(instanceID uint32, committeeSize int, pubKey string, proof []byte) bool
}

type localBlockOracle struct {
	committeeSize int
	oc            *eligibility.FixedRolacle
	nodeID        types.NodeId
}

func NewLocalOracle(rolacle *eligibility.FixedRolacle, committeeSize int, nodeID types.NodeId) *localBlockOracle {
	//oc.Register(true, pubKey)
	return &localBlockOracle{
		committeeSize: committeeSize,
		oc:            rolacle,
		nodeID:        nodeID,
	}
}

func (bo *localBlockOracle) Register(isHonest bool, pubkey string) {
	bo.oc.Register(isHonest, pubkey)
}

// Eligible checks whether we're eligible to mine a block in layer i
func (bo *localBlockOracle) BlockEligible(layerID types.LayerID) ([]types.BlockEligibilityProof, error) {
	eligible := bo.oc.Eligible(uint32(layerID), bo.committeeSize, bo.nodeID.Key, nil)
	var proofs []types.BlockEligibilityProof
	if eligible {
		proofs = []types.BlockEligibilityProof{{
			J:   0,
			Sig: nil,
		}}
	}
	return proofs, nil
}

func (bo *localBlockOracle) Eligible(instanceID uint32, committeeSize int, pubKey string, proof []byte) bool {
	return bo.oc.Eligible(instanceID, committeeSize, pubKey, proof)
}

type blockOracle struct {
	committeeSize int
	oc            *OracleClient
	nodeID        types.NodeId
}

func NewBlockOracleFromClient(oc *OracleClient, committeeSize int, nodeID types.NodeId) *blockOracle {
	return &blockOracle{
		committeeSize: committeeSize,
		oc:            oc,
		nodeID:        nodeID,
	}
}

// Eligible checks whether we're eligible to mine a block in layer i
func (bo *blockOracle) BlockEligible(layerID types.LayerID) ([]types.BlockEligibilityProof, error) {
	eligible := bo.oc.Eligible(uint32(layerID), bo.committeeSize, bo.nodeID.Key)
	var proofs []types.BlockEligibilityProof
	if eligible {
		proofs = []types.BlockEligibilityProof{{
			J:   0,
			Sig: []byte{1, 2, 3},
		}}
	}
	return proofs, nil
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
