package activation

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common"
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
	ActiveSetIds(l types.EpochId) uint32
}

//GetLatestVerified() []types.BlockID

type MeshProvider interface {
	GetLatestVerified() []types.BlockID
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
	BuildNIPST(challenge *common.Hash) (*nipst.NIPST, error)
}

type Builder struct {
	nodeId         types.NodeId
	db             *ActivationDb
	net            Broadcaster
	activeSet      ActiveSetProvider
	mesh           MeshProvider
	layersPerEpoch uint64
	tickProvider   PoETNumberOfTickProvider
	nipstBuilder   NipstBuilder
	timer          chan types.LayerID
	stop           chan struct{}
	finished       chan struct{}
	working        bool
}

type Processor struct {
	db            *ActivationDb
	epochProvider EpochProvider
}

func NewBuilder(nodeId types.NodeId, db database.DB, meshdb *mesh.MeshDB, net Broadcaster, activeSet ActiveSetProvider, view MeshProvider, layersPerEpoch uint64, nipstBuilder NipstBuilder, layerClock chan types.LayerID) *Builder {
	return &Builder{
		nodeId,
		NewActivationDb(db, meshdb, layersPerEpoch),
		net,
		activeSet,
		view,
		layersPerEpoch,
		PoETNumberOfTickProvider{},
		nipstBuilder,
		layerClock,
		make(chan struct{}),
		make(chan struct{}),
		false,
	}
}

func (b *Builder) Start(){
	go b.loop()
}

func (b *Builder) Stop() {
	b.finished <- struct{}{}
}

func (b *Builder) loop() {
	for {
		select {
		case <-b.stop:
			return
		case layer := <-b.timer:
			if b.working {
				break
			}
			b.working = true
			go func() {
				err := b.PublishActivationTx(types.EpochId(uint64(layer) / b.layersPerEpoch))
				if err != nil {
					log.Error("cannot create atx : %v", err)
				}
				b.finished <- struct{}{}
			}()
		case <-b.finished:
			b.working = false
		}
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
		if types.EpochId(uint64(atx.LayerIdx) / b.layersPerEpoch) == epoch {
			return fmt.Errorf("atx already created for epoch %v", epoch)
		}
		seq = atx.Sequence + 1
	} else {
		prevAtx = &types.EmptyAtxId
	}
	posAtxId := &types.EmptyAtxId
	endTick := uint64(0)
	LayerIdx := uint64(0)
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
		LayerIdx = uint64(posAtx.LayerIdx)
	}

	challenge := types.NIPSTChallenge{
		NodeId:         b.nodeId,
		Sequence:       seq,
		PrevATXId:      *prevAtx,
		LayerIdx:       types.LayerID(uint64(LayerIdx) + b.layersPerEpoch),
		StartTick:      endTick,
		EndTick:        b.tickProvider.NumOfTicks(), //todo: add provider when
		PositioningAtx: *posAtxId,
	}

	hash, err := challenge.Hash()
	if err != nil {
		return fmt.Errorf("getting challenge hash failed: %v", err)
	}
	npst, err := b.nipstBuilder.BuildNIPST(hash)
	if err != nil {
		return err
	}
	atx := types.NewActivationTxWithChallenge(challenge, uint32(b.activeSet.ActiveSetIds(types.EpochId(LayerIdx / b.layersPerEpoch))),
		b.mesh.GetLatestVerified(), npst, true)

	buf, err := types.AtxAsBytes(atx)
	if err != nil {
		return err
	}
	//todo: should we do something about it? wait for something?
	return b.net.Broadcast(AtxProtocol, buf)

}

func (b *Builder) Persist(c *types.NIPSTChallenge) {
	//todo: implement storing to persistent media
}

func (b *Builder) Load() *types.NIPSTChallenge {
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
