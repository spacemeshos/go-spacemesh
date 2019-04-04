package activation

import (
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/nipst"
	"github.com/spacemeshos/go-spacemesh/rand"
)

type Network interface {
	Broadcast(channel string, data []byte) error
}

type ActiveSetProvider interface {
	GetActiveSetSize(l mesh.LayerID) uint32
}

type MeshProvider interface {
	GetLatestView() []mesh.BlockID
	LatestLayerId() mesh.LayerID
}

type EpochId uint64

func (l EpochId) ToBytes() []byte { return common.Uint64ToBytes(uint64(l)) }

type EpochProvider interface {
	Epoch(l mesh.LayerID) EpochId
}

type Builder struct {
	nodeId        mesh.NodeId
	db            *ActivationDb
	net           Network
	activeSet     ActiveSetProvider
	mesh          MeshProvider
	epochProvider EpochProvider
}

type Processor struct {
	db            *ActivationDb
	epochProvider EpochProvider
}

func (p *Processor) ProcessBlockATXs(block *mesh.Block) {
	for _, atx := range block.ATXs {
		err := p.db.StoreAtx(p.epochProvider.Epoch(atx.LayerIndex), atx)
		if err != nil {
			log.Error("cannot store atx: %v", atx)
		}
	}
}

func NewBuilder(nodeId mesh.NodeId, db database.DB, net Network, activeSet ActiveSetProvider, view MeshProvider, epochDuration EpochProvider) *Builder {
	return &Builder{
		nodeId, &ActivationDb{db}, net, activeSet, view, epochDuration,
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
	var posAtx *mesh.AtxId = nil
	if ech > 0 {
		posAtx, err = b.GetPositioningAtxId(ech - 1)
		if err != nil {
			return err
		}
	} else {
		posAtx = &mesh.EmptyAtx
	}

	if prevAtx == nil {
		log.Info("previous ATX not found")
		prevAtx = &mesh.EmptyAtx
	}

	atx := mesh.NewActivationTx(b.nodeId, seq+1, *prevAtx, l, 0, *posAtx, b.activeSet.GetActiveSetSize(l-1), b.mesh.GetLatestView(), nipst)

	buf, err := mesh.AtxAsBytes(atx)
	if err != nil {
		return err
	}
	//todo: should we do something about it? wait for something?
	return b.net.Broadcast(miner.AtxProtocol, buf)
}

func (b *Builder) GetPrevAtxId(node mesh.NodeId) (*mesh.AtxId, error) {
	ids, err := b.db.GetNodeAtxIds(node)

	if err != nil || len(ids) == 0 {
		return nil, err
	}
	return &ids[len(ids)-1], nil
}

func (b *Builder) GetPositioningAtxId(epochId EpochId) (*mesh.AtxId, error) {
	atxs, err := b.db.GetEpochAtxIds(epochId)
	if err != nil {
		return nil, err
	}
	atxId := atxs[rand.Int31n(int32(len(atxs)))]

	return &atxId, nil
}

func (b *Builder) GetLastSequence(node mesh.NodeId) uint64 {
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
