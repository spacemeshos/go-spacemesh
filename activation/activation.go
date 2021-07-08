// Package activation is responsible for creating activation transactions and running the mining flow, coordinating
// PoST building, sending proofs to PoET and building NIPoST structs.
package activation

import (
	"context"
	"errors"
	"fmt"
	"github.com/spacemeshos/ed25519"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/post/shared"
)

// AtxProtocol is the protocol id for broadcasting atxs over gossip
const AtxProtocol = "AtxGossip"

var totalWeightCache = NewTotalWeightCache(1000)

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
	BuildNIPoST(challenge *types.Hash32, timeout chan struct{}, stop chan struct{}) (*types.NIPoST, error)
}

type idStore interface {
	StoreNodeIdentity(id types.NodeID) error
	GetIdentity(id string) (types.NodeID, error)
}

type NIPoSTValidator interface {
	Validate(id signing.PublicKey, NIPoST *types.NIPoST, expectedChallenge types.Hash32, numUnits uint) error
	ValidatePoST(id []byte, PoST *types.PoST, PoSTMetadata *types.PoSTMetadata, numUnits uint) error
}

type atxDBProvider interface {
	GetAtxHeader(id types.ATXID) (*types.ActivationTxHeader, error)
	CalcTotalWeightFromView(view []types.BlockID, pubEpoch types.EpochID) (uint64, error)
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
	StartSmeshing(ctx context.Context, coinbase types.Address, opts PoSTSetupOpts) error
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
	LayersPerEpoch  uint16
}

type SmeshingStatus int32

const (
	SmeshingStatusIdle SmeshingStatus = iota
	SmeshingStatusPendingPostInit
	SmeshingStatusStarted
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
	layersPerEpoch    uint16
	db                atxDBProvider
	net               broadcaster
	mesh              meshProvider
	tickProvider      poetNumberOfTickProvider
	nipostBuilder     nipostBuilder
	postSetupProvider PoSTSetupProvider
	challenge         *types.NIPoSTChallenge
	initialPoST       *types.PoST
	layerClock        layerClock
	stop              chan struct{}
	status            SmeshingStatus
	mtx               sync.Mutex
	store             bytesStore
	syncer            syncer
	log               log.Log
	committedSpace    uint64
}

// NewBuilder returns an atx builder that will start a routine that will attempt to create an atx upon each new layer.
func NewBuilder(cfg Config, nodeID types.NodeID, signer signer, db atxDBProvider, net broadcaster, mesh meshProvider, layersPerEpoch uint16, nipostBuilder nipostBuilder, postSetupProvider PoSTSetupProvider, layerClock layerClock, syncer syncer, store bytesStore, log log.Log) *Builder {
	return &Builder{
		signer:            signer,
		nodeID:            nodeID,
		coinbaseAccount:   cfg.CoinbaseAccount,
		goldenATXID:       cfg.GoldenATXID,
		layersPerEpoch:    cfg.LayersPerEpoch,
		db:                db,
		net:               net,
		mesh:              mesh,
		nipostBuilder:     nipostBuilder,
		postSetupProvider: postSetupProvider,
		layerClock:        layerClock,
		stop:              make(chan struct{}),
		syncer:            syncer,
		store:             store,
		log:               log,
		status:            SmeshingStatusIdle,
	}
}

// Smeshing returns true iff atx builder started.
func (b *Builder) Smeshing() bool {
	b.mtx.Lock()
	defer b.mtx.Unlock()

	return b.status == SmeshingStatusStarted
}

// StartSmeshing is the main entry point of the atx builder.
// It runs the main loop of the builder and shouldn't be called more than once.
// If the post data is incomplete or missing, data creation
// session will be preceded. Changing of the post potions (e.g., number of labels),
// after initial setup, is supported.
func (b *Builder) StartSmeshing(ctx context.Context, coinbase types.Address, opts PoSTSetupOpts) error {
	b.mtx.Lock()
	if b.status != SmeshingStatusIdle {
		b.mtx.Unlock()
		return errors.New("already started")
	}
	b.stop = make(chan struct{})
	b.status = SmeshingStatusPendingPostInit
	b.mtx.Unlock()

	doneChan, err := b.postSetupProvider.StartSession(opts)
	if err != nil {
		b.status = SmeshingStatusIdle
		return fmt.Errorf("failed to start PoST setup session: %v", err)
	}

	go func() {
		<-doneChan
		if s := b.postSetupProvider.Status(); s.State != PoSTSetupStateComplete {
			b.status = SmeshingStatusIdle
			b.log.Error("failed to complete PoST setup: %v", b.postSetupProvider.LastError())
			return
		}

		b.status = SmeshingStatusStarted
		go b.loop(ctx)
	}()

	return nil
}

// StopSmeshing stops the atx builder.
func (b *Builder) StopSmeshing(deleteFiles bool) error {
	b.mtx.Lock()
	defer b.mtx.Unlock()

	if b.status == SmeshingStatusIdle {
		return errors.New("not started")
	}

	if err := b.postSetupProvider.StopSession(deleteFiles); err != nil {
		return fmt.Errorf("failed to stop post data creation session: %v", err)
	}

	close(b.stop)
	b.status = SmeshingStatusIdle

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

// StopRequestedError is a specific type of error the indicated a user has stopped mining
type StopRequestedError struct{}

func (s StopRequestedError) Error() string { return "stop requested" }

func (b *Builder) waitOrStop(ch chan struct{}) error {
	select {
	case <-ch:
		return nil
	case <-b.stop:
		return StopRequestedError{}
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
	b.initialPoST, _, err = b.postSetupProvider.GenerateProof(shared.ZeroChallenge)
	if err != nil {
		b.log.Error("PoST execution failed: %v", err)
		b.status = SmeshingStatusIdle
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
		if err := b.PublishActivationTx(ctx); err != nil {
			b.log.WithContext(ctx).With().Error("error attempting to publish atx",
				b.layerClock.GetCurrentLayer(),
				b.currentEpoch(),
				log.Err(err))
			if _, stopRequested := err.(StopRequestedError); stopRequested {
				return
			}
			currentLayer := b.layerClock.GetCurrentLayer()
			b.log.With().Error("atx construction errored", log.Err(err), currentLayer, currentLayer.GetEpoch())
			events.ReportAtxCreated(false, uint64(b.currentEpoch()), "")
			select {
			case <-b.stop:
				return
			case <-b.layerClock.AwaitLayer(currentLayer + 1):
				continue
			}
		}
	}
}

func (b *Builder) buildNIPoSTChallenge(ctx context.Context) error {
	syncedCh := make(chan struct{})
	b.syncer.RegisterChForSynced(ctx, syncedCh)
	<-syncedCh
	challenge := &types.NIPoSTChallenge{NodeID: b.nodeID}
	atxID, pubLayerID, endTick, err := b.GetPositioningAtxInfo()
	if err != nil {
		return fmt.Errorf("failed to get positioning ATX: %v", err)
	}
	challenge.PositioningATX = atxID
	challenge.PubLayerID = pubLayerID.Add(b.layersPerEpoch)
	challenge.StartTick = endTick
	challenge.EndTick = endTick + b.tickProvider.NumOfTicks()
	if prevAtx, err := b.GetPrevAtx(b.nodeID); err != nil {
		challenge.InitialPoSTIndices = b.initialPoST.Indices
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

// SetCoinbase sets the address rewardAddress to be the coinbase account written into the activation transaction
// the rewards for blocks made by this miner will go to this address
func (b *Builder) SetCoinbase(rewardAddress types.Address) {
	b.accountLock.Lock()
	b.coinbaseAccount = rewardAddress
	b.accountLock.Unlock()
}

// Coinbase returns the current coinbase address.
func (b *Builder) Coinbase() types.Address {
	b.accountLock.RLock()
	acc := b.coinbaseAccount
	b.accountLock.RUnlock()
	return acc
}

// MinGas [...]
func (b *Builder) MinGas() uint64 {
	panic("not implemented")
}

// SetMinGas [...]
func (b *Builder) SetMinGas(value uint64) {
	panic("not implemented")
}

func (b *Builder) getNIPoSTKey() []byte {
	return []byte("NIPoST")
}

func (b *Builder) storeChallenge(ch *types.NIPoSTChallenge) error {
	bts, err := types.InterfaceToBytes(ch)
	if err != nil {
		return err
	}
	return b.store.Put(b.getNIPoSTKey(), bts)
}

func (b *Builder) loadChallenge() error {
	bts, err := b.store.Get(b.getNIPoSTKey())
	if err != nil {
		return err
	}
	if len(bts) > 0 {
		tp := &types.NIPoSTChallenge{}
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
		err := b.buildNIPoSTChallenge(ctx)
		if err != nil {
			b.log.With().Error("failed to build new atx challenge", log.Err(err))
			return err
		}
	}

	b.log.With().Info("challenge ready")

	pubEpoch := b.challenge.PubLayerID.GetEpoch()

	hash, err := b.challenge.Hash()
	if err != nil {
		b.log.With().Error("getting challenge hash failed", log.Err(err))
		return fmt.Errorf("getting challenge hash failed: %v", err)
	}

	// ⏳ the following method waits for a PoET proof, which should take ~1 epoch
	atxExpired := b.layerClock.AwaitLayer((pubEpoch + 2).FirstLayer()) // this fires when the target epoch is over

	b.log.With().Info("build NIPST")

	nipost, err := b.nipostBuilder.BuildNIPoST(hash, atxExpired, b.stop)
	if err != nil {
		if _, stopRequested := err.(StopRequestedError); stopRequested {
			return err
		}
		b.log.With().Error("failed to build nipost", log.Err(err))
		return fmt.Errorf("failed to build nipost: %v", err)
	}

	b.log.With().Info("awaiting atx publication epoch",
		log.FieldNamed("pub_epoch", pubEpoch),
		log.FieldNamed("pub_epoch_first_layer", pubEpoch.FirstLayer()),
		log.FieldNamed("current_layer", b.layerClock.GetCurrentLayer()),
	)
	if err := b.waitOrStop(b.layerClock.AwaitLayer(pubEpoch.FirstLayer())); err != nil {
		b.log.With().Error("failed to wait of publication epoch", log.Err(err))
		return err
	}
	b.log.Info("publication epoch has arrived!")
	if discarded := b.discardChallengeIfStale(); discarded {
		return fmt.Errorf("atx target epoch has passed during nipost construction")
	}

	// when we reach here an epoch has passed
	// we've completed the sequential work, now before publishing the atx,
	// we need to provide number of atx seen in the epoch of the positioning atx.

	// ensure we are synced before generating the ATX's view
	syncedCh := make(chan struct{})
	b.syncer.RegisterChForSynced(ctx, syncedCh)
	if err := b.waitOrStop(syncedCh); err != nil {
		return err
	}

	var initialPoST *types.PoST
	if b.challenge.PrevATXID == *types.EmptyATXID {
		initialPoST = b.initialPoST
	}

	atx := types.NewActivationTx(*b.challenge, b.Coinbase(), nipost, b.postSetupProvider.LastOpts().NumUnits, initialPoST)

	atxReceived := b.db.AwaitAtx(atx.ID())
	defer b.db.UnsubscribeAtx(atx.ID())
	size, err := b.signAndBroadcast(ctx, atx)
	if err != nil {
		b.log.With().Error("failed to publish atx", append(atx.Fields(size), log.Err(err))...)
		return err
	}

	b.log.Event().Info("atx published", atx.Fields(size)...)
	events.ReportAtxCreated(true, uint64(b.currentEpoch()), atx.ShortString())

	select {
	case <-atxReceived:
		b.log.Info("atx received in db")
	case <-b.layerClock.AwaitLayer((atx.TargetEpoch() + 1).FirstLayer()):
		syncedCh := make(chan struct{})
		b.syncer.RegisterChForSynced(ctx, syncedCh)
		select {
		case <-atxReceived:
			b.log.Info("atx received in db (in the last moment)")
		case <-syncedCh: // ensure we've seen all blocks before concluding that the ATX was lost
			b.log.With().Error("target epoch has passed before atx was added to database", atx.ID())
			b.discardChallenge()
			return fmt.Errorf("target epoch has passed")
		case <-b.stop:
			return StopRequestedError{}
		}
	case <-b.stop:
		return StopRequestedError{}
	}
	b.discardChallenge()
	return nil
}

func (b *Builder) currentEpoch() types.EpochID {
	return b.layerClock.GetCurrentLayer().GetEpoch()
}

func (b *Builder) discardChallenge() {
	b.challenge = nil
	if err := b.store.Put(b.getNIPoSTKey(), []byte{}); err != nil {
		b.log.Error("failed to discard NIPoST challenge: %v", err)
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
		return types.ATXID{}, 0, 0, fmt.Errorf("cannot find pos atx: %v", err)
	} else if id == b.goldenATXID {
		b.log.With().Info("using golden atx as positioning atx", id)
		return id, 0, 0, nil
	} else if atx, err := b.db.GetAtxHeader(id); err != nil {
		return types.ATXID{}, 0, 0, fmt.Errorf("inconsistent state: failed to get atx header: %v", err)
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
