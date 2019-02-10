package oracle

import (
<<<<<<< HEAD
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

const DefaultBlockCommitteeSize = 200
=======
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

// todo: configure oracle test constants like committee size and honesty.
>>>>>>> 961580118b23445872756fdbe5ba3f23cfd7b103

type BlockOracle interface {
	BlockEligible(id mesh.LayerID, pubKey string) bool
}

type HareOracle interface {
	Eligible(instanceID uint32, committeeSize int, pubKey string, proof []byte) bool
}

type localBlockOracle struct {
	committeeSize int
	oc            *eligibility.FixedRolacle
}

func NewLocalOracle(committeeSize int) *localBlockOracle {
	oc := eligibility.New()
	//oc.Register(true, pubKey)
	return &localBlockOracle{
		committeeSize,
		oc,
	}
}

func (bo *localBlockOracle) Register(isHonest bool, pubkey string) {
	bo.oc.Register(isHonest, pubkey)
}

// Eligible checks whether we're eligible to mine a block in layer i
func (bo *localBlockOracle) BlockEligible(id mesh.LayerID, pubKey string) bool {
	return bo.oc.Eligible(uint32(id), bo.committeeSize, pubKey, nil)
}

func (bo *localBlockOracle) Eligible(instanceID uint32, committeeSize int, pubKey string, proof []byte) bool {
	return bo.oc.Eligible(instanceID, committeeSize, pubKey, proof)
}

type blockOracle struct {
	committeeSize int
	oc            *OracleClient
}

func NewBlockOracle(worldid uint64, committeeSize int, pubKey string) *blockOracle {
	oc := NewOracleClientWithWorldID(worldid)
	oc.Register(pubKey)
	return &blockOracle{
		committeeSize,
		oc,
	}
}

// Eligible checks whether we're eligible to mine a block in layer i
<<<<<<< HEAD
func (bo *blockOracle) Eligible(id mesh.LayerID, pubKey string) bool {
	return bo.oc.Validate(common.Uint32ToBytes(uint32(id)), -1, bo.committeeSize, pubKey)
=======
func (bo *blockOracle) BlockEligible(id mesh.LayerID, pubKey string) bool {
	return bo.oc.Eligible(uint32(id), bo.committeeSize, pubKey)
>>>>>>> 961580118b23445872756fdbe5ba3f23cfd7b103
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
