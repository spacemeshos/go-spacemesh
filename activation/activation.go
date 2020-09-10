// Package activation is responsible for creating activation transactions and running the mining flow, coordinating
// PoST building, sending proofs to PoET and building NIPoST structs.
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

// AtxProtocol is the protocol id for broadcasting atxs over gossip
const AtxProtocol = "AtxGossip"

var activesetCache = NewActivesetCache(1000)

type meshProvider interface {
	GetOrphanBlocksBefore(l types.LayerID) ([]types.BlockID, error)
	LatestLayer() types.LayerID
}

type broadcaster interface {
	Broadcast(channel string, data []byte) error
}

type poetNumberOfTickProvider struct {
}

func (provider *poetNumberOfTickProvider) NumOfTicks() uint64 {
	return 0
}

type nipstBuilder interface {
	BuildNIPST(challenge *types.Hash32, timeout chan struct{}, stop chan struct{}) (*types.NIPST, error)
}

type idStore interface {
	StoreNodeIdentity(id types.NodeID) error
	GetIdentity(id string) (types.NodeID, error)
}

type nipstValidator interface {
	Validate(id signing.PublicKey, nipst *types.NIPST, expectedChallenge types.Hash32) error
	VerifyPost(id signing.PublicKey, proof *types.PostProof, space uint64) error
}

type atxDBProvider interface {
	GetAtxHeader(id types.ATXID) (*types.ActivationTxHeader, error)
	CalcActiveSetFromView(view []types.BlockID, pubEpoch types.EpochID) (uint32, error)
	GetNodeLastAtxID(nodeID types.NodeID) (types.ATXID, error)
	GetPosAtxID() (types.ATXID, error)
	AwaitAtx(id types.ATXID) chan struct{}
	UnsubscribeAtx(id types.ATXID)
}

type bytesStore interface {
	Put(key []byte, buf []byte) error
	Get(key []byte) ([]byte, error)
}

type signer interface {
	Sign(m []byte) []byte
}

const (
	// InitIdle status means that an init file does not exist and is not prepared
	InitIdle = 1 + iota
	// InitInProgress status means that an init file preparation is now in progress
	InitInProgress
	// InitDone status indicates there is a prepared init file
	InitDone
)

// Builder struct is the struct that orchestrates the creation of activation transactions
// it is responsible for initializing post, receiving poet proof and orchestrating nipst. after which it will
// calculate active set size and providing relevant view as proof
type Builder struct {
	signer
	nodeID          types.NodeID
	coinbaseAccount types.Address
	db              atxDBProvider
	net             broadcaster
	mesh            meshProvider
	layersPerEpoch  uint16
	tickProvider    poetNumberOfTickProvider
	nipstBuilder    nipstBuilder
	postProver      PostProverClient
	challenge       *types.NIPSTChallenge
	commitment      *types.PostProof
	layerClock      layerClock
	stop            chan struct{}
	started         uint32
	store           bytesStore
	syncer          syncer
	accountLock     sync.RWMutex
	initStatus      int32
	initDone        chan struct{}
	log             log.Log
}

type layerClock interface {
	AwaitLayer(layerID types.LayerID) chan struct{}
	GetCurrentLayer() types.LayerID
}

type syncer interface {
	Await() chan struct{}
}

// NewBuilder returns an atx builder that will start a routine that will attempt to create an atx upon each new layer.
func NewBuilder(nodeID types.NodeID, coinbaseAccount types.Address, signer signer, db atxDBProvider, net broadcaster, mesh meshProvider, layersPerEpoch uint16, nipstBuilder nipstBuilder, postProver PostProverClient, layerClock layerClock, syncer syncer, store bytesStore, log log.Log) *Builder {
	return &Builder{
		signer:          signer,
		nodeID:          nodeID,
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

// GetSmesherID returns the ID of the smesher that created this activation
func (b *Builder) GetSmesherID() types.NodeID {
	return b.nodeID
}

// SignAtx signs the atx and assigns the signature into atx.Sig
// this function returns an error if atx could not be converted to bytes
func (b *Builder) SignAtx(atx *types.ActivationTx) error {
	return SignAtx(b, atx)
}

// StopRequestedError is a specific type of error the indicated a user has stopped mining
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
			events.ReportAtxCreated(false, uint64(b.currentEpoch()), "")
			<-b.layerClock.AwaitLayer(b.layerClock.GetCurrentLayer() + 1)
		}
	}
}

func (b *Builder) buildNipstChallenge(currentLayer types.LayerID) error {
	<-b.syncer.Await()
	challenge := &types.NIPSTChallenge{NodeID: b.nodeID}
	if posAtx, err := b.GetPositioningAtx(); err != nil {
		if b.currentEpoch() != 0 {
			return fmt.Errorf("failed to get positioning ATX: %v", err)
		}
		challenge.EndTick = b.tickProvider.NumOfTicks()
		challenge.PubLayerID = currentLayer.Add(b.layersPerEpoch)
	} else {
		challenge.PositioningATX = posAtx.ID()
		challenge.PubLayerID = posAtx.PubLayerID.Add(b.layersPerEpoch)
		challenge.StartTick = posAtx.EndTick
		challenge.EndTick = posAtx.EndTick + b.tickProvider.NumOfTicks()
	}
	if prevAtx, err := b.GetPrevAtx(b.nodeID); err != nil {
		challenge.CommitmentMerkleRoot = b.commitment.MerkleRoot
	} else {
		challenge.PrevATXID = prevAtx.ID()
		challenge.Sequence = prevAtx.Sequence + 1
	}
	b.challenge = challenge
	if err := b.storeChallenge(b.challenge); err != nil {
		return fmt.Errorf("failed to store nipst challenge: %v", err)
	}
	return nil
}

// StartPost initiates post commitment generation process. It returns an error if a process is already in progress or
// if a post has been already initialized
func (b *Builder) StartPost(rewardAddress types.Address, dataDir string, space uint64) error {
	if !atomic.CompareAndSwapInt32(&b.initStatus, InitIdle, InitInProgress) {
		switch atomic.LoadInt32(&b.initStatus) {
		case InitDone:
			return fmt.Errorf("already initialized")
		case InitInProgress:
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

// SetCoinbaseAccount sets the address rewardAddress to be the coinbase account written into the activation transaction
// the rewards for blocks made by this miner will go to this address
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

func (b *Builder) getNipstKey() []byte {
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
		b.log.With().Info("using existing atx challenge", b.currentEpoch())
	} else {
		b.log.With().Info("building new atx challenge", b.currentEpoch())
		err := b.buildNipstChallenge(b.layerClock.GetCurrentLayer())
		if err != nil {
			return err
		}
	}
	pubEpoch := b.challenge.PubLayerID.GetEpoch()

	hash, err := b.challenge.Hash()
	if err != nil {
		return fmt.Errorf("getting challenge hash failed: %v", err)
	}
	// ⏳ the following method waits for a PoET proof, which should take ~1 epoch
	atxExpired := b.layerClock.AwaitLayer((pubEpoch + 2).FirstLayer()) // this fires when the target epoch is over
	nipst, err := b.nipstBuilder.BuildNIPST(hash, atxExpired, b.stop)
	if err != nil {
		if _, stopRequested := err.(StopRequestedError); stopRequested {
			return err
		}
		return fmt.Errorf("failed to build nipst: %v", err)
	}

	b.log.With().Info("awaiting atx publication epoch",
		log.FieldNamed("pub_epoch", pubEpoch),
		log.FieldNamed("pub_epoch_first_layer", pubEpoch.FirstLayer()),
		log.FieldNamed("current_layer", b.layerClock.GetCurrentLayer()),
	)
	if err := b.waitOrStop(b.layerClock.AwaitLayer(pubEpoch.FirstLayer())); err != nil {
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

	var activeSetSize uint32
	var commitment *types.PostProof
	if b.challenge.PrevATXID == *types.EmptyATXID {
		commitment = b.commitment
	}

	atx := types.NewActivationTx(*b.challenge, b.getCoinbaseAccount(), nipst, commitment)

	b.log.With().Info("active ids seen for epoch", log.FieldNamed("atx_pub_epoch", pubEpoch),
		log.Uint32("view_cnt", activeSetSize))

	atxReceived := b.db.AwaitAtx(atx.ID())
	defer b.db.UnsubscribeAtx(atx.ID())
	size, err := b.signAndBroadcast(atx)
	if err != nil {
		b.log.With().Error("failed to publish atx", append(atx.Fields(size), log.Err(err))...)
		return err
	}

	b.log.Event().Info("atx published!", atx.Fields(size)...)
	events.ReportAtxCreated(true, uint64(b.currentEpoch()), atx.ShortString())

	select {
	case <-atxReceived:
		b.log.Info("atx received in db")
	case <-b.layerClock.AwaitLayer((atx.TargetEpoch() + 1).FirstLayer()):
		select {
		case <-atxReceived:
			b.log.Info("atx received in db (in the last moment)")
		case <-b.syncer.Await(): // ensure we've seen all blocks before concluding that the ATX was lost
			b.log.With().Error("target epoch has passed before atx was added to database", atx.ID())
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

func (b *Builder) currentEpoch() types.EpochID {
	return b.layerClock.GetCurrentLayer().GetEpoch()
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
	if id, err := b.db.GetPosAtxID(); err != nil {
		return nil, fmt.Errorf("cannot find pos atx: %v", err)
	} else if atx, err := b.db.GetAtxHeader(id); err != nil {
		return nil, fmt.Errorf("inconsistent state: failed to get atx header: %v", err)
	} else {
		return atx, nil
	}
}

// GetPrevAtx gets the last atx header of specified node Id, it returns error if no previous atx found or if no
// AtxHeader struct in db
func (b *Builder) GetPrevAtx(node types.NodeID) (*types.ActivationTxHeader, error) {
	if id, err := b.db.GetNodeLastAtxID(node); err != nil {
		return nil, fmt.Errorf("no prev atx found: %v", err)
	} else if atx, err := b.db.GetAtxHeader(id); err != nil {
		return nil, fmt.Errorf("inconsistent state: failed to get atx header: %v", err)
	} else {
		return atx, nil
	}
}

func (b *Builder) discardChallengeIfStale() bool {
	if b.challenge != nil && b.challenge.PubLayerID.GetEpoch()+1 < b.currentEpoch() {
		b.log.With().Info("atx target epoch has already passed -- starting over",
			log.FieldNamed("target_epoch", b.challenge.PubLayerID.GetEpoch()+1),
			log.FieldNamed("current_epoch", b.currentEpoch()),
		)
		b.discardChallenge()
		return true
	}
	return false
}

// ExtractPublicKey extracts public key from message and verifies public key exists in idStore, this is how we validate
// ATX signature. If this is the first ATX it is considered valid anyways and ATX syntactic validation will determine ATX validity
func ExtractPublicKey(signedAtx *types.ActivationTx) (*signing.PublicKey, error) {
	bts, err := signedAtx.InnerBytes()
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

// SignAtx signs the atx atx with specified signer and assigns the signature into atx.Sig
// this function returns an error if atx could not be converted to bytes
func SignAtx(signer signer, atx *types.ActivationTx) error {
	bts, err := atx.InnerBytes()
	if err != nil {
		return err
	}
	atx.Sig = signer.Sign(bts)
	return nil
}
