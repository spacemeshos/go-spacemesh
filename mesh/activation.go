package mesh

import (
	"bytes"
	"fmt"
	"github.com/davecgh/go-xdr/xdr2"
	"github.com/spacemeshos/go-spacemesh/nipst"
	"io"
)

//todo: choose which type is VRF
type Vrf string

type Id struct {
	Key string
	Vrf Vrf
}

type ActivationTx struct {
	Id             Id
	Sequence       uint64
	PrevATX        Id
	LayerIndex     LayerID
	StartTick      uint64 //whatever
	PositioningATX Id
	Nipst          nipst.Nipst
	ActiveSetSize  uint32
	View           []BlockID
	//todo: add sig
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

func NewAtx() *ActivationTx {
	return nil
}
