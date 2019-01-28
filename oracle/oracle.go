package oracle

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

const DefaultBlockCommitteeSize = 200

type BlockOracle interface {
	Validate(id mesh.LayerID, pubKey fmt.Stringer) bool
}

type blockOracle struct {
	committeeSize int
	oc *OracleClient
}

func NewBlockOracle(worldid uint64, committeeSize int, pubKey fmt.Stringer) *blockOracle {
	oc := NewOracleClientWithWorldID(worldid)
	oc.Register(pubKey)
	return &blockOracle{
		committeeSize,
		oc,
	}
}

// ValidateBlock makes oracleBlock a BlockValidator
func (bo *blockOracle) ValidateBlock(block mesh.Block) bool {
	// NOTE: this only validates a single block, the view edges should be manually validated
	return bo.Validate(block.LayerIndex, block.MinerID)
}

func (bo *blockOracle) Validate(id mesh.LayerID, pubKey string) bool {
	return bo.oc.Validate(common.Uint32ToBytes(uint32(id)), -1, bo.committeeSize, pubKey)
}

type HareOracle interface {
	Validate(instanceID hare.InstanceId, K int, committeeSize int, pubKey string, proof []byte) bool
}

type hareOracle struct {
	oc *OracleClient
}

func NewHareOracle(worldid uint64, pubKey fmt.Stringer) *hareOracle {
	oc := NewOracleClientWithWorldID(worldid)
	oc.Register(pubKey)
	return &hareOracle{
		oc,
	}
}

func (bo *hareOracle) Validate(instanceID hare.InstanceId, K uint32, committeeSize int, pubKey string, proof []byte) bool {
	//note: we don't use the proof in the oracle server. we keep it just for the future syntax
	//todo: maybe replace k to be uint32 like hare wants, and don't use -1 for blocks
	return bo.oc.Validate(instanceID.Bytes(), int(K), committeeSize, pubKey)
}

