package mesh

import (
	"bytes"
	"fmt"
	"github.com/davecgh/go-xdr/xdr2"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/nipst"
	"io"
)

//todo: choose which type is VRF
type Vrf string

type Id struct {
	Key string
	Vrf Vrf
}

func (id Id) String() string {
	return id.Key + string(id.Vrf)
}

func (id Id) ToBytes() []byte {
	return common.Hex2Bytes(id.String())
}

type AtxId struct {
	common.Hash
}

type ActivationTxHeader struct {
	NodeId         Id
	Sequence       uint64
	PrevATX        AtxId
	LayerIndex     LayerID
	StartTick      uint64 //whatever
	PositioningATX AtxId
	ActiveSetSize  uint32
	View           []BlockID
}

type ActivationTx struct {
	ActivationTxHeader
	Nipst nipst.NIPST
	//todo: add sig
}

func NewActivationTx(NodeId Id, Sequence uint64, PrevATX AtxId, LayerIndex LayerID,
	StartTick uint64, PositioningATX AtxId, ActiveSetSize uint32, View []BlockID, nipst nipst.NIPST) *ActivationTx {
	return &ActivationTx{
		ActivationTxHeader: ActivationTxHeader{
			NodeId:         NodeId,
			Sequence:       Sequence,
			PrevATX:        PrevATX,
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

func BytesAsAtx(buf io.Reader) (*ActivationTx, error) {
	b := ActivationTx{}
	_, err := xdr.Unmarshal(buf, &b)
	if err != nil {
		return &b, err
	}
	return &b, nil
}
