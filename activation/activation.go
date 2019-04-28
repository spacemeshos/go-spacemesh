package activation

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/nipst"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/types"
)

const AtxProtocol = "AtxGossip"

var activesetCache = NewActivesetCache(1000)

type ActiveSetProvider interface {
	GetActiveSetSize(l types.LayerID) uint32
}

type MeshProvider interface {
	GetLatestView() []types.BlockID
	LatestLayerId() types.LayerID
}

type EpochProvider interface {
	Epoch(l types.LayerID) types.EpochId
}

type Broadcaster interface {
	Broadcast(channel string, data []byte) error
}

type PoETNumberOfTickProvider struct {
}

func (provider *PoETNumberOfTickProvider) NumOfTicks() uint64 {
	return 0
}

type NipstBuilder interface {
	BuildNIPST(challange []byte) (*nipst.NIPST, error)
}

type IdStore interface {
	StoreNodeIdentity(id types.NodeId) error
	GetIdentity(id string) (types.NodeId, error)
}

type Builder struct {
	nodeId         types.NodeId
	db             *ActivationDb
	net            Broadcaster
	activeSet      ActiveSetProvider
	mesh           MeshProvider
	epochProvider  EpochProvider
	layersPerEpoch uint64
	tickProvider   PoETNumberOfTickProvider
	nipstBuilder   NipstBuilder
}

type Processor struct {
	db            *ActivationDb
	epochProvider EpochProvider
}

func NewBuilder(nodeId types.NodeId, db database.DB, meshdb *mesh.MeshDB, net Broadcaster, activeSet ActiveSetProvider, view MeshProvider, epochDuration EpochProvider, layersPerEpoch uint64, nipstBuilder NipstBuilder, identityStore IdStore) *Builder {
	return &Builder{
		nodeId,
		NewActivationDb(db, identityStore, meshdb, layersPerEpoch),
		net,
		activeSet,
		view,
		epochDuration,
		layersPerEpoch,
		PoETNumberOfTickProvider{},
		nipstBuilder,
	}
}

func (b *Builder) PublishActivationTx(epoch types.EpochId) error {
	prevAtx, err := b.GetPrevAtxId(b.nodeId)
	seq := uint64(0)
	if err == nil {
		atx, err := b.db.GetAtx(*prevAtx)
		if err != nil {
			return err
		}
		seq = atx.Sequence + 1
	} else {
		prevAtx = &types.EmptyAtx
	}
	posAtxId := &types.EmptyAtx
	endTick := uint64(0)
	LayerIdx := b.mesh.LatestLayerId()
	if epoch > 0 {
		//positioning atx is from the last epoch
		posAtxId, err = b.GetPositioningAtxId(epoch - 1)
		if err != nil {
			return err
		}
		posAtx, err := b.db.GetAtx(*posAtxId)
		if err != nil {
			return err
		}
		endTick = posAtx.EndTick
	}

	challenge := types.PoETChallenge{
		NodeId:         b.nodeId,
		Sequence:       seq,
		PrevATXId:      *prevAtx,
		LayerIdx:       types.LayerID(uint64(LayerIdx) + b.layersPerEpoch),
		StartTick:      endTick,
		EndTick:        b.tickProvider.NumOfTicks(), //todo: add provider when
		PositioningAtx: *posAtxId,
	}

	data, err := challenge.ToBytes()
	if err != nil {
		return err
	}
	npst, err := b.nipstBuilder.BuildNIPST(data)
	if err != nil {
		return err
	}
	//todo: check if view should be latest layer -1
	atx := types.NewActivationTxWithcChallenge(challenge, b.activeSet.GetActiveSetSize(b.mesh.LatestLayerId()-1), b.mesh.GetLatestView(), npst)

	buf, err := types.AtxAsBytes(atx)
	if err != nil {
		return err
	}
	//todo: should we do something about it? wait for something?
	return b.net.Broadcast(AtxProtocol, buf)

}

func (b *Builder) Persist(c *types.PoETChallenge) {
	//todo: implement storing to persistent media
}

func (b *Builder) Load() *types.PoETChallenge {
	//todo: implement loading from persistent media
	return nil
}

func (b *Builder) GetPrevAtxId(node types.NodeId) (*types.AtxId, error) {
	ids, err := b.db.GetNodeAtxIds(node)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no prev atxs for node %v", node.Key)
	}
	return &ids[len(ids)-1], nil
}

func (b *Builder) GetPositioningAtxId(epochId types.EpochId) (*types.AtxId, error) {
	//todo: make this on blocking until an atx is received
	atxs, err := b.db.GetEpochAtxIds(epochId)
	if err != nil {
		return nil, err
	}
	atxId := atxs[rand.Int31n(int32(len(atxs)))]

	return &atxId, nil
}

func (b *Builder) GetLastSequence(node types.NodeId) uint64 {
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
