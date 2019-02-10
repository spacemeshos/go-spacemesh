package oracle

import (
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

// todo: configure oracle test constants like committee size and honesty.


type BlockOracle interface {
	Eligible(id mesh.LayerID, pubKey string) bool
}

type blockOracle struct {
	committeeSize int
	oc            *OracleClient
}

func NewBlockOracle(worldid uint64, committeeSize int, pubKey string) *blockOracle {
	oc := NewOracleClientWithWorldID(worldid)
	oc.Register(true, pubKey)
	return &blockOracle{
		committeeSize,
		oc,
	}
}

// EligibleBlock makes oracleBlock a BlockValidator
func (bo *blockOracle) EligibleBlock(block *mesh.Block) bool {
	// NOTE: this only validates a single block, the view edges should be manually validated
	return bo.Eligible(block.LayerIndex, block.MinerID)
}

// Eligible checks whether we're eligible to mine a block in layer i
func (bo *blockOracle) Eligible(id mesh.LayerID, pubKey string) bool {
	return bo.oc.Eligible(uint32(id), bo.committeeSize, pubKey)
}

type HareOracle interface {
	Eligible(instanceID hare.InstanceId, K int, committeeSize int, pubKey string, proof []byte) bool
}

type hareOracle struct {
	oc *OracleClient
}

func NewHareOracle(worldid uint64, pubKey string) *hareOracle {
	oc := NewOracleClientWithWorldID(worldid)
	oc.Register(true, pubKey)
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
