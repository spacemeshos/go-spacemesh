package mesh

import (
	"bytes"
	"fmt"
	"github.com/davecgh/go-xdr/xdr2"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/nipst"
)

//todo: choose which type is VRF
type Vrf string

type NodeId struct {
	Key string
	Vrf Vrf
}

func (id NodeId) String() string {
	return id.Key + string(id.Vrf)
}

func (id NodeId) ToBytes() []byte {
	return common.Hex2Bytes(id.String())
}

type AtxId struct {
	common.Hash
}

type ActivationTxHeader struct {
	NodeId         NodeId
	Sequence       uint64
	PrevATXId      AtxId
	LayerIndex     LayerID
	StartTick      uint64 //implement later
	EndTick        uint64 //implement later
	PositioningATX AtxId
	ActiveSetSize  uint32
	View           []BlockID
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
			StartTick:      StartTick, //todo: whatever
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
		panic("could not Serialize transaction")
	}

	return AtxId{crypto.Keccak256Hash(tx)}
}

func AtxHeaderAsBytes(tx *ActivationTxHeader) ([]byte, error) {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, &tx); err != nil {
		return nil, fmt.Errorf("error atx header %v", err)
	}
	return w.Bytes(), nil
}

func AtxAsBytes(tx *ActivationTx) ([]byte, error) {
	var w bytes.Buffer
	if _, err := xdr.Marshal(&w, &tx); err != nil {
		return nil, fmt.Errorf("error marshalling block ids %v", err)
	}
	return w.Bytes(), nil
}

func BytesAsAtx(b []byte) (*ActivationTx, error) {
	buf := bytes.NewReader(b)
	atx := ActivationTx{}
	_, err := xdr.Unmarshal(buf, &atx)
	if err != nil {
		return nil, err
	}
	return &atx, nil
}
