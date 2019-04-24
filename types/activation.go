package types

import (
	"bytes"
	"github.com/nullstyle/go-xdr/xdr3"
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
	PoETChallenge
	VerifiedActiveSet uint32
	ActiveSetSize     uint32
	View              []BlockID
	Valid             bool
}

type PoETChallenge struct {
	NodeId         NodeId
	Sequence       uint64
	PrevATXId      AtxId
	LayerIdx       LayerID
	StartTick      uint64
	EndTick        uint64
	PositioningAtx AtxId
}

func (p *PoETChallenge) ToBytes() ([]byte, error) {
	var w bytes.Buffer
	_, err := xdr.Marshal(&w, &p)
	if err != nil {
		return nil, err
	}

	return w.Bytes(), nil
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
			PoETChallenge: PoETChallenge{
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

func NewActivationTxWithcChallenge(poetChallenge PoETChallenge, ActiveSetSize uint32, View []BlockID, nipst *nipst.NIPST) *ActivationTx {
	return &ActivationTx{
		ActivationTxHeader: ActivationTxHeader{
			PoETChallenge: poetChallenge,
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
	// no other atx with same id and sequence number
	// if s != 0 the prevAtx is valid and it's seq num is s -1
	// positioning atx is valid
	// validate nipst duration?
	// fields 1-7 of the atx are the challenge of the poet
	// layer index i^ satisfies i -i^ < (layers_passed during nipst creation) ANTON: maybe should be ==?
	// the atx view contains d active Ids
	return nil
}
