package oracle

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common"
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

type stringerer string

func (s stringerer) String() string {
	return string(s)
}

// ValidateBlock
func (bo *blockOracle) ValidateBlock(block mesh.Block) bool {
	return bo.Validate(block.LayerIndex, stringerer(block.MinerID))
}

func (bo *blockOracle) Validate(id mesh.LayerID, pubKey fmt.Stringer) bool {
	return bo.oc.Validate(common.Uint32ToBytes(uint32(id)), -1, bo.committeeSize, pubKey)
}


type HareOracle interface {
	Validate(instanceID []byte, K int, committeeSize int, pubKey fmt.Stringer, proof []byte) bool
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

func (bo *hareOracle) Validate(instanceID []byte, K uint32, committeeSize int, pubKey fmt.Stringer, proof []byte) bool {
	//note: we don't use the proof in the oracle server. we keep it just for the future syntax
	//todo: maybe replace k to be uint32 like hare wants, and don't use -1 for blocks
	return bo.oc.Validate(instanceID, int(K), committeeSize, pubKey)
}

