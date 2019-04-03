package mesh

import (
	"bytes"
	"fmt"
	"github.com/davecgh/go-xdr/xdr2"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/nipst"
	"io"
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

type EpochId uint64

func (l EpochId) ToBytes() []byte { return common.Uint64ToBytes(uint64(l)) }

type AtxId struct {
	common.Hash
}

var EmptyAtx = AtxId{common.Hash{0}}

var activesetCache = NewActivesetCache(1000)

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
			StartTick:      StartTick, //todo: this is still a
			PositioningATX: PositioningATX,
			ActiveSetSize:  ActiveSetSize,
			View:           View,
		},
		Nipst: nipst,
	}

}

type TraversalFunc = func(block *BlockHeader) error

func (t ActivationTx) Id() AtxId {
	tx, err := AtxHeaderAsBytes(&t.ActivationTxHeader)
	if err != nil {
		panic("could not Serialize transaction")
	}

	return AtxId{crypto.Keccak256Hash(tx)}
}

func (t ActivationTx) Valid() bool {
	//todo: implement
	return true
}

func (t ActivationTx) ActivesetValid() bool {
	if t.VerifiedActiveSet > 0 {
		return t.VerifiedActiveSet >= t.ActiveSetSize
	}
	return false
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

func (m *Mesh) CalcActiveSetFromView(a *ActivationTx) (uint32, error) {
	bytes, err := viewAsBytes(a.View)
	if err != nil {
		return 0, err
	}

	count, found := activesetCache.Get(common.BytesToHash(bytes))
	if found {
		return count, nil
	}

	var counter uint32 = 0
	traversalFunc := func(block *BlockHeader) error {
		blk, err := m.GetBlock(block.Id)
		if err != nil {
			log.Error("cannot validate atx, block %v not found", block.Id)
			return err
		}
		for _, atx := range blk.ATXs {
			if atx.Valid() {
				counter++
				if counter >= a.ActiveSetSize {
					return io.EOF
				}
			}
		}
		return nil
	}

	errHandler := func(er error) {}

	mp := map[BlockID]struct{}{}
	for _, blk := range a.View {
		mp[blk] = struct{}{}
	}

	firstLayerOfLastEpoch := a.LayerIndex - LayersInEpoch - ((a.LayerIndex - LayersInEpoch) % LayersInEpoch)
	m.mdb.ForBlockInView(mp, firstLayerOfLastEpoch, traversalFunc, errHandler)
	activesetCache.Add(common.BytesToHash(bytes), counter)

	return counter, nil

}
