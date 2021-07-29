// Package activation is responsible for creating activation transactions and running the mining flow, coordinating
// Post building, sending proofs to PoET and building NIPost structs.
package activation

import (
	"context"
	"errors"
	"fmt"
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/post/shared"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
)

var (
	// ErrStopRequested is returned when builder is stopped.
	ErrStopRequested = errors.New("builder: stop requested")
	// ErrATXChallengeExpired is returned when atx missed its publication window and needs to be regenerated.
	ErrATXChallengeExpired = errors.New("builder: atx expired")
	// ErrPoetServiceUnstable is returned when poet quality of service is low.
	ErrPoetServiceUnstable = errors.New("builder: poet service is unstable")
)

// AtxProtocol is the protocol id for broadcasting atxs over gossip
const AtxProtocol = "AtxGossip"

const defaultPoetRetryInterval = 5 * time.Second

type meshProvider interface {
	GetOrphanBlocksBefore(l types.LayerID) ([]types.BlockID, error)
	LatestLayer() types.LayerID
}

type broadcaster interface {
	Broadcast(ctx context.Context, channel string, data []byte) error
}

type poetNumberOfTickProvider struct {
}

func (provider *poetNumberOfTickProvider) NumOfTicks() uint64 {
	return 1
}

type nipostBuilder interface {
	updatePoETProver(PoetProvingServiceClient)
	BuildNIPost(ctx context.Context, challenge *types.Hash32, timeout chan struct{}) (*types.NIPost, error)
}

type idStore interface {
	StoreNodeIdentity(id types.NodeID) error
	GetIdentity(id string) (types.NodeID, error)
}

type nipostValidator interface {
	Validate(id signing.PublicKey, NIPost *types.NIPost, expectedChallenge types.Hash32, numUnits uint) error
	ValidatePost(id []byte, Post *types.Post, PostMetadata *types.PostMetadata, numUnits uint) error
}

type atxDBProvider interface {
	GetAtxHeader(id types.ATXID) (*types.ActivationTxHeader, error)
	GetNodeLastAtxID(nodeID types.NodeID) (types.ATXID, error)
	GetPosAtxID() (types.ATXID, error)
	GetAtxTimestamp(id types.ATXID) (time.Time, error)
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

type layerClock interface {
	AwaitLayer(layerID types.LayerID) chan struct{}
	GetCurrentLayer() types.LayerID
}

type syncer interface {
	RegisterChForSynced(context.Context, chan struct{})
}

// SmeshingProvider defines the functionality required for the node's Smesher API.
type SmeshingProvider interface {
	Smeshing() bool
	StartSmeshing(ctx context.Context, coinbase types.Address, opts PostSetupOpts) error
	StopSmeshing(deleteFiles bool) error
	SmesherID() types.NodeID
	Coinbase() types.Address
	SetCoinbase(coinbase types.Address)
	MinGas() uint64
	SetMinGas(value uint64)
}

// A compile time check to ensure that Builder fully implements the SmeshingProvider interface.
var _ SmeshingProvider = (*Builder)(nil)

// Config defines configuration for Builder
type Config struct {
	CoinbaseAccount types.Address
	GoldenATXID     types.ATXID
	LayersPerEpoch  uint32
}

type smeshingStatus int32

const (
	smeshingStatusIdle smeshingStatus = iota
	smeshingStatusPendingPostSetup
	smeshingStatusStarted
)

// Builder struct is the struct that orchestrates the creation of activation transactions
// it is responsible for initializing post, receiving poet proof and orchestrating nipst. after which it will
// calculate total weight and providing relevant view as proof
type Builder struct {
	signer
	accountLock       sync.RWMutex
	nodeID            types.NodeID
	coinbaseAccount   types.Address
	goldenATXID       types.ATXID
	layersPerEpoch    uint32
	db                atxDBProvider
	net               broadcaster
	mesh              meshProvider
	tickProvider      poetNumberOfTickProvider
	nipostBuilder     nipostBuilder
	postSetupProvider PostSetupProvider
	challenge         *types.NIPostChallenge
	initialPost       *types.Post
	// pendingATX is created with current commitment and nipst from current challenge.
	pendingATX            *types.ActivationTx
	layerClock            layerClock
	status                smeshingStatus
	mtx                   sync.Mutex
	store                 bytesStore
	syncer                syncer
	log                   log.Log
	runCtx                context.Context
	stop                  func()
	poetRetryInterval     time.Duration
	poetClientInitializer PoETClientInitializer
	// pendingPoetClient is modified using atomic operations on unsafe.Pointer
	pendingPoetClient unsafe.Pointer
}

// BuilderOption ...
type BuilderOption func(*Builder)

// WithPoetRetryInterval modifies time that builder will have to wait before retrying ATX build process
// if it failed due to issues with PoET server.
func WithPoetRetryInterval(interval time.Duration) BuilderOption {
	return func(b *Builder) {
		b.poetRetryInterval = interval
	}
}

// PoETClientInitializer interfaces for creating PoetProvingServiceClient.
type PoETClientInitializer func(string) PoetProvingServiceClient

// WithPoETClientInitializer modifies initialization logic for PoET client. Used during client update.
func WithPoETClientInitializer(initializer PoETClientInitializer) BuilderOption {
	return func(b *Builder) {
		b.poetClientInitializer = initializer
	}
}

// NewBuilder returns an atx builder that will start a routine that will attempt to create an atx upon each new layer.
func NewBuilder(conf Config, nodeID types.NodeID, signer signer, db atxDBProvider, net broadcaster, mesh meshProvider,
	layersPerEpoch uint32, nipostBuilder nipostBuilder, postSetupProvider PostSetupProvider, layerClock layerClock,
	syncer syncer, store bytesStore, log log.Log, opts ...BuilderOption) *Builder {
	b := &Builder{
		signer:            signer,
		nodeID:            nodeID,
		coinbaseAccount:   conf.CoinbaseAccount,
		goldenATXID:       conf.GoldenATXID,
		layersPerEpoch:    conf.LayersPerEpoch,
		db:                db,
		net:               net,
		mesh:              mesh,
		nipostBuilder:     nipostBuilder,
		postSetupProvider: postSetupProvider,
		layerClock:        layerClock,
		store:             store,
		syncer:            syncer,
		log:               log,
		poetRetryInterval: defaultPoetRetryInterval,
		status:            smeshingStatusIdle,
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Smeshing returns true iff atx builder started.
func (b *Builder) Smeshing() bool {
	b.mtx.Lock()
	defer b.mtx.Unlock()

	return b.status == smeshingStatusStarted
}

// StartSmeshing is the main entry point of the atx builder.
// It runs the main loop of the builder and shouldn't be called more than once.
// If the post data is incomplete or missing, data creation
// session will be preceded. Changing of the post potions (e.g., number of labels),
// after initial setup, is supported.
func (b *Builder) StartSmeshing(ctx context.Context, coinbase types.Address, opts PostSetupOpts) error {
	b.mtx.Lock()
	if b.status != smeshingStatusIdle {
		b.mtx.Unlock()
		return errors.New("already started")
	}
	b.status = smeshingStatusPendingPostSetup
	b.runCtx, b.stop = context.WithCancel(ctx)
	b.mtx.Unlock()

	doneChan, err := b.postSetupProvider.StartSession(opts)
	if err != nil {
		b.status = smeshingStatusIdle
		return fmt.Errorf("failed to start Post setup session: %v", err)
	}

	go func() {
		<-doneChan
		if s := b.postSetupProvider.Status(); s.State != postSetupStateComplete {
			b.status = smeshingStatusIdle
			b.log.Error("failed to complete Post setup: %v", b.postSetupProvider.LastError())
			return
		}

		b.status = smeshingStatusStarted
		go b.loop(b.runCtx)
	}()

	return nil
}

// StopSmeshing stops the atx builder.
func (b *Builder) StopSmeshing(deleteFiles bool) error {
	b.mtx.Lock()
	defer b.mtx.Unlock()

	if b.status == smeshingStatusIdle {
		return errors.New("not started")
	}

	if err := b.postSetupProvider.StopSession(deleteFiles); err != nil {
		return fmt.Errorf("failed to stop post data creation session: %v", err)
	}

	b.stop()
	b.status = smeshingStatusIdle

	return nil
}

// SmesherID returns the ID of the smesher that created this activation
func (b *Builder) SmesherID() types.NodeID {
	return b.nodeID
}

// SignAtx signs the atx and assigns the signature into atx.Sig
// this function returns an error if atx could not be converted to bytes
func (b *Builder) SignAtx(atx *types.ActivationTx) error {
	return SignAtx(b, atx)
}

func (b *Builder) waitOrStop(ctx context.Context, ch chan struct{}) error {
	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ErrStopRequested
	}
}

// loop is the main loop that tries to create an atx per tick received from the global clock
func (b *Builder) loop(ctx context.Context) {
	err := b.loadChallenge()
	if err != nil {
		log.Info("challenge not loaded: %s", err)
	}

	// Once initialized, run the execution phase with zero-challenge,
	// to create the initial proof (the commitment).
	// TODO(moshababo): don't generate the commitment every time smeshing is starting, but once only.
	b.initialPost, _, err = b.postSetupProvider.GenerateProof(shared.ZeroChallenge)
	if err != nil {
		b.log.Error("Post execution failed: %v", err)
		b.status = smeshingStatusIdle
		return
	}

	// ensure layer 1 has arrived
	if err := b.waitOrStop(ctx, b.layerClock.AwaitLayer(types.NewLayerID(1))); err != nil {
		return
	}
	b.run(ctx)
}

func (b *Builder) run(ctx context.Context) {
	var poetRetryTimer *time.Timer
	defer func() {
		if poetRetryTimer != nil {
			poetRetryTimer.Stop()
		}
	}()
	defer b.log.Info("atx builder is stopped")
	for {
		client := atomic.LoadPointer(&b.pendingPoetClient)
		if client != nil {
			b.nipostBuilder.updatePoETProver(*(*PoetProvingServiceClient)(client))
			// CaS here will not lose concurrent update
			atomic.CompareAndSwapPointer(&b.pendingPoetClient, client, nil)
		}

		if err := b.PublishActivationTx(ctx); err != nil {
			if errors.Is(err, ErrStopRequested) {
				return
			}
			b.log.With().Error("error attempting to publish atx",
				b.layerClock.GetCurrentLayer(),
				b.currentEpoch(),
				log.Err(err))
			events.ReportAtxCreated(false, uint32(b.currentEpoch()), "")

			switch {
			case errors.Is(err, ErrATXChallengeExpired):
				// can be retried immediatly with a new challenge
			case errors.Is(err, ErrPoetServiceUnstable):
				if poetRetryTimer == nil {
					poetRetryTimer = time.NewTimer(b.poetRetryInterval)
				} else {
					poetRetryTimer.Reset(b.poetRetryInterval)
				}
				select {
				case <-ctx.Done():
					return
				case <-poetRetryTimer.C:
				}
			default:
				// other failures are related to in-process software. we may as well panic here
				currentLayer := b.layerClock.GetCurrentLayer()
				select {
				case <-ctx.Done():
					return
				case <-b.layerClock.AwaitLayer(currentLayer.Add(1)):
				}
			}
		}
	}
}

func (b *Builder) buildNIPostChallenge(ctx context.Context) error {
	syncedCh := make(chan struct{})
	b.syncer.RegisterChForSynced(ctx, syncedCh)
	<-syncedCh
	challenge := &types.NIPostChallenge{NodeID: b.nodeID}
	atxID, pubLayerID, endTick, err := b.GetPositioningAtxInfo()
	if err != nil {
		return fmt.Errorf("failed to get positioning ATX: %v", err)
	}
	challenge.PositioningATX = atxID
	challenge.PubLayerID = pubLayerID.Add(b.layersPerEpoch)
	challenge.StartTick = endTick
	challenge.EndTick = endTick + b.tickProvider.NumOfTicks()
	if prevAtx, err := b.GetPrevAtx(b.nodeID); err != nil {
		challenge.InitialPostIndices = b.initialPost.Indices
	} else {
		challenge.PrevATXID = prevAtx.ID()
		challenge.Sequence = prevAtx.Sequence + 1
	}
	b.challenge = challenge
	if err := b.storeChallenge(b.challenge); err != nil {
		return fmt.Errorf("failed to store nipost challenge: %v", err)
	}
	return nil
}

// UpdatePoETServer updates poet client. Context is used to verify that the target is responsive.
func (b *Builder) UpdatePoETServer(ctx context.Context, target string) error {
	client := b.poetClientInitializer(target)
	// TODO(dshulyak) not enough information to verify that PoetServiceID matches with an expected one.
	// Maybe it should be provided during update.
	_, err := client.PoetServiceID(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPoetServiceUnstable, err)
	}
	atomic.StorePointer(&b.pendingPoetClient, unsafe.Pointer(&client))
	return nil
}

// SetCoinbase sets the address rewardAddress to be the coinbase account written into the activation transaction
// the rewards for blocks made by this miner will go to this address
func (b *Builder) SetCoinbase(rewardAddress types.Address) {
	b.accountLock.Lock()
	defer b.accountLock.Unlock()
	b.coinbaseAccount = rewardAddress
}

// Coinbase returns the current coinbase address.
func (b *Builder) Coinbase() types.Address {
	b.accountLock.RLock()
	defer b.accountLock.RUnlock()
	return b.coinbaseAccount
}

// MinGas [...]
func (b *Builder) MinGas() uint64 {
	panic("not implemented")
}

// SetMinGas [...]
func (b *Builder) SetMinGas(value uint64) {
	panic("not implemented")
}

func (b *Builder) getNIPostKey() []byte {
	return []byte("NIPost")
}

func (b *Builder) storeChallenge(ch *types.NIPostChallenge) error {
	bts, err := types.InterfaceToBytes(ch)
	if err != nil {
		return err
	}
	return b.store.Put(b.getNIPostKey(), bts)
}

func (b *Builder) loadChallenge() error {
	bts, err := b.store.Get(b.getNIPostKey())
	if err != nil {
		return err
	}
	if len(bts) > 0 {
		tp := &types.NIPostChallenge{}
		err = types.BytesToInterface(bts, tp)
		if err != nil {
			return err
		}
		b.challenge = tp
	}
	return nil
}

// PublishActivationTx attempts to publish an atx, it returns an error if an atx cannot be created.
func (b *Builder) PublishActivationTx(ctx context.Context) error {
	b.discardChallengeIfStale()
	if b.challenge != nil {
		b.log.With().Info("using existing atx challenge", b.currentEpoch())
	} else {
		b.log.With().Info("building new atx challenge", b.currentEpoch())
		err := b.buildNIPostChallenge(ctx)
		if err != nil {
			b.log.Error(err.Error())
			return fmt.Errorf("failed to build new atx challenge: %w", err)
		}
	}

	b.log.With().Info("new atx challenge is ready", b.currentEpoch())

	if b.pendingATX == nil {
		var err error
		b.pendingATX, err = b.createAtx(ctx)
		if err != nil {
			b.log.Error(err.Error())
			return err
		}
	}

	atx := b.pendingATX
	atxReceived := b.db.AwaitAtx(atx.ID())
	defer b.db.UnsubscribeAtx(atx.ID())
	size, err := b.signAndBroadcast(ctx, atx)
	if err != nil {
		b.log.Error(err.Error())
		return err
	}

	b.log.Event().Info(fmt.Sprintf("atx published %v", atx.ID().ShortString()), atx.Fields(size)...)
	events.ReportAtxCreated(true, uint32(b.currentEpoch()), atx.ShortString())

	select {
	case <-atxReceived:
		b.log.With().Info(fmt.Sprintf("received atx in db %v", atx.ID().ShortString()), atx.ID())
	case <-b.layerClock.AwaitLayer((atx.TargetEpoch() + 1).FirstLayer()):
		syncedCh := make(chan struct{})
		b.syncer.RegisterChForSynced(ctx, syncedCh)
		select {
		case <-atxReceived:
			b.log.With().Info(fmt.Sprintf("received atx in db %v (in the last moment)", atx.ID().ShortString()), atx.ID())
		case <-syncedCh: // ensure we've seen all blocks before concluding that the ATX was lost
			b.discardChallenge()
			return fmt.Errorf("%w: target epoch has passed", ErrATXChallengeExpired)
		case <-ctx.Done():
			return ErrStopRequested
		}
	case <-ctx.Done():
		return ErrStopRequested
	}
	b.discardChallenge()
	return nil
}

func (b *Builder) createAtx(ctx context.Context) (*types.ActivationTx, error) {
	b.log.With().Info("challenge ready")

	pubEpoch := b.challenge.PubLayerID.GetEpoch()

	hash, err := b.challenge.Hash()
	if err != nil {
		return nil, fmt.Errorf("getting challenge hash failed: %w", err)
	}

	// the following method waits for a PoET proof, which should take ~1 epoch
	atxExpired := b.layerClock.AwaitLayer((pubEpoch + 2).FirstLayer()) // this fires when the target epoch is over

	b.log.With().Info("building NIPost")

	nipost, err := b.nipostBuilder.BuildNIPost(ctx, hash, atxExpired)
	if err != nil {
		return nil, fmt.Errorf("failed to build NIPost: %w", err)
	}

	b.log.With().Info("awaiting atx publication epoch",
		log.FieldNamed("pub_epoch", pubEpoch),
		log.FieldNamed("pub_epoch_first_layer", pubEpoch.FirstLayer()),
		log.FieldNamed("current_layer", b.layerClock.GetCurrentLayer()),
	)
	if err := b.waitOrStop(ctx, b.layerClock.AwaitLayer(pubEpoch.FirstLayer())); err != nil {
		return nil, fmt.Errorf("failed to wait of publication epoch: %w", err)
	}
	b.log.Info("publication epoch has arrived!")
	if discarded := b.discardChallengeIfStale(); discarded {
		return nil, fmt.Errorf("%w: atx target epoch has passed during nipost construction", ErrATXChallengeExpired)
	}

	// when we reach here an epoch has passed
	// we've completed the sequential work, now before publishing the atx,
	// we need to provide number of atx seen in the epoch of the positioning atx.

	// ensure we are synced before generating the ATX's view
	syncedCh := make(chan struct{})
	b.syncer.RegisterChForSynced(ctx, syncedCh)
	if err := b.waitOrStop(ctx, syncedCh); err != nil {
		return nil, err
	}

	var initialPost *types.Post
	if b.challenge.PrevATXID == *types.EmptyATXID {
		initialPost = b.initialPost
	}

	return types.NewActivationTx(*b.challenge, b.Coinbase(), nipost, b.postSetupProvider.LastOpts().NumUnits, initialPost), nil
}

func (b *Builder) currentEpoch() types.EpochID {
	return b.layerClock.GetCurrentLayer().GetEpoch()
}

func (b *Builder) discardChallenge() {
	b.challenge = nil
	b.pendingATX = nil
	if err := b.store.Put(b.getNIPostKey(), []byte{}); err != nil {
		b.log.Error("failed to discard NIPost challenge: %v", err)
	}
}

func (b *Builder) signAndBroadcast(ctx context.Context, atx *types.ActivationTx) (int, error) {
	if err := b.SignAtx(atx); err != nil {
		return 0, fmt.Errorf("failed to sign ATX: %v", err)
	}
	buf, err := types.InterfaceToBytes(atx)
	if err != nil {
		return 0, fmt.Errorf("failed to serialize ATX: %v", err)
	}
	if err := b.net.Broadcast(ctx, AtxProtocol, buf); err != nil {
		return 0, fmt.Errorf("failed to broadcast ATX: %v", err)
	}
	return len(buf), nil
}

// GetPositioningAtxInfo return the following details about the latest atx, to be used as a positioning atx:
// 	atxID, pubLayerID, endTick
func (b *Builder) GetPositioningAtxInfo() (types.ATXID, types.LayerID, uint64, error) {
	if id, err := b.db.GetPosAtxID(); err != nil {
		return types.ATXID{}, types.LayerID{}, 0, fmt.Errorf("cannot find pos atx: %v", err)
	} else if id == b.goldenATXID {
		b.log.With().Info("using golden atx as positioning atx", id)
		return id, types.LayerID{}, 0, nil
	} else if atx, err := b.db.GetAtxHeader(id); err != nil {
		return types.ATXID{}, types.LayerID{}, 0, fmt.Errorf("inconsistent state: failed to get atx header: %v", err)
	} else {
		return id, atx.PubLayerID, atx.EndTick, nil
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
