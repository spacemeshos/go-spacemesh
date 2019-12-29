package activation

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/post/shared"
	"sync"
	"sync/atomic"
	"time"
)

const AtxProtocol = "AtxGossip"

var activesetCache = NewActivesetCache(5)
var tooSoonErr = errors.New("received PoET proof too soon")

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
	BuildNIPST(challenge *types.Hash32) (*types.NIPST, error)
}

type IdStore interface {
	StoreNodeIdentity(id types.NodeId) error
	GetIdentity(id string) (types.NodeId, error)
}

type NipstValidator interface {
	Validate(id signing.PublicKey, nipst *types.NIPST, expectedChallenge types.Hash32) error
	VerifyPost(id signing.PublicKey, proof *types.PostProof, space uint64) error
}

type ATXDBProvider interface {
	GetAtxHeader(id types.AtxId) (*types.ActivationTxHeader, error)
	CalcActiveSetFromView(view []types.BlockID, pubEpoch types.EpochId) (uint32, error)
	GetNodeLastAtxId(nodeId types.NodeId) (types.AtxId, error)
	GetPosAtxId(epochId types.EpochId) (*types.AtxId, error)
}

type BytesStore interface {
	Put(key []byte, buf []byte) error
	Get(key []byte) ([]byte, error)
}

type Signer interface {
	Sign(m []byte) []byte
}

const (
	InitIdle = 1 + iota
	InitInProgress
	InitDone
)

type Builder struct {
	Signer
	nodeId          types.NodeId
	coinbaseAccount types.Address
	db              ATXDBProvider
	net             Broadcaster
	mesh            MeshProvider
	layersPerEpoch  uint16
	tickProvider    PoETNumberOfTickProvider
	nipstBuilder    NipstBuilder
	postProver      PostProverClient
	challenge       *types.NIPSTChallenge
	nipst           *types.NIPST
	commitment      *types.PostProof
	posLayerID      types.LayerID
	prevATX         *types.ActivationTxHeader
	timer           chan types.LayerID
	stop            chan struct{}
	finished        chan struct{}
	working         bool
	started         uint32
	store           BytesStore
	isSynced        func() bool
	accountLock     sync.RWMutex
	initStatus      int32
	log             log.Log
}

// NewBuilder returns an atx builder that will start a routine that will attempt to create an atx upon each new layer.
func NewBuilder(nodeId types.NodeId, coinbaseAccount types.Address, signer Signer, db ATXDBProvider, net Broadcaster, mesh MeshProvider, layersPerEpoch uint16, nipstBuilder NipstBuilder, postProver PostProverClient, layerClock chan types.LayerID, isSyncedFunc func() bool, store BytesStore, log log.Log) *Builder {
	return &Builder{
		Signer:          signer,
		nodeId:          nodeId,
		coinbaseAccount: coinbaseAccount,
		db:              db,
		net:             net,
		mesh:            mesh,
		layersPerEpoch:  layersPerEpoch,
		nipstBuilder:    nipstBuilder,
		postProver:      postProver,
		timer:           layerClock,
		stop:            make(chan struct{}),
		finished:        make(chan struct{}),
		isSynced:        isSyncedFunc,
		store:           store,
		initStatus:      InitIdle,
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
	close(b.stop)
}

func (b Builder) SignAtx(atx *types.ActivationTx) (*types.ActivationTx, error) {
	bts, err := types.InterfaceToBytes(atx.InnerActivationTx)
	if err != nil {
		return nil, err
	}
	atx.Sig = b.Sign(bts)
	return atx, nil
}

// loop is the main loop that tries to create an atx per tick received from the global clock
func (b *Builder) loop() {
	err := b.loadChallenge()
	if err != nil {
		log.Info("challenge not loaded: %s", err)
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

			if atomic.LoadInt32(&b.initStatus) != InitDone {
				b.log.Info("post is not initialized yet, not building Nipst")
				break
			}
			if b.working {
				break
			}
			b.working = true
			go func() {
				epoch := layer.GetEpoch(b.layersPerEpoch)
				err := b.PublishActivationTx(epoch)
				if err != nil {
					if err == tooSoonErr {
						b.log.Warning("cannot create atx in epoch %v: %v", epoch, err)
					} else {
						b.log.Error("cannot create atx in epoch %v: %v", epoch, err)
					}
				}
				b.finished <- struct{}{}
			}()
		case <-b.finished:
			b.working = false
		}
	}
}

type alreadyPublishedErr struct{}

func (alreadyPublishedErr) Error() string { return "already published" }

func (b *Builder) buildNipstChallenge(epoch types.EpochId) error {
	if b.prevATX == nil {
		prevAtxId, err := b.GetPrevAtxId(b.nodeId)
		if err != nil {
			b.log.Info("no prev ATX found, starting fresh")
		} else {
			b.prevATX, err = b.db.GetAtxHeader(prevAtxId)
			if err != nil {
				// TODO: handle inconsistent state
				b.log.Panic("prevAtx (id: %v) not found in DB -- inconsistent state", prevAtxId.ShortString())
			}
		}
	}
	seq := uint64(0)
	prevAtxId := *types.EmptyAtxId
	commitmentMerkleRoot := []byte(nil)

	if b.prevATX == nil {
		// if and only if it's the first ATX, the merkle root of the initial PoST proof,
		// the commitment, is included in the Nipst challenge. This is done in order to prove
		// that it existed before the PoET start time.
		commitmentMerkleRoot = b.commitment.MerkleRoot
	} else {
		seq = b.prevATX.Sequence + 1
		//todo: handle this case for loading mem and recovering
		//check if this node hasn't published an activation already
		if b.prevATX.PubLayerIdx.GetEpoch(b.layersPerEpoch) == epoch+1 {
			b.log.With().Info("atx already created, aborting", log.EpochId(uint64(epoch)))
			return alreadyPublishedErr{}
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
			return fmt.Errorf("cannot find pos atx in epoch %v: %v", epoch, err)
		}
	}

	b.challenge = &types.NIPSTChallenge{
		NodeId:               b.nodeId,
		Sequence:             seq,
		PrevATXId:            prevAtxId,
		PubLayerIdx:          atxPubLayerID,
		StartTick:            posAtxEndTick,
		EndTick:              b.tickProvider.NumOfTicks(), //todo: add provider when
		PositioningAtx:       posAtxId,
		CommitmentMerkleRoot: commitmentMerkleRoot,
	}

	err = b.storeChallenge(b.challenge)
	if err != nil {
		log.Error("challenge cannot be stored: %s", err)
	}
	return nil
}

func (b *Builder) StartPost(rewardAddress types.Address, dataDir string, space uint64) error {
	if !atomic.CompareAndSwapInt32(&b.initStatus, InitIdle, InitInProgress) {
		switch atomic.LoadInt32(&b.initStatus) {
		case InitDone:
			return fmt.Errorf("already initialized")
		case InitInProgress:
		default:
			return fmt.Errorf("already started")
		}
	}

	if err := b.postProver.SetParams(dataDir, space); err != nil {
		return err
	}
	b.SetCoinbaseAccount(rewardAddress)

	initialized, err := b.postProver.IsInitialized()
	if err != nil {
		atomic.StoreInt32(&b.initStatus, InitIdle)
		return err
	}

	if !initialized {
		if err := b.postProver.VerifyInitAllowed(); err != nil {
			atomic.StoreInt32(&b.initStatus, InitIdle)
			return err
		}
	}

	b.log.With().Info("Starting PoST initialization",
		log.String("datadir", dataDir),
		log.String("space", fmt.Sprintf("%d", space)),
		log.String("rewardAddress", fmt.Sprintf("%x", rewardAddress)),
	)

	go func() {
		if initialized {
			// If initialized, run the execution phase with zero-challenge,
			// to create the initial proof (the commitment).
			b.commitment, err = b.postProver.Execute(shared.ZeroChallenge)
			if err != nil {
				b.log.Error("PoST execution failed: %v", err)
				atomic.StoreInt32(&b.initStatus, InitIdle)
				return
			}
		} else {
			// If not initialized, run the initialization phase.
			// This would create the initial proof (the commitment) as well.
			b.commitment, err = b.postProver.Initialize()
			if err != nil {
				b.log.Error("PoST initialization failed: %v", err)
				atomic.StoreInt32(&b.initStatus, InitIdle)
				return
			}
		}

		b.log.With().Info("PoST initialization completed",
			log.String("datadir", dataDir),
			log.String("space", fmt.Sprintf("%d", space)),
			log.String("commitment merkle root", fmt.Sprintf("%x", b.commitment.MerkleRoot)),
		)

		atomic.StoreInt32(&b.initStatus, InitDone)
	}()

	return nil
}

// MiningStats returns state of post init, coinbase reward account and data directory path for post commitment
func (b *Builder) MiningStats() (int, string, string) {
	acc := b.getCoinbaseAccount()
	initStatus := atomic.LoadInt32(&b.initStatus)
	datadir := b.postProver.Cfg().DataDir
	return int(initStatus), acc.String(), datadir
}

func (b *Builder) SetCoinbaseAccount(rewardAddress types.Address) {
	b.accountLock.Lock()
	b.coinbaseAccount = rewardAddress
	b.accountLock.Unlock()
}

func (b *Builder) getCoinbaseAccount() types.Address {
	b.accountLock.RLock()
	acc := b.coinbaseAccount
	b.accountLock.RUnlock()
	return acc
}

func (b Builder) getNipstKey() []byte {
	return []byte("Nipst")
}

func (b *Builder) storeChallenge(ch *types.NIPSTChallenge) error {
	bts, err := types.InterfaceToBytes(ch)
	if err != nil {
		return err
	}
	return b.store.Put(b.getNipstKey(), bts)
}

func (b *Builder) loadChallenge() error {
	bts, err := b.store.Get(b.getNipstKey())
	if err != nil {
		return err
	}
	if len(bts) > 0 {
		tp := &types.NIPSTChallenge{}
		err = types.BytesToInterface(bts, tp)
		if err != nil {
			return err
		}
		b.challenge = tp
	}
	return nil
}

func (b *Builder) discardChallenge() error {
	return b.store.Put(b.getNipstKey(), []byte{})
}

// PublishActivationTx attempts to publish an atx for the given epoch, it returns an error if an atx cannot be created
// publish atx may not produce an atx each time it is called, that is expected behaviour as well.
func (b *Builder) PublishActivationTx(epoch types.EpochId) error {
	if b.nipst != nil {
		b.log.With().Info("re-entering atx creation in epoch %v", log.EpochId(uint64(epoch)))
	} else {
		b.log.With().Info("starting build atx in epoch %v", log.EpochId(uint64(epoch)))
		if b.challenge != nil {
			if b.challenge.PubLayerIdx.GetEpoch(b.layersPerEpoch) < epoch {
				log.Info("previous challenge loaded from store, but epoch has already passed, %s, %s", epoch, b.challenge.PubLayerIdx.GetEpoch(b.layersPerEpoch))
				b.challenge = nil
				err := b.discardChallenge()
				if err != nil {
					log.Error("cannot discard challenge")
				}
			}
		}
		if b.challenge == nil {
			err := b.buildNipstChallenge(epoch)
			if err != nil {
				if _, alreadyPublished := err.(alreadyPublishedErr); alreadyPublished {
					return nil
				}
				return err
			}
		}
		hash, err := b.challenge.Hash()
		if err != nil {
			return fmt.Errorf("getting challenge hash failed: %v", err)
		}
		b.nipst, err = b.nipstBuilder.BuildNIPST(hash)
		if err != nil {
			return fmt.Errorf("cannot create Nipst: %v", err)
		}
	}
	if b.mesh.LatestLayer().GetEpoch(b.layersPerEpoch) < b.challenge.PubLayerIdx.GetEpoch(b.layersPerEpoch) {
		// cannot determine active set size before mesh reaches publication epoch, will try again in next layer
		b.log.Warning("received PoET proof too soon. ATX publication epoch: %v; mesh epoch: %v; started in clock-epoch: %v",
			b.challenge.PubLayerIdx.GetEpoch(b.layersPerEpoch), b.mesh.LatestLayer().GetEpoch(b.layersPerEpoch), epoch)
		return tooSoonErr
	}

	// when we reach here an epoch has passed
	// we've completed the sequential work, now before publishing the atx,
	// we need to provide number of atx seen in the epoch of the positioning atx.
	posEpoch := b.posLayerID.GetEpoch(b.layersPerEpoch)
	pubEpoch := b.challenge.PubLayerIdx.GetEpoch(b.layersPerEpoch) // can be 0 in first epoch
	targetEpoch := pubEpoch + 1

	viewLayer := pubEpoch.FirstLayer(b.layersPerEpoch)
	var view []types.BlockID
	if viewLayer > 0 {
		var err error
		view, err = b.mesh.GetOrphanBlocksBefore(viewLayer)
		if err != nil {
			return fmt.Errorf("failed to get current view for layer %v: %v", viewLayer, err)
		}
	}

	var activeSetSize uint32
	if pubEpoch > 0 {
		var err error
		b.log.With().Info("calculating active ids")
		activeSetSize, err = b.db.CalcActiveSetFromView(view, pubEpoch)
		if err != nil {
			return fmt.Errorf("failed to calculate activeset: %v", err)
		}
	}
	if activeSetSize == 0 && !targetEpoch.IsGenesis() {
		return fmt.Errorf("empty active set size found! epochId: %v, len(view): %d, view: %v",
			pubEpoch, len(view), view)
	}

	var commitment *types.PostProof
	if b.prevATX == nil {
		commitment = b.commitment
	}

	atx := types.NewActivationTxWithChallenge(*b.challenge, b.getCoinbaseAccount(), activeSetSize, view, b.nipst, commitment)

	b.log.With().Info("active ids seen for epoch", log.Uint64("pos_atx_epoch", uint64(posEpoch)),
		log.Uint32("view_cnt", activeSetSize))

	signedAtx, err := b.SignAtx(atx)
	if err != nil {
		return err
	}

	buf, err := types.InterfaceToBytes(signedAtx)
	if err != nil {
		return err
	}
	b.prevATX = atx.ActivationTxHeader

	// cleanup state
	b.nipst = nil
	b.challenge = nil
	b.posLayerID = 0

	time.Sleep(10 * time.Second)
	err = b.net.Broadcast(AtxProtocol, buf)
	if err != nil {
		return err
	}

	err = b.discardChallenge()
	if err != nil {
		log.Error("cannot discard Nipst challenge %s", err)
	}

	b.log.Event().Info("atx published!", log.AtxId(atx.ShortString()), log.String("prev_atx_id", atx.PrevATXId.ShortString()),
		log.String("post_atx_id", atx.PositioningAtx.ShortString()), log.LayerId(uint64(atx.PubLayerIdx)), log.EpochId(uint64(atx.PubLayerIdx.GetEpoch(b.layersPerEpoch))),
		log.Uint32("active_set", atx.ActiveSetSize), log.String("miner", b.nodeId.Key[:5]), log.Int("view", len(atx.View)), log.Int("atx_size", len(buf)))

	return nil
}

func (b *Builder) Persist(c *types.NIPSTChallenge) {
	//todo: implement storing to persistent media
}

func (b *Builder) Load() *types.NIPSTChallenge {
	//todo: implement loading from persistent media
	return nil
}

func (b *Builder) GetPrevAtxId(node types.NodeId) (types.AtxId, error) {
	id, err := b.db.GetNodeLastAtxId(node)
	if err != nil {
		return *types.EmptyAtxId, err
	}
	return id, nil
}

// GetPositioningAtxId returns the top ATX (highest layer number) from the provided epoch epochId.
// It returns an error if the top ATX is from a different epoch.
func (b *Builder) GetPositioningAtxId(epochId types.EpochId) (*types.AtxId, error) {
	//todo: make this on blocking until an atx is received
	atxId, err := b.db.GetPosAtxId(epochId)
	if err != nil {
		return nil, err
	}
	return atxId, nil
}

// GetLastSequence retruns the last sequence number of atx reported by node id node
// it will return 0 if no previous atx was found for the requested node
func (b *Builder) GetLastSequence(node types.NodeId) uint64 {
	atxId, err := b.GetPrevAtxId(node)
	if err != nil {
		return 0
	}
	atx, err := b.db.GetAtxHeader(atxId)
	if err != nil {
		b.log.Error("wtf no atx in db %v", atxId)
		return 0
	}
	return atx.Sequence
}

// GetPositioningAtx return the atx object for the positioning atx according to requested epochId
func (b *Builder) GetPositioningAtx(epochId types.EpochId) (*types.ActivationTxHeader, error) {
	posAtxId, err := b.GetPositioningAtxId(epochId)
	if err != nil {
		if b.prevATX != nil && b.prevATX.PubLayerIdx.GetEpoch(b.layersPerEpoch) == epochId {
			//if the atx was created by this miner but have not propagated as an atx to the notwork yet, use the cached atx
			return b.prevATX, nil
		} else {
			return nil, fmt.Errorf("cannot find pos atx id: %v", err)
		}
	}
	posAtx, err := b.db.GetAtxHeader(*posAtxId)
	if err != nil {
		return nil, fmt.Errorf("cannot find pos atx: %v", err.Error())
	}
	return posAtx, nil
}

// ExtractPublicKey extracts public key from message and verifies public key exists in IdStore, this is how we validate
// ATX signature. If this is the first ATX it is considered valid anyways and ATX syntactic validation will determine ATX validity
func ExtractPublicKey(signedAtx *types.ActivationTx) (*signing.PublicKey, error) {
	bts, err := signedAtx.AtxBytes()
	if err != nil {
		return nil, err
	}

	pubKey, err := ed25519.ExtractPublicKey(bts, signedAtx.Sig)
	if err != nil {
		return nil, err
	}

	pub := signing.NewPublicKey(pubKey)
	return pub, nil
}

func SignAtx(signer *signing.EdSigner, atx *types.ActivationTx) (*types.ActivationTx, error) {
	bts, err := atx.AtxBytes()
	if err != nil {
		return nil, err
	}
	atx.Sig = signer.Sign(bts)
	return atx, nil
}
