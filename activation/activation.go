// Package activation is responsible for creating activation transactions and running the mining flow, coordinating
// Post building, sending proofs to PoET and building NIPost structs.
package activation

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/spacemeshos/post/shared"
	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation/metrics"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
)

// PoetConfig is the configuration to interact with the poet server.
type PoetConfig struct {
	PhaseShift        time.Duration `mapstructure:"phase-shift"`
	CycleGap          time.Duration `mapstructure:"cycle-gap"`
	GracePeriod       time.Duration `mapstructure:"grace-period"`
	RequestRetryDelay time.Duration `mapstructure:"retry-delay"`
	MaxRequestRetries int           `mapstructure:"retry-max"`
}

func DefaultPoetConfig() PoetConfig {
	return PoetConfig{
		RequestRetryDelay: 400 * time.Millisecond,
		MaxRequestRetries: 10,
	}
}

const defaultPoetRetryInterval = 5 * time.Second

// Config defines configuration for Builder.
type Config struct {
	CoinbaseAccount types.Address
	GoldenATXID     types.ATXID
	LayersPerEpoch  uint32
}

// Builder struct is the struct that orchestrates the creation of activation transactions
// it is responsible for initializing post, receiving poet proof and orchestrating nipst. after which it will
// calculate total weight and providing relevant view as proof.
type Builder struct {
	pendingPoetClients atomic.Pointer[[]PoetProvingServiceClient]
	started            *atomic.Bool

	eg errgroup.Group

	signer            *signing.EdSigner
	accountLock       sync.RWMutex
	nodeID            types.NodeID
	coinbaseAccount   types.Address
	goldenATXID       types.ATXID
	layersPerEpoch    uint32
	cdb               *datastore.CachedDB
	atxHandler        atxHandler
	publisher         pubsub.Publisher
	nipostBuilder     nipostBuilder
	postSetupProvider postSetupProvider
	initialPost       *types.Post
	initialPostMeta   *types.PostMetadata

	// smeshingMutex protects `StartSmeshing` and `StopSmeshing` from concurrent access
	smeshingMutex sync.Mutex

	// pendingATX is created with current commitment and nipost from current challenge.
	pendingATX            *types.ActivationTx
	layerClock            layerClock
	syncer                syncer
	log                   log.Logger
	parentCtx             context.Context
	stop                  context.CancelFunc
	poetCfg               PoetConfig
	poetRetryInterval     time.Duration
	poetClientInitializer PoETClientInitializer
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
type PoETClientInitializer func(string, PoetConfig) (PoetProvingServiceClient, error)

// WithPoETClientInitializer modifies initialization logic for PoET client. Used during client update.
func WithPoETClientInitializer(initializer PoETClientInitializer) BuilderOption {
	return func(b *Builder) {
		b.poetClientInitializer = initializer
	}
}

// WithContext modifies parent context for background job.
func WithContext(ctx context.Context) BuilderOption {
	return func(b *Builder) {
		b.parentCtx = ctx
	}
}

// WithPoetConfig sets the poet config.
func WithPoetConfig(c PoetConfig) BuilderOption {
	return func(b *Builder) {
		b.poetCfg = c
	}
}

// NewBuilder returns an atx builder that will start a routine that will attempt to create an atx upon each new layer.
func NewBuilder(
	conf Config,
	nodeID types.NodeID,
	signer *signing.EdSigner,
	cdb *datastore.CachedDB,
	hdlr atxHandler,
	publisher pubsub.Publisher,
	nipostBuilder nipostBuilder,
	postSetupProvider postSetupProvider,
	layerClock layerClock,
	syncer syncer,
	log log.Log,
	opts ...BuilderOption,
) *Builder {
	b := &Builder{
		parentCtx:             context.Background(),
		signer:                signer,
		nodeID:                nodeID,
		coinbaseAccount:       conf.CoinbaseAccount,
		goldenATXID:           conf.GoldenATXID,
		layersPerEpoch:        conf.LayersPerEpoch,
		cdb:                   cdb,
		atxHandler:            hdlr,
		publisher:             publisher,
		nipostBuilder:         nipostBuilder,
		postSetupProvider:     postSetupProvider,
		layerClock:            layerClock,
		syncer:                syncer,
		started:               atomic.NewBool(false),
		log:                   log,
		poetRetryInterval:     defaultPoetRetryInterval,
		poetClientInitializer: defaultPoetClientFunc,
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Smeshing returns true iff atx builder is smeshing.
func (b *Builder) Smeshing() bool {
	return b.started.Load()
}

// StartSmeshing is the main entry point of the atx builder. It runs the main
// loop of the builder in a new go-routine and shouldn't be called more than
// once without calling StopSmeshing in between. If the post data is incomplete
// or missing, data creation session will be preceded. Changing of the post
// options (e.g., number of labels), after initial setup, is supported. If data
// creation fails for any reason then the go-routine will panic.
func (b *Builder) StartSmeshing(coinbase types.Address, opts PostSetupOpts) error {
	b.smeshingMutex.Lock()
	defer b.smeshingMutex.Unlock()

	if !b.started.CompareAndSwap(false, true) {
		return errors.New("already started")
	}

	b.coinbaseAccount = coinbase
	ctx, stop := context.WithCancel(b.parentCtx)
	b.stop = stop

	err := b.postSetupProvider.PrepareInitializer(b.parentCtx, opts)
	if err != nil {
		return fmt.Errorf("failed to prepare post initializer: %w", err)
	}

	b.eg.Go(func() error {
		defer b.started.Store(false)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-b.syncer.RegisterForATXSynced():
			// ensure we are ATX synced before starting the PoST Session
		}

		// If start session returns any error other than context.Canceled
		// (which is how we signal it to stop) then we panic.
		if err := b.postSetupProvider.StartSession(ctx); err != nil && !errors.Is(err, context.Canceled) {
			b.log.Panic("initialization failed: %v", err)
		}

		b.run(ctx)
		return nil
	})

	return nil
}

// StopSmeshing stops the atx builder.
// It doesn't wait for the smeshing to stop.
func (b *Builder) StopSmeshing(deleteFiles bool) error {
	b.smeshingMutex.Lock()
	defer b.smeshingMutex.Unlock()

	if !b.started.Load() {
		return errors.New("not started")
	}

	b.stop()
	err := b.eg.Wait()
	switch {
	case err == nil || errors.Is(err, context.Canceled):
		if !deleteFiles {
			return nil
		}

		if err := b.postSetupProvider.Reset(); err != nil {
			b.log.With().Error("failed to delete post files", log.Err(err))
			return err
		}
		return nil
	default:
		return fmt.Errorf("failed to stop post data creation session: %w", err)
	}
}

// SmesherID returns the ID of the smesher that created this activation.
func (b *Builder) SmesherID() types.NodeID {
	return b.nodeID
}

func (b *Builder) run(ctx context.Context) {
	if err := b.generateInitialPost(ctx); err != nil {
		b.log.Error("Failed to generate proof: %s", err)
		return
	}

	select {
	case <-ctx.Done():
		return
	case <-b.layerClock.AwaitLayer(types.LayerID(0)):
	}
	b.loop(ctx)
}

func (b *Builder) generateInitialPost(ctx context.Context) error {
	// Generate the initial POST if we don't have an ATX...
	if _, err := b.cdb.GetLastAtx(b.nodeID); err == nil {
		return nil
	}
	// ...and if we don't have an initial POST persisted already.
	if _, err := loadPost(b.nipostBuilder.DataDir()); err == nil {
		return nil
	}

	// Create the initial post and save it.
	startTime := time.Now()
	var err error
	b.initialPost, b.initialPostMeta, err = b.postSetupProvider.GenerateProof(ctx, shared.ZeroChallenge)
	if err != nil {
		return fmt.Errorf("post execution: %w", err)
	}
	metrics.PostDuration.Set(float64(time.Since(startTime).Nanoseconds()))

	if err := savePost(b.nipostBuilder.DataDir(), b.initialPost); err != nil {
		b.log.With().Warning("failed to save initial post: %w", log.Err(err))
	}
	return nil
}

func (b *Builder) receivePendingPoetClients() *[]PoetProvingServiceClient {
	return b.pendingPoetClients.Swap(nil)
}

// loop is the main loop that tries to create an atx per tick received from the global clock.
func (b *Builder) loop(ctx context.Context) {
	defer b.log.Info("atx builder stopped")

	for {
		if poetClients := b.receivePendingPoetClients(); poetClients != nil {
			b.nipostBuilder.UpdatePoETProvers(*poetClients)
		}

		ctx := log.WithNewSessionID(ctx)
		err := b.PublishActivationTx(ctx)
		if err == nil {
			continue
		} else if errors.Is(err, context.Canceled) {
			return
		}

		b.log.WithContext(ctx).With().Warning("failed to publish atx",
			b.layerClock.CurrentLayer(),
			b.currentEpoch(),
			log.Err(err),
		)

		switch {
		case errors.Is(err, ErrATXChallengeExpired):
			b.log.WithContext(ctx).Debug("retrying with new challenge after waiting for a layer")
			if err = b.discardChallenge(); err != nil {
				b.log.WithContext(ctx).Error("failed to discard challenge", log.Err(err))
			}
			// give node some time to sync in case selecting the positioning ATX caused the challenge to expire
			currentLayer := b.layerClock.CurrentLayer()
			select {
			case <-ctx.Done():
				return
			case <-b.layerClock.AwaitLayer(currentLayer.Add(1)):
			}
		case errors.Is(err, ErrPoetServiceUnstable):
			b.log.WithContext(ctx).With().Warning("retrying after poet retry interval", log.Duration("interval", b.poetRetryInterval))
			select {
			case <-ctx.Done():
				return
			case <-time.After(b.poetRetryInterval):
			}
		default:
			b.log.WithContext(ctx).With().Warning("unknown error", log.Err(err))
			// other failures are related to in-process software. we may as well panic here
			currentLayer := b.layerClock.CurrentLayer()
			select {
			case <-ctx.Done():
				return
			case <-b.layerClock.AwaitLayer(currentLayer.Add(1)):
			}
		}
	}
}

func (b *Builder) buildNIPostChallenge(ctx context.Context) (*types.NIPostChallenge, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-b.syncer.RegisterForATXSynced():
	}
	current := b.currentEpoch()
	prev, err := b.cdb.GetLastAtx(b.nodeID)
	if err != nil {
		if !errors.Is(err, sql.ErrNotFound) {
			return nil, err
		}
	} else if prev.PublishEpoch == current+1 {
		current += 1
	}

	until := time.Until(b.poetRoundStart(current))
	if until <= 0 {
		metrics.PublishLateWindowLatency.Observe(-until.Seconds())
		return nil, fmt.Errorf("%w: builder doesn't have time to submit in epoch %d. poet round already started %v ago",
			ErrATXChallengeExpired, current, -until)
	}
	metrics.PublishOntimeWindowLatency.Observe(until.Seconds())
	if until > b.poetCfg.GracePeriod {
		wait := until - b.poetCfg.GracePeriod
		b.log.WithContext(ctx).With().Debug("waiting for fresh atxs",
			log.Duration("till poet round", until),
			log.Uint32("current epoch", current.Uint32()),
			log.Duration("wait", wait),
		)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}

	posAtx, err := b.GetPositioningAtx()
	if err != nil {
		return nil, fmt.Errorf("failed to get positioning ATX: %w", err)
	}

	challenge := &types.NIPostChallenge{
		PublishEpoch:   current + 1,
		PositioningATX: posAtx,
	}

	if prevAtx, err := b.cdb.GetLastAtx(b.nodeID); err != nil {
		commitmentAtx, err := b.postSetupProvider.CommitmentAtx()
		if err != nil {
			return nil, fmt.Errorf("failed to get commitment ATX: %w", err)
		}
		challenge.CommitmentATX = &commitmentAtx
		challenge.InitialPostIndices = b.initialPost.Indices
	} else {
		challenge.PrevATXID = prevAtx.ID
		challenge.Sequence = prevAtx.Sequence + 1
	}

	if err = saveNipostChallenge(b.nipostBuilder.DataDir(), challenge); err != nil {
		return nil, err
	}
	return challenge, nil
}

// UpdatePoETServers updates poet client. Context is used to verify that the target is responsive.
func (b *Builder) UpdatePoETServers(ctx context.Context, endpoints []string) error {
	b.log.WithContext(ctx).With().Debug("request to update poet services",
		log.Array("endpoints", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
			for _, endpoint := range endpoints {
				encoder.AppendString(endpoint)
			}
			return nil
		})))

	clients := make([]PoetProvingServiceClient, 0, len(endpoints))
	for _, endpoint := range endpoints {
		client, err := b.poetClientInitializer(endpoint, b.poetCfg)
		if err != nil {
			return &PoetSvcUnstableError{source: fmt.Errorf("initial poet client '%s': %w", endpoint, err)}
		}
		// TODO(dshulyak) not enough information to verify that PoetServiceID matches with an expected one.
		// Maybe it should be provided during update.
		ctx, cancel := context.WithTimeout(ctx, time.Second*10)
		defer cancel()
		sid, err := client.PoetServiceID(ctx)
		if err != nil {
			return &PoetSvcUnstableError{source: fmt.Errorf("failed to query poet '%s' for ID: %w", endpoint, err)}
		}
		b.log.WithContext(ctx).With().Debug("preparing to update poet service", log.String("poet_id", hex.EncodeToString(sid.ServiceID)))
		clients = append(clients, client)
	}

	b.pendingPoetClients.Store(&clients)
	return nil
}

// SetCoinbase sets the address rewardAddress to be the coinbase account written into the activation transaction
// the rewards for blocks made by this miner will go to this address.
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

func (b *Builder) loadChallenge() (*types.NIPostChallenge, error) {
	nipost, err := loadNipostChallenge(b.nipostBuilder.DataDir())
	if err != nil {
		return nil, err
	}
	if nipost.TargetEpoch() < b.currentEpoch() {
		b.log.With().Info("atx nipost challenge is stale - discarding it",
			log.Stringer("target_epoch", nipost.TargetEpoch()),
			log.Stringer("publish_epoch", nipost.PublishEpoch),
			log.Stringer("current_epoch", b.currentEpoch()),
		)
		if err = b.discardChallenge(); err != nil {
			return nil, fmt.Errorf("%w: atx nipost challenge is stale", err)
		}
		return nil, errors.New("atx nipost challenge is stale")
	}
	return nipost, nil
}

// PublishActivationTx attempts to publish an atx, it returns an error if an atx cannot be created.
func (b *Builder) PublishActivationTx(ctx context.Context) error {
	logger := b.log.WithContext(ctx)

	challenge, err := b.loadChallenge()
	if err != nil {
		logger.With().Info("building new atx challenge",
			log.Stringer("current_epoch", b.currentEpoch()),
			log.Err(err),
		)
		challenge, err = b.buildNIPostChallenge(ctx)
		if err != nil {
			return err
		}
	}

	logger.With().Info("atx challenge is ready",
		log.Stringer("current_epoch", b.currentEpoch()),
		log.Stringer("publish_epoch", challenge.PublishEpoch),
		log.Stringer("target_epoch", challenge.TargetEpoch()),
	)

	if b.pendingATX == nil {
		var err error
		b.pendingATX, err = b.createAtx(ctx, challenge)
		if err != nil {
			return fmt.Errorf("create ATX: %w", err)
		}
	}

	atx := b.pendingATX
	atxReceived := b.atxHandler.AwaitAtx(atx.ID())
	defer b.atxHandler.UnsubscribeAtx(atx.ID())
	size, err := b.broadcast(ctx, atx)
	if err != nil {
		return fmt.Errorf("broadcast: %w", err)
	}

	logger.Event().Info("atx published", log.Inline(atx), log.Int("size", size))

	select {
	case <-atxReceived:
		logger.With().Info("received atx in db", atx.ID())
	case <-b.layerClock.AwaitLayer((atx.TargetEpoch() + 1).FirstLayer()):
		if err = b.discardChallenge(); err != nil {
			return fmt.Errorf("%w: target epoch has passed", err)
		}
		return fmt.Errorf("%w: target epoch has passed", ErrATXChallengeExpired)
	case <-ctx.Done():
		return ctx.Err()
	}
	if err = b.discardChallenge(); err != nil {
		return fmt.Errorf("%w: after published atx", err)
	}
	return nil
}

func (b *Builder) poetRoundStart(epoch types.EpochID) time.Time {
	return b.layerClock.LayerToTime(epoch.FirstLayer()).Add(b.poetCfg.PhaseShift)
}

func (b *Builder) createAtx(ctx context.Context, challenge *types.NIPostChallenge) (*types.ActivationTx, error) {
	pubEpoch := challenge.PublishEpoch
	nextPoetRoundStart := b.poetRoundStart(pubEpoch)

	// NiPoST must be ready before start of the next poet round.
	buildingNipostCtx, cancel := context.WithDeadline(ctx, nextPoetRoundStart)
	defer cancel()
	nipost, postDuration, err := b.nipostBuilder.BuildNIPost(buildingNipostCtx, challenge)
	if err != nil {
		return nil, fmt.Errorf("build NIPost: %w", err)
	}
	metrics.PostDuration.Set(float64(postDuration.Nanoseconds()))

	b.log.With().Info("awaiting atx publication epoch",
		log.Stringer("pub_epoch", pubEpoch),
		log.Stringer("pub_epoch_first_layer", pubEpoch.FirstLayer()),
		log.Stringer("current_layer", b.layerClock.CurrentLayer()),
	)
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for publication epoch: %w", ctx.Err())
	case <-b.layerClock.AwaitLayer(pubEpoch.FirstLayer()):
	}
	b.log.Debug("publication epoch has arrived!")

	if challenge.TargetEpoch() < b.currentEpoch() {
		if err = b.discardChallenge(); err != nil {
			return nil, fmt.Errorf("%w: atx publish epoch has passed during nipost construction", err)
		}
		return nil, fmt.Errorf("%w: atx publish epoch has passed during nipost construction", ErrATXChallengeExpired)
	}

	// when we reach here an epoch has passed
	// we've completed the sequential work, now before publishing the atx,
	// we need to provide number of atx seen in the epoch of the positioning atx.

	// ensure we are synced before generating the ATX's view
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-b.syncer.RegisterForATXSynced():
	}

	var initialPost *types.Post
	var nonce *types.VRFPostIndex
	var nodeID *types.NodeID
	if challenge.PrevATXID == types.EmptyATXID {
		nodeID = &b.nodeID
		initialPost = b.initialPost
		nonce, err = b.postSetupProvider.VRFNonce()
		if err != nil {
			return nil, fmt.Errorf("build atx: %w", err)
		}
	}

	atx := types.NewActivationTx(
		*challenge,
		b.Coinbase(),
		nipost,
		b.postSetupProvider.LastOpts().NumUnits,
		initialPost,
		nonce,
	)
	atx.InnerActivationTx.NodeID = nodeID
	if err = SignAndFinalizeAtx(b.signer, atx); err != nil {
		return nil, fmt.Errorf("sign atx: %w", err)
	}
	return atx, nil
}

func (b *Builder) currentEpoch() types.EpochID {
	return b.layerClock.CurrentLayer().GetEpoch()
}

func (b *Builder) discardChallenge() error {
	b.pendingATX = nil
	if err := discardNipostChallenge(b.nipostBuilder.DataDir()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (b *Builder) broadcast(ctx context.Context, atx *types.ActivationTx) (int, error) {
	buf, err := codec.Encode(atx)
	if err != nil {
		return 0, fmt.Errorf("failed to serialize ATX: %w", err)
	}
	if err := b.publisher.Publish(ctx, pubsub.AtxProtocol, buf); err != nil {
		return 0, fmt.Errorf("failed to broadcast ATX: %w", err)
	}
	return len(buf), nil
}

// GetPositioningAtx returns atx id from the newest epoch with the highest tick height.
func (b *Builder) GetPositioningAtx() (types.ATXID, error) {
	id, err := b.atxHandler.GetPosAtxID()
	if err != nil {
		if errors.Is(err, sql.ErrNotFound) {
			b.log.With().Info("using golden atx as positioning atx", b.goldenATXID)
			return b.goldenATXID, nil
		}
		return types.ATXID{}, fmt.Errorf("cannot find pos atx: %w", err)
	}
	return id, nil
}

// SignAndFinalizeAtx signs the atx with specified signer and calculates the ID of the ATX.
func SignAndFinalizeAtx(signer *signing.EdSigner, atx *types.ActivationTx) error {
	atx.Signature = signer.Sign(signing.ATX, atx.SignedBytes())
	atx.SmesherID = signer.NodeID()
	return atx.Initialize()
}
