package oracle

import (
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

const DefaultBlockCommitteeSize = 200

type BlockOracle interface {
	Eligible(id mesh.LayerID, pubKey string) bool
}

type blockOracle struct {
	committeeSize int
	oc *OracleClient
}

func NewBlockOracle(worldid uint64, committeeSize int, pubKey string) *blockOracle {
	oc := NewOracleClientWithWorldID(worldid)
	oc.Register(pubKey)
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
	return bo.oc.Validate(common.Uint32ToBytes(uint32(id)), -1, bo.committeeSize, pubKey)
}

type HareOracle interface {
	Eligible(instanceID hare.InstanceId, K int, committeeSize int, pubKey string, proof []byte) bool
}

type hareOracle struct {
	oc *OracleClient
}

func NewHareOracle(worldid uint64, pubKey string) *hareOracle {
	oc := NewOracleClientWithWorldID(worldid)
	oc.Register(pubKey)
	return &hareOracle{
		oc,
	}
}

// Eligible checks eligibility for an identity in a round
func (bo *hareOracle) Eligible(instanceID hare.InstanceId, K uint32, committeeSize int, pubKey string, proof []byte) bool {
	//note: we don't use the proof in the oracle server. we keep it just for the future syntax
	//todo: maybe replace k to be uint32 like hare wants, and don't use -1 for blocks
	return bo.oc.Validate(instanceID.Bytes(), int(K), committeeSize, pubKey)
}