package activation

import (
	"fmt"
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/post/shared"
	"sync"
	"sync/atomic"
)

const AtxProtocol = "AtxGossip"

var activesetCache = NewActivesetCache(1000)

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
	BuildNIPST(challenge *types.Hash32, timeout chan struct{}, stop chan struct{}) (*types.NIPST, error)
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
	GetPosAtxId() (types.AtxId, error)
	AwaitAtx(id types.AtxId) chan struct{}
	UnsubscribeAtx(id types.AtxId)
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
	commitment      *types.PostProof
	layerClock      LayerClock
	stop            chan struct{}
	started         uint32
	store           BytesStore
	syncer          Syncer
	accountLock     sync.RWMutex
	initStatus      int32
	initDone        chan struct{}
	log             log.Log
}

type LayerClock interface {
	AwaitLayer(layerId types.LayerID) chan struct{}
	GetCurrentLayer() types.LayerID
}

type Syncer interface {
	Await() chan struct{}
}

// NewBuilder returns an atx builder that will start a routine that will attempt to create an atx upon each new layer.
func NewBuilder(nodeId types.NodeId, coinbaseAccount types.Address, signer Signer, db ATXDBProvider, net Broadcaster, mesh MeshProvider, layersPerEpoch uint16, nipstBuilder NipstBuilder, postProver PostProverClient, layerClock LayerClock, syncer Syncer, store BytesStore, log log.Log) *Builder {
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
		layerClock:      layerClock,
		stop:            make(chan struct{}),
		syncer:          syncer,
		store:           store,
		initStatus:      InitIdle,
		initDone:        make(chan struct{}),
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
	close(b.stop)
}

func (b Builder) SignAtx(atx *types.ActivationTx) error {
	return SignAtx(b, atx)
}

type StopRequestedError struct{}

func (s StopRequestedError) Error() string { return "stop requested" }

func (b *Builder) waitOrStop(ch chan struct{}) error {
	select {
	case <-ch:
		return nil
	case <-b.stop:
		return &StopRequestedError{}
	}
}

// loop is the main loop that tries to create an atx per tick received from the global clock
func (b *Builder) loop() {
	err := b.loadChallenge()
	if err != nil {
		log.Info("challenge not loaded: %s", err)
	}
	if err := b.waitOrStop(b.initDone); err != nil {
		return
	}
	// ensure layer 1 has arrived
	if err := b.waitOrStop(b.layerClock.AwaitLayer(1)); err != nil {
		return
	}
	for {
		select {
		case <-b.stop:
			return
		default:
		}
		if err := b.PublishActivationTx(); err != nil {
			if _, stopRequested := err.(StopRequestedError); stopRequested {
				return
			}
			b.log.With().Error("failed to publish ATX", log.Err(err))
			events.Publish(events.AtxCreated{Created: false, Layer: uint64(b.currentEpoch())})
			<-b.layerClock.AwaitLayer(b.layerClock.GetCurrentLayer() + 1)
		}
	}
}

func (b *Builder) buildNipstChallenge() error {
	<-b.syncer.Await()
	challenge := &types.NIPSTChallenge{NodeId: b.nodeId}
	if posAtx, err := b.GetPositioningAtx(); err != nil {
		if !b.currentEpoch().IsGenesis() {
			return fmt.Errorf("failed to get positioning ATX: %v", err)
		}
		challenge.EndTick = b.tickProvider.NumOfTicks()
	} else {
		challenge.PositioningAtx = posAtx.Id()
		challenge.PubLayerIdx = posAtx.PubLayerIdx.Add(b.layersPerEpoch)
		challenge.StartTick = posAtx.EndTick
		challenge.EndTick = posAtx.EndTick + b.tickProvider.NumOfTicks()
	}
	if prevAtx, err := b.GetPrevAtx(b.nodeId); err != nil {
		challenge.CommitmentMerkleRoot = b.commitment.MerkleRoot
	} else {
		challenge.PrevATXId = prevAtx.Id()
		challenge.Sequence = prevAtx.Sequence + 1
	}
	b.challenge = challenge
	if err := b.storeChallenge(b.challenge); err != nil {
		return fmt.Errorf("failed to store nipst challenge: %v", err)
	}
	return nil
}

func (b *Builder) StartPost(rewardAddress types.Address, dataDir string, space uint64) error {
	if !atomic.CompareAndSwapInt32(&b.initStatus, InitIdle, InitInProgress) {
		switch atomic.LoadInt32(&b.initStatus) {
		case InitDone:
			return fmt.Errorf("already initialized")
		case InitInProgress:
			fallthrough
		default:
			return fmt.Errorf("already started")
		}
	}

	if err := b.postProver.SetParams(dataDir, space); err != nil {
		return err
	}
	b.SetCoinbaseAccount(rewardAddress)

	initialized, _, err := b.postProver.IsInitialized()
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
		close(b.initDone)
	}()

	return nil
}

// MiningStats returns state of post init, coinbase reward account and data directory path for post commitment
func (b *Builder) MiningStats() (int, uint64, string, string) {
	acc := b.getCoinbaseAccount()
	initStatus := atomic.LoadInt32(&b.initStatus)
	remainingBytes := uint64(0)
	if initStatus == InitInProgress {
		var err error
		_, remainingBytes, err = b.postProver.IsInitialized()
		if err != nil {
			b.log.With().Error("failed to check remaining init bytes", log.Err(err))
		}
	}
	datadir := b.postProver.Cfg().DataDir
	return int(initStatus), remainingBytes, acc.String(), datadir
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

// PublishActivationTx attempts to publish an atx, it returns an error if an atx cannot be created.
func (b *Builder) PublishActivationTx() error {
	b.discardChallengeIfStale()
	if b.challenge != nil {
		b.log.With().Info("using existing atx challenge", log.EpochId(uint64(b.currentEpoch())))
	} else {
		b.log.With().Info("building new atx challenge", log.EpochId(uint64(b.currentEpoch())))
		err := b.buildNipstChallenge()
		if err != nil {
			return err
		}
	}
	pubEpoch := b.challenge.PubLayerIdx.GetEpoch(b.layersPerEpoch)

	hash, err := b.challenge.Hash()
	if err != nil {
		return fmt.Errorf("getting challenge hash failed: %v", err)
	}
	// ⏳ the following method waits for a PoET proof, which should take ~1 epoch
	atxExpired := b.layerClock.AwaitLayer((pubEpoch + 2).FirstLayer(b.layersPerEpoch)) // this fires when the target epoch is over
	nipst, err := b.nipstBuilder.BuildNIPST(hash, atxExpired, b.stop)
	if err != nil {
		if _, stopRequested := err.(StopRequestedError); stopRequested {
			return err
		}
		return fmt.Errorf("failed to build nipst: %v", err)
	}

	b.log.With().Info("awaiting atx publication epoch",
		log.Uint64("pub_epoch", uint64(pubEpoch)),
		log.Uint64("pub_epoch_first_layer", uint64(pubEpoch.FirstLayer(b.layersPerEpoch))),
		log.Uint64("current_layer", uint64(b.layerClock.GetCurrentLayer())),
	)
	if err := b.waitOrStop(b.layerClock.AwaitLayer(pubEpoch.FirstLayer(b.layersPerEpoch))); err != nil {
		return err
	}
	b.log.Info("publication epoch has arrived!")
	if discarded := b.discardChallengeIfStale(); discarded {
		return fmt.Errorf("atx target epoch has passed during nipst construction")
	}

	// when we reach here an epoch has passed
	// we've completed the sequential work, now before publishing the atx,
	// we need to provide number of atx seen in the epoch of the positioning atx.

	// ensure we are synced before generating the ATX's view
	if err := b.waitOrStop(b.syncer.Await()); err != nil {
		return err
	}
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
	if activeSetSize == 0 && !(pubEpoch + 1).IsGenesis() {
		return fmt.Errorf("empty active set size found! epochId: %v, len(view): %d, view: %v",
			pubEpoch, len(view), view)
	}

	var commitment *types.PostProof
	if b.challenge.PrevATXId == *types.EmptyAtxId {
		commitment = b.commitment
	}

	atx := types.NewActivationTx(*b.challenge, b.getCoinbaseAccount(), activeSetSize, view, nipst, commitment)

	b.log.With().Info("active ids seen for epoch", log.Uint64("atx_pub_epoch", uint64(pubEpoch)),
		log.Uint32("view_cnt", activeSetSize))

	atxReceived := b.db.AwaitAtx(atx.Id())
	defer b.db.UnsubscribeAtx(atx.Id())
	size, err := b.signAndBroadcast(atx)
	if err != nil {
		return err
	}

	commitStr := "nil"
	if commitment != nil {
		commitStr = commitment.String()
	}
	b.log.Event().Info("atx published!",
		log.AtxId(atx.ShortString()),
		log.String("prev_atx_id", atx.PrevATXId.ShortString()),
		log.String("pos_atx_id", atx.PositioningAtx.ShortString()),
		log.LayerId(uint64(atx.PubLayerIdx)),
		log.EpochId(uint64(atx.PubLayerIdx.GetEpoch(b.layersPerEpoch))),
		log.Uint32("active_set", atx.ActiveSetSize),
		log.String("miner", b.nodeId.ShortString()),
		log.Int("view", len(atx.View)),
		log.Uint64("sequence_number", atx.Sequence),
		log.String("NIPSTChallenge", hash.String()),
		log.String("commitment", commitStr),
		log.Int("atx_size", size),
	)
	events.Publish(events.AtxCreated{Created: true, Id: atx.ShortString(), Layer: uint64(b.currentEpoch())})

	select {
	case <-atxReceived:
		b.log.Info("atx received in db")
	case <-b.layerClock.AwaitLayer((atx.TargetEpoch(b.layersPerEpoch) + 1).FirstLayer(b.layersPerEpoch)):
		select {
		case <-atxReceived:
			b.log.Info("atx received in db (in the last moment)")
		case <-b.syncer.Await(): // ensure we've seen all blocks before concluding that the ATX was lost
			b.log.With().Error("target epoch has passed before atx was added to database",
				log.AtxId(atx.ShortString()))
			b.discardChallenge()
			return fmt.Errorf("target epoch has passed")
		case <-b.stop:
			return &StopRequestedError{}
		}
	case <-b.stop:
		return &StopRequestedError{}
	}
	b.discardChallenge()
	return nil
}

func (b *Builder) currentEpoch() types.EpochId {
	return b.layerClock.GetCurrentLayer().GetEpoch(b.layersPerEpoch)
}

func (b *Builder) discardChallenge() {
	b.challenge = nil
	if err := b.store.Put(b.getNipstKey(), []byte{}); err != nil {
		b.log.Error("failed to discard Nipst challenge: %v", err)
	}
}

func (b *Builder) signAndBroadcast(atx *types.ActivationTx) (int, error) {
	if err := b.SignAtx(atx); err != nil {
		return 0, fmt.Errorf("failed to sign ATX: %v", err)
	}
	buf, err := types.InterfaceToBytes(atx)
	if err != nil {
		return 0, fmt.Errorf("failed to serialize ATX: %v", err)
	}
	if err := b.net.Broadcast(AtxProtocol, buf); err != nil {
		return 0, fmt.Errorf("failed to broadcast ATX: %v", err)
	}
	return len(buf), nil
}

// GetPositioningAtx return the latest atx to be used as a positioning atx
func (b *Builder) GetPositioningAtx() (*types.ActivationTxHeader, error) {
	if id, err := b.db.GetPosAtxId(); err != nil {
		return nil, fmt.Errorf("cannot find pos atx: %v", err)
	} else if atx, err := b.db.GetAtxHeader(id); err != nil {
		return nil, fmt.Errorf("inconsistent state: failed to get atx header: %v", err)
	} else {
		return atx, nil
	}
}

func (b *Builder) GetPrevAtx(node types.NodeId) (*types.ActivationTxHeader, error) {
	if id, err := b.db.GetNodeLastAtxId(node); err != nil {
		return nil, fmt.Errorf("no prev atx found: %v", err)
	} else if atx, err := b.db.GetAtxHeader(id); err != nil {
		return nil, fmt.Errorf("inconsistent state: failed to get atx header: %v", err)
	} else {
		return atx, nil
	}
}

func (b *Builder) discardChallengeIfStale() bool {
	if b.challenge != nil && b.challenge.PubLayerIdx.GetEpoch(b.layersPerEpoch)+1 < b.currentEpoch() {
		b.log.With().Info("atx target epoch has already passed -- starting over",
			log.Uint64("target_epoch", uint64(b.challenge.PubLayerIdx.GetEpoch(b.layersPerEpoch)+1)),
			log.Uint64("current_epoch", uint64(b.currentEpoch())),
		)
		b.discardChallenge()
		return true
	}
	return false
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

func SignAtx(signer Signer, atx *types.ActivationTx) error {
	bts, err := atx.AtxBytes()
	if err != nil {
		return err
	}
	atx.Sig = signer.Sign(bts)
	return nil
}
