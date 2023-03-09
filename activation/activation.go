// Package activation is responsible for creating activation transactions and running the mining flow, coordinating
// Post building, sending proofs to PoET and building NIPost structs.
package activation

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
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
	"github.com/spacemeshos/go-spacemesh/sql/kvstore"
)

// PoetConfig is the configuration to interact with the poet server.
type PoetConfig struct {
	PhaseShift  time.Duration `mapstructure:"phase-shift"`
	CycleGap    time.Duration `mapstructure:"cycle-gap"`
	GracePeriod time.Duration `mapstructure:"grace-period"`
}

func DefaultPoetConfig() PoetConfig {
	return PoetConfig{}
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

	// commitmentAtx caches the ATX ID used for the PoST commitment by this node. It is set / fetched
	// from the DB by calling `getCommitmentAtx()` and cAtxMutex protects its access.
	commitmentAtx *types.ATXID

	// smeshingMutex protects `StartSmeshing` and `StopSmeshing` from concurrent access
	smeshingMutex sync.Mutex

	// pendingATX is created with current commitment and nipst from current challenge.
	pendingATX            *types.ActivationTx
	layerClock            layerClock
	syncer                syncer
	log                   log.Log
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

// StartSmeshing is the main entry point of the atx builder.
// It runs the main loop of the builder and shouldn't be called more than once.
// If the post data is incomplete or missing, data creation
// session will be preceded. Changing of the post potions (e.g., number of labels),
// after initial setup, is supported.
func (b *Builder) StartSmeshing(coinbase types.Address, opts PostSetupOpts) error {
	b.smeshingMutex.Lock()
	defer b.smeshingMutex.Unlock()

	if !b.started.CompareAndSwap(false, true) {
		return errors.New("already started")
	}

	b.coinbaseAccount = coinbase
	ctx, stop := context.WithCancel(b.parentCtx)
	b.stop = stop

	b.eg.Go(func() error {
		defer b.started.Store(false)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-b.syncer.RegisterForATXSynced():
			// ensure we are ATX synced before starting the PoST Session
		}

		if err := b.postSetupProvider.StartSession(ctx, opts); err != nil {
			return err
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
	if err := b.generateProof(ctx); err != nil {
		b.log.Error("Failed to generate proof: %w", err)
		return
	}

	select {
	case <-ctx.Done():
		return
	case <-b.layerClock.AwaitLayer(types.NewLayerID(0)):
	}

	b.waitForFirstATX(ctx)
	b.loop(ctx)
}

func (b *Builder) generateProof(ctx context.Context) error {
	// don't generate the commitment every time smeshing is starting, but once only.
	if _, err := b.cdb.GetLastAtx(b.nodeID); err != nil {
		// Once initialized, run the execution phase with zero-challenge,
		// to create the initial proof (the commitment).
		startTime := time.Now()
		b.initialPost, b.initialPostMeta, err = b.postSetupProvider.GenerateProof(ctx, shared.ZeroChallenge)
		if err != nil {
			return fmt.Errorf("post execution: %w", err)
		}
		metrics.PostDuration.Set(float64(time.Since(startTime).Nanoseconds()))
	}

	return nil
}

// waitForFirstATX waits until the first ATX can be published. The return value indicates
// if the function waited or not (for testing).
func (b *Builder) waitForFirstATX(ctx context.Context) bool {
	currentLayer := b.layerClock.CurrentLayer()
	currEpoch := currentLayer.GetEpoch()
	if currEpoch == 0 { // genesis miner
		return false
	}
	if prev, err := b.cdb.GetLastAtx(b.nodeID); err == nil {
		if prev.PublishEpoch() == currEpoch {
			// miner has published in the current epoch
			return false
		}
	}

	// miner didn't publish ATX that targets current epoch.
	// This estimate work if the majority of the nodes use poet servers that are configured the same way.
	// TODO: do better when nodes use poet services with different phase shifts.
	// We must wait till an ATX from other nodes arrives.
	// To calculate the needed time we find the Poet round end
	// and add twice network grace plus average PoST duration time.
	// The first grace time is for poet proof propagation, The second one - for the ATX.
	// Max wait time depicts an estimated safe moment to still be able to
	// submit a poet challenge with the obtained ATX.
	//
	//                 Grace ──────┐
	//                 Period      │  ┌─ATX arrives
	//                     │  PoST │  │   ┌ wait deadline
	//                     │ ┌───► │  │   │   ┌ Next round start
	//   ┌────────────────┬┴─►   └─┴─►│   │   ▼───────
	//   │  POET ROUND N  │           │   │   │ ROUND N+1
	// ──┴────────────┬───┴───────────▼───▼───┴───────►time
	//     EPOCH N    │      EPOCH N+1
	// ───────────────┴───────────────────────────────
	averagePostTime := time.Second // TODO should probably come up with a reasonable average value.
	poetRoundEndOffset := b.poetCfg.PhaseShift - b.poetCfg.CycleGap
	atxArrivalOffset := poetRoundEndOffset + 2*b.poetCfg.GracePeriod + averagePostTime
	waitDeadline := b.layerClock.LayerToTime(currEpoch.FirstLayer()).Add(b.poetCfg.PhaseShift).Add(-b.poetCfg.GracePeriod)

	var expectedAtxArrivalTime time.Time
	if time.Now().After(waitDeadline) {
		b.log.WithContext(ctx).With().Info("missed the window to submit a poet challenge. Will wait for next epoch.")
		expectedAtxArrivalTime = b.layerClock.LayerToTime((currEpoch + 1).FirstLayer()).Add(atxArrivalOffset)
	} else {
		expectedAtxArrivalTime = b.layerClock.LayerToTime(currEpoch.FirstLayer()).Add(atxArrivalOffset)
	}

	waitTime := time.Until(expectedAtxArrivalTime)
	timer := time.NewTimer(waitTime)
	defer timer.Stop()

	b.log.WithContext(ctx).With().Info("waiting for the first ATX", log.Duration("wait", waitTime))
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
	}
	b.log.WithContext(ctx).With().Info("ready to build first atx",
		log.Stringer("current_layer", b.layerClock.CurrentLayer()),
		log.Stringer("current_epoch", b.layerClock.CurrentLayer().GetEpoch()))
	return true
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
			b.log.WithContext(ctx).With().Info("waiting for atx to propagate before building the next challenge",
				log.Duration("wait", b.poetRetryInterval),
			)
			select {
			case <-ctx.Done():
				return
			case <-time.After(b.poetRetryInterval):
			}
			continue
		}

		if errors.Is(err, context.Canceled) {
			return
		}

		b.log.WithContext(ctx).With().Error("error attempting to publish atx",
			b.layerClock.CurrentLayer(),
			b.currentEpoch(),
			log.Err(err),
		)

		switch {
		case errors.Is(err, ErrATXChallengeExpired):
			b.log.WithContext(ctx).Debug("retrying with new challenge after waiting for a layer")
			b.discardChallenge()
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

	atxID, pubLayerID, err := b.GetPositioningAtxInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to get positioning ATX: %w", err)
	}
	challenge := &types.NIPostChallenge{
		PositioningATX: atxID,
		PubLayerID:     pubLayerID.Add(b.layersPerEpoch),
	}
	if challenge.TargetEpoch() < b.currentEpoch() {
		b.discardChallenge()
		return nil, fmt.Errorf("%w: selected outdated positioning ATX", ErrATXChallengeExpired)
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

	if err := kvstore.AddNIPostChallenge(b.cdb, challenge); err != nil {
		return nil, fmt.Errorf("failed to store nipost challenge: %w", err)
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
		b.log.WithContext(ctx).With().Debug("preparing to update poet service", log.String("poet_id", hex.EncodeToString(sid)))
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
	nipost, err := kvstore.GetNIPostChallenge(b.cdb)
	if err != nil {
		return nil, fmt.Errorf("failed to load nipost challenge from DB: %w", err)
	}
	if nipost.TargetEpoch() < b.currentEpoch() {
		b.log.With().Info("atx nipost challenge is stale - discarding it",
			log.FieldNamed("target_epoch", nipost.TargetEpoch()),
			log.FieldNamed("publish_epoch", nipost.PublishEpoch()),
			log.FieldNamed("current_epoch", b.currentEpoch()),
		)
		b.discardChallenge()
		return nil, errors.New("atx nipost challenge is stale")
	}
	return nipost, nil
}

// PublishActivationTx attempts to publish an atx, it returns an error if an atx cannot be created.
func (b *Builder) PublishActivationTx(ctx context.Context) error {
	logger := b.log.WithContext(ctx)

	challenge, err := b.loadChallenge()
	if err != nil {
		logger.With().Info("challenge not loaded", log.Err(err))
		logger.With().Info("building new atx challenge", log.Stringer("current_epoch", b.currentEpoch()))
		challenge, err = b.buildNIPostChallenge(ctx)
		if err != nil {
			return fmt.Errorf("build new atx challenge: %w", err)
		}
	}

	logger.With().Info("atx challenge is ready",
		log.FieldNamed("current_epoch", b.currentEpoch()),
		log.FieldNamed("publish_epoch", challenge.PublishEpoch()),
		log.FieldNamed("target_epoch", challenge.TargetEpoch()),
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
		b.discardChallenge()
		return fmt.Errorf("%w: target epoch has passed", ErrATXChallengeExpired)
	case <-ctx.Done():
		return ctx.Err()
	}
	b.discardChallenge()
	return nil
}

func (b *Builder) createAtx(ctx context.Context, challenge *types.NIPostChallenge) (*types.ActivationTx, error) {
	pubEpoch := challenge.PublishEpoch()
	poetRoundStart := b.layerClock.LayerToTime((pubEpoch - 1).FirstLayer()).Add(b.poetCfg.PhaseShift)
	nextPoetRoundStart := b.layerClock.LayerToTime(pubEpoch.FirstLayer()).Add(b.poetCfg.PhaseShift)

	now := time.Now()
	if poetRoundStart.Before(now) {
		b.discardChallenge()
		return nil, fmt.Errorf("%w: poet round has already started at %s (now: %s)", ErrATXChallengeExpired, poetRoundStart, now)
	}

	poetChallenge := types.PoetChallenge{
		NIPostChallenge: challenge,
		NumUnits:        b.postSetupProvider.LastOpts().NumUnits,
	}
	if challenge.PrevATXID == *types.EmptyATXID {
		poetChallenge.InitialPost = b.initialPost
		poetChallenge.InitialPostMetadata = b.initialPostMeta
	}

	// NiPoST must be ready before start of the next poet round.
	buildingNipostCtx, cancel := context.WithDeadline(ctx, nextPoetRoundStart)
	defer cancel()
	nipost, postDuration, err := b.nipostBuilder.BuildNIPost(buildingNipostCtx, &poetChallenge)
	if err != nil {
		return nil, fmt.Errorf("build NIPost: %w", err)
	}
	metrics.PostDuration.Set(float64(postDuration.Nanoseconds()))

	b.log.With().Info("awaiting atx publication epoch",
		log.FieldNamed("pub_epoch", pubEpoch),
		log.FieldNamed("pub_epoch_first_layer", pubEpoch.FirstLayer()),
		log.FieldNamed("current_layer", b.layerClock.CurrentLayer()),
	)
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for publication epoch: %w", err)
	case <-b.layerClock.AwaitLayer(pubEpoch.FirstLayer()):
	}
	b.log.Info("publication epoch has arrived!")

	if challenge.TargetEpoch() < b.currentEpoch() {
		b.discardChallenge()
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
	if challenge.PrevATXID == *types.EmptyATXID {
		initialPost = b.initialPost
		nonce, err = b.postSetupProvider.VRFNonce()
		if err != nil {
			return nil, fmt.Errorf("build atx: %w", err)
		}
	}

	atx := types.NewActivationTx(
		*challenge,
		&b.nodeID,
		b.Coinbase(),
		nipost,
		b.postSetupProvider.LastOpts().NumUnits,
		initialPost,
		nonce,
	)
	if err = SignAndFinalizeAtx(b.signer, atx); err != nil {
		return nil, fmt.Errorf("sign atx: %w", err)
	}
	return atx, nil
}

func (b *Builder) currentEpoch() types.EpochID {
	return b.layerClock.CurrentLayer().GetEpoch()
}

func (b *Builder) discardChallenge() {
	b.pendingATX = nil
	if err := kvstore.ClearNIPostChallenge(b.cdb); err != nil {
		b.log.Error("failed to discard NIPost challenge: %w", err)
	}
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

// GetPositioningAtxInfo returns id and publication layer from the best observed atx.
func (b *Builder) GetPositioningAtxInfo() (types.ATXID, types.LayerID, error) {
	id, err := b.atxHandler.GetPosAtxID()
	if err != nil {
		if errors.Is(err, sql.ErrNotFound) {
			b.log.With().Info("using golden atx as positioning atx", b.goldenATXID)
			return b.goldenATXID, types.NewLayerID(0), nil
		}
		return types.ATXID{}, types.LayerID{}, fmt.Errorf("cannot find pos atx: %w", err)
	}
	atx, err := b.cdb.GetAtxHeader(id)
	if err != nil {
		return types.ATXID{}, types.LayerID{}, fmt.Errorf("inconsistent state: failed to get atx header: %w", err)
	}
	return id, atx.PubLayerID, nil
}

// SignAndFinalizeAtx signs the atx with specified signer and calculates the ID of the ATX.
func SignAndFinalizeAtx(signer *signing.EdSigner, atx *types.ActivationTx) error {
	atx.Signature = signer.Sign(signing.ATX, atx.SignedBytes())
	return atx.CalcAndSetID()
}
