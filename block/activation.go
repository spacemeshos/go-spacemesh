package block

import (
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/nipst"
)

type EpochId uint64

func (l EpochId) ToBytes() []byte { return common.Uint64ToBytes(uint64(l)) }

type AtxId struct {
	common.Hash
}

var EmptyAtx = AtxId{common.Hash{0}}


type ActivationTxHeader struct {
	NodeId            NodeId
	Sequence          uint64
	PrevATXId         AtxId
	LayerIndex        LayerID
	StartTick         uint64 //implement later
	EndTick           uint64 //implement later
	PositioningATX    AtxId
	ActiveSetSize     uint32
	VerifiedActiveSet uint32
	View              []BlockID
}

type ActivationTx struct {
	ActivationTxHeader
	Nipst *nipst.NIPST
	//todo: add sig
}

func NewActivationTx(NodeId NodeId, Sequence uint64, PrevATX AtxId, LayerIndex LayerID,
	StartTick uint64, PositioningATX AtxId, ActiveSetSize uint32, View []BlockID, nipst *nipst.NIPST) *ActivationTx {
	return &ActivationTx{
		ActivationTxHeader: ActivationTxHeader{
			NodeId:         NodeId,
			Sequence:       Sequence,
			PrevATXId:      PrevATX,
			LayerIndex:     LayerIndex,
			StartTick:      StartTick,
			PositioningATX: PositioningATX,
			ActiveSetSize:  ActiveSetSize,
			View:           View,
		},
		Nipst: nipst,
	}

}


func (t ActivationTx) Id() AtxId {
	tx, err := AtxHeaderAsBytes(&t.ActivationTxHeader)
	if err != nil {
		panic("could not Serialize atx")
	}

	return AtxId{crypto.Keccak256Hash(tx)}
}

func (t ActivationTx) Valid() bool {
	//todo: implement
	return true
}
