package types

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
	POeTChallange
	VerifiedActiveSet uint32
	ActiveSetSize     uint32
	View              []BlockID
}

type POeTChallange struct {
	NodeId         NodeId
	Sequence       uint64
	PrevATXId      AtxId
	LayerIdx       LayerID
	StartTick      uint64
	EndTick        uint64
	PositioningAtx AtxId
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
			POeTChallange: POeTChallange{
				NodeId:         NodeId,
				Sequence:       Sequence,
				PrevATXId:      PrevATX,
				LayerIdx:       LayerIndex,
				StartTick:      StartTick,
				PositioningAtx: PositioningATX,
			},
			ActiveSetSize: ActiveSetSize,
			View:          View,
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

func (t ActivationTx) Validate() error {
	//todo: implement
	// valid signature
	//
	return nil
}
