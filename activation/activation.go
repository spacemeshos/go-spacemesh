package activation

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/types"
	"sync/atomic"
	"time"
)

const AtxProtocol = "AtxGossip"

var activesetCache = NewActivesetCache(1000)

type ActiveSetProvider interface {
	ActiveSetSize(epochId types.EpochId) (uint32, error)
}

type MeshProvider interface {
	GetOrphanBlocksBefore(l types.LayerID) ([]types.BlockID, error)
	LatestLayer() types.LayerID
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
	BuildNIPST(challenge *common.Hash) (*types.NIPST, error)
	IsPostInitialized() bool
	InitializePost() (*types.PostProof, error)
}

type IdStore interface {
	StoreNodeIdentity(id types.NodeId) error
	GetIdentity(id string) (types.NodeId, error)
}

type NipstValidator interface {
	Validate(nipst *types.NIPST, expectedChallenge common.Hash) error
}

type ATXDBProvider interface {
	GetAtx(id types.AtxId) (*types.ActivationTx, error)
	CalcActiveSetFromView(a *types.ActivationTx) (uint32, error)
	GetNodeAtxIds(nodeId types.NodeId) ([]types.AtxId, error)
	GetEpochAtxIds(epochId types.EpochId) ([]types.AtxId, error)
}

type Builder struct {
	nodeId          types.NodeId
	coinbaseAccount address.Address
	db              ATXDBProvider
	net             Broadcaster
	activeSet       ActiveSetProvider
	mesh            MeshProvider
	layersPerEpoch  uint16
	tickProvider    PoETNumberOfTickProvider
	nipstBuilder    NipstBuilder
	challenge       *types.NIPSTChallenge
	nipst           *types.NIPST
	posLayerID      types.LayerID
	prevATX         *types.ActivationTx
	timer           chan types.LayerID
	stop            chan struct{}
	finished        chan struct{}
	working         bool
	started         uint32
	isSynced        func() bool
	log             log.Log
}

type Processor struct {
	db            *ActivationDb
	epochProvider EpochProvider
}

// NewBuilder returns an atx builder that will start a routine that will attempt to create an atx upon each new layer.
func NewBuilder(nodeId types.NodeId, coinbaseAccount address.Address, db ATXDBProvider, net Broadcaster, activeSet ActiveSetProvider, mesh MeshProvider, layersPerEpoch uint16, nipstBuilder NipstBuilder, layerClock chan types.LayerID, isSyncedFunc func() bool, log log.Log) *Builder {

	return &Builder{
		nodeId:          nodeId,
		coinbaseAccount: coinbaseAccount,
		db:              db,
		net:             net,
		activeSet:       activeSet,
		mesh:            mesh,
		layersPerEpoch:  layersPerEpoch,
		nipstBuilder:    nipstBuilder,
		timer:           layerClock,
		stop:            make(chan struct{}),
		finished:        make(chan struct{}),
		isSynced:        isSyncedFunc,
		log:             log,
	}
}

// Start is the main entry point of the atx builder. it runs the main loop of the builder and shouldn't be called more than once
func (b *Builder) Start() {
	if atomic.LoadUint32(&b.started) == 1 {
		return
	}
	atomic.StoreUint32(&b.started, 1)
	go b.loop()
}

// Stop stops the atx builder.
func (b *Builder) Stop() {
	b.finished <- struct{}{}
}

// loop is the main loop that tries to create an atx per tick received from the global clock
func (b *Builder) loop() {
	// post is initialized here, consider moving it to another location.
	if !b.nipstBuilder.IsPostInitialized() {
		_, err := b.nipstBuilder.InitializePost() // TODO: add proof to first ATX
		if err != nil {
			b.log.Error("PoST initialization failed: %v", err)
			return
		}
	}
	for {
		select {
		case <-b.stop:
			return
		case layer := <-b.timer:
			if !b.isSynced() {
				b.log.Info("cannot create atx : not synced")
				break
			}
			if b.working {
				break
			}
			b.working = true
			go func() {
				epoch := layer.GetEpoch(b.layersPerEpoch)
				_, err := b.PublishActivationTx(epoch)
				if err != nil {
					b.log.Error("cannot create atx in epoch %v: %v", epoch, err)
				}
				b.finished <- struct{}{}
			}()
		case <-b.finished:
			b.working = false
		}
	}
}

// PublishActivationTx attempts to publish an atx for the given epoch, it returns an error if an atx cannot be created
// publish atx may not produce an atx each time it is called, that is expected behaviour as well.
func (b *Builder) PublishActivationTx(epoch types.EpochId) (bool, error) {
	if b.nipst != nil {
		b.log.With().Info("re-entering atx creation", log.EpochId(uint64(epoch)))
	} else {
		b.log.With().Info("starting build atx", log.EpochId(uint64(epoch)))
		if b.prevATX == nil {
			prevAtxId, err := b.GetPrevAtxId(b.nodeId)
			if err != nil {
				b.log.Info("no prev ATX found, starting fresh")
			} else {
				b.prevATX, err = b.db.GetAtx(*prevAtxId)
				if err != nil {
					// TODO: handle inconsistent state
					b.log.Panic("prevAtx (id: %v) not found in DB -- inconsistent state", prevAtxId.ShortId())
				}
			}
		}
		seq := uint64(0)
		prevAtxId := *types.EmptyAtxId
		if b.prevATX != nil {
			seq = b.prevATX.Sequence + 1

			//check if this node hasn't published an activation already
			if b.prevATX.PubLayerIdx.GetEpoch(b.layersPerEpoch) == epoch+1 {
				b.log.With().Info("atx already created, aborting", log.EpochId(uint64(epoch)))
				return false, nil
			}
			prevAtxId = b.prevATX.Id()
		}
		posAtxEndTick := uint64(0)
		b.posLayerID = types.LayerID(0)

		//positioning atx is from this epoch as well, since we will be publishing the atx in the next epoch
		//todo: what if no other atx was received in this epoch yet?
		posAtxId := *types.EmptyAtxId
		posAtx, err := b.GetPositioningAtx(epoch)
		atxPubLayerID := types.LayerID(0)
		if err == nil {
			posAtxEndTick = posAtx.EndTick
			b.posLayerID = posAtx.PubLayerIdx
			atxPubLayerID = b.posLayerID.Add(b.layersPerEpoch)
			posAtxId = posAtx.Id()
		} else {
			if !epoch.IsGenesis() {
				return false, fmt.Errorf("cannot find pos atx: %v", err)
			}
		}

		b.challenge = &types.NIPSTChallenge{
			NodeId:         b.nodeId,
			Sequence:       seq,
			PrevATXId:      prevAtxId,
			PubLayerIdx:    atxPubLayerID,
			StartTick:      posAtxEndTick,
			EndTick:        b.tickProvider.NumOfTicks(), //todo: add provider when
			PositioningAtx: posAtxId,
		}

		hash, err := b.challenge.Hash()
		if err != nil {
			return false, fmt.Errorf("getting challenge hash failed: %v", err)
		}
		b.nipst, err = b.nipstBuilder.BuildNIPST(hash)
		if err != nil {
			return false, fmt.Errorf("cannot create nipst: %v", err)
		}
	}
	if b.mesh.LatestLayer().GetEpoch(b.layersPerEpoch) < b.challenge.PubLayerIdx.GetEpoch(b.layersPerEpoch) {
		// cannot determine active set size before mesh reaches publication epoch, will try again in next layer
		b.log.Warning("received PoET proof too soon. ATX publication epoch: %v; mesh epoch: %v; started in clock-epoch: %v",
			b.challenge.PubLayerIdx.GetEpoch(b.layersPerEpoch), b.mesh.LatestLayer().GetEpoch(b.layersPerEpoch), epoch)
		return false, nil
	}
	// when we reach here an epoch has passed
	// we've completed the sequential work, now before publishing the atx,
	// we need to provide number of atx seen in the epoch of the positioning atx.
	posEpoch := b.posLayerID.GetEpoch(b.layersPerEpoch)
	activeIds, err := b.activeSet.ActiveSetSize(posEpoch)
	if err != nil && !posEpoch.IsGenesis() {
		return false, err
	}
	view, err := b.mesh.GetOrphanBlocksBefore(b.mesh.LatestLayer())
	if err != nil {
		return false, err
	}
	atx := types.NewActivationTxWithChallenge(*b.challenge, b.coinbaseAccount, activeIds, view, b.nipst)
	b.log.With().Info("start calculation active set")
	activeSetSize, err := b.db.CalcActiveSetFromView(atx) // TODO: remove this assertion to improve performance
	b.log.With().Info("active ids seen for epoch", log.Uint64("pos_atx_epoch", uint64(posEpoch)),
		log.Uint32("cache_cnt", activeIds), log.Uint32("view_cnt", activeSetSize))

	if !atx.TargetEpoch(b.layersPerEpoch).IsGenesis() && activeSetSize == 0 {
		b.log.Warning("empty active set size found! len(view): %d, view: %v", len(atx.View), atx.View)
		return false, nil
	}

	if atx.TargetEpoch(b.layersPerEpoch).IsGenesis() {
		atx.ActiveSetSize = 0
	} else {
		if activeSetSize != atx.ActiveSetSize {
			b.log.Panic("active set size mismatch! size based on view: %d, size reported: %d",
				activeSetSize, atx.ActiveSetSize)
		}
	}

	buf, err := types.AtxAsBytes(atx)
	if err != nil {
		return false, err
	}
	b.prevATX = atx

	// cleanup state
	b.nipst = nil
	b.challenge = nil
	b.posLayerID = 0

	time.Sleep(20 * time.Second)
	err = b.net.Broadcast(AtxProtocol, buf)
	if err != nil {
		return false, err
	}

	b.log.With().EventInfo(fmt.Sprintf("atx published! id: %v, prevATXID: %v, posATXID: %v, layer: %v, published in epoch: %v, active set: %v miner: %v view %v",
		atx.ShortId(), atx.PrevATXId.ShortString(), atx.PositioningAtx.ShortString(), atx.PubLayerIdx,
		atx.PubLayerIdx.GetEpoch(b.layersPerEpoch), atx.ActiveSetSize, b.nodeId.Key[:5], len(atx.View)))
	return true, nil
}

func (b *Builder) Persist(c *types.NIPSTChallenge) {
	//todo: implement storing to persistent media
}

func (b *Builder) Load() *types.NIPSTChallenge {
	//todo: implement loading from persistent media
	return nil
}

func (b *Builder) GetPrevAtxId(node types.NodeId) (*types.AtxId, error) {
	//todo: make sure atx ids are ordered and valid
	ids, err := b.db.GetNodeAtxIds(node)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no prev atxs for node %v", node.Key)
	}
	return &ids[len(ids)-1], nil
}

// GetPositioningAtxId returns a randomly selected atx from the provided epoch epochId
// it returns an error if epochs were not found in db
func (b *Builder) GetPositioningAtxId(epochId types.EpochId) (*types.AtxId, error) {
	//todo: make this on blocking until an atx is received
	atxs, err := b.db.GetEpochAtxIds(epochId)
	if err != nil {
		return nil, err
	}
	atxId := atxs[rand.Int31n(int32(len(atxs)))]

	return &atxId, nil
}

// GetLastSequence retruns the last sequence number of atx reported by node id node
// it will return 0 if no previous atx was found for the requested node
func (b *Builder) GetLastSequence(node types.NodeId) uint64 {
	atxId, err := b.GetPrevAtxId(node)
	if err != nil {
		return 0
	}
	atx, err := b.db.GetAtx(*atxId)
	if err != nil {
		b.log.Error("wtf no atx in db %v", *atxId)
		return 0
	}
	return atx.Sequence
}

// GetPositioningAtx return the atx object for the positioning atx according to requested epochId
func (b *Builder) GetPositioningAtx(epochId types.EpochId) (*types.ActivationTx, error) {
	posAtxId, err := b.GetPositioningAtxId(epochId)
	if err != nil {
		if b.prevATX != nil {
			//if the atx was created by this miner but have not propagated as an atx to the notwork yet, use the cached atx
			return b.prevATX, nil
		} else {
			return nil, fmt.Errorf("cannot find pos atx id: %v", err)
		}
	}
	posAtx, err := b.db.GetAtx(*posAtxId)
	if err != nil {
		return nil, fmt.Errorf("cannot find pos atx: %v", err.Error())
	}
	return posAtx, nil
}
