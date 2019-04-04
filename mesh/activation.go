package mesh

import (
	"bytes"
	"fmt"
	"github.com/davecgh/go-xdr/xdr2"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/nipst"
	"github.com/spacemeshos/go-spacemesh/rand"
)

//todo: choose which type is VRF
type Vrf string

const AtxProtocol = "AtxGossip"

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
			StartTick:      StartTick,
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
		panic("could not Serialize atx")
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

type ActiveSetProvider interface {
	GetActiveSetSize(l LayerID) uint32
}

type MeshProvider interface {
	GetLatestView() []BlockID
	LatestLayerId() LayerID
}

type EpochProvider interface {
	Epoch(l LayerID) EpochId
}

type Broadcaster interface {
	Broadcast(channel string, data []byte) error
}

type Builder struct {
	nodeId        NodeId
	db            *ActivationDb
	net           Broadcaster
	activeSet     ActiveSetProvider
	mesh          MeshProvider
	epochProvider EpochProvider
}

type Processor struct {
	db            *ActivationDb
	epochProvider EpochProvider
}

func NewBuilder(nodeId NodeId, db database.DB, net Broadcaster, activeSet ActiveSetProvider, view MeshProvider, epochDuration EpochProvider) *Builder {
	return &Builder{
		nodeId, NewActivationDb(db), net, activeSet, view, epochDuration,
	}
}

func (b *Builder) PublishActivationTx(nipst *nipst.NIPST) error {
	seq := b.GetLastSequence(b.nodeId)
	prevAtx, err := b.GetPrevAtxId(b.nodeId)
	if seq > 0 && err != nil {
		log.Error("cannot find prev ATX for nodeid %v ", b.nodeId)
		return err
	}
	l := b.mesh.LatestLayerId()
	ech := b.epochProvider.Epoch(l)
	var posAtx *AtxId = nil
	if ech > 0 {
		posAtx, err = b.GetPositioningAtxId(ech - 1)
		if err != nil {
			return err
		}
	} else {
		posAtx = &EmptyAtx
	}

	if prevAtx == nil {
		log.Info("previous ATX not found")
		prevAtx = &EmptyAtx
	}

	atx := NewActivationTx(b.nodeId, seq+1, *prevAtx, l, 0, *posAtx, b.activeSet.GetActiveSetSize(l-1), b.mesh.GetLatestView(), nipst)

	buf, err := AtxAsBytes(atx)
	if err != nil {
		return err
	}
	//todo: should we do something about it? wait for something?
	return b.net.Broadcast(AtxProtocol, buf)
}

func (b *Builder) GetPrevAtxId(node NodeId) (*AtxId, error) {
	ids, err := b.db.GetNodeAtxIds(node)

	if err != nil || len(ids) == 0 {
		return nil, err
	}
	return &ids[len(ids)-1], nil
}

func (b *Builder) GetPositioningAtxId(epochId EpochId) (*AtxId, error) {
	atxs, err := b.db.GetEpochAtxIds(epochId)
	if err != nil {
		return nil, err
	}
	atxId := atxs[rand.Int31n(int32(len(atxs)))]

	return &atxId, nil
}

func (b *Builder) GetLastSequence(node NodeId) uint64 {
	atxId, err := b.GetPrevAtxId(node)
	if err != nil {
		return 0
	}
	atx, err := b.db.GetAtx(*atxId)
	if err != nil {
		log.Error("wtf no atx in db %v", *atxId)
		return 0
	}
	return atx.Sequence
}
