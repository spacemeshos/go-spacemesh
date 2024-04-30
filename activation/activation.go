// Package activation is responsible for creating activation transactions and running the mining flow, coordinating
// Post building, sending proofs to PoET and building NIPost structs.
package activation

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spacemeshos/post/shared"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation/metrics"
	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/metrics/public"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
)

// PoetConfig is the configuration to interact with the poet server.
type PoetConfig struct {
	PhaseShift        time.Duration `mapstructure:"phase-shift"`
	CycleGap          time.Duration `mapstructure:"cycle-gap"`
	GracePeriod       time.Duration `mapstructure:"grace-period"`
	RequestTimeout    time.Duration `mapstructure:"poet-request-timeout"`
	RequestRetryDelay time.Duration `mapstructure:"retry-delay"`
	MaxRequestRetries int           `mapstructure:"retry-max"`
}

func DefaultPoetConfig() PoetConfig {
	return PoetConfig{
		RequestRetryDelay: 400 * time.Millisecond,
		MaxRequestRetries: 10,
	}
}

const (
	defaultPoetRetryInterval = 5 * time.Second

	// Jitter added to the wait time before building a nipost challenge.
	// It is expressed as % of poet grace period which translates to:
	//  mainnet (grace period 1h) -> 36s
	//  systest (grace period 10s) -> 0.1s
	maxNipostChallengeBuildJitter = 1.0
)

// Config defines configuration for Builder.
type Config struct {
	GoldenATXID      types.ATXID
	LabelsPerUnit    uint64
	RegossipInterval time.Duration
}

// Builder struct is the struct that orchestrates the creation of activation transactions
// it is responsible for initializing post, receiving poet proof and orchestrating nipst. after which it will
// calculate total weight and providing relevant view as proof.
type Builder struct {
	accountLock       sync.RWMutex
	coinbaseAccount   types.Address
	conf              Config
	db                sql.Executor
	localDB           *localsql.Database
	publisher         pubsub.Publisher
	nipostBuilder     nipostBuilder
	validator         nipostValidator
	layerClock        layerClock
	syncer            syncer
	log               *zap.Logger
	parentCtx         context.Context
	poetCfg           PoetConfig
	poetRetryInterval time.Duration
	// delay before PoST in ATX is considered valid (counting from the time it was received)
	postValidityDelay time.Duration

	posAtxFinder positioningAtxFinder

	// states of each known identity
	postStates PostStates

	// smeshingMutex protects methods like `StartSmeshing` and `StopSmeshing` from concurrent execution
	// since they (can) modify the fields below.
	smeshingMutex sync.Mutex
	signers       map[types.NodeID]*signing.EdSigner
	eg            errgroup.Group
	stop          context.CancelFunc
}

type positioningAtxFinder struct {
	finding sync.Mutex
	found   *struct {
		id         types.ATXID
		forPublish types.EpochID
	}
}

type BuilderOption func(*Builder)

func WithPostValidityDelay(delay time.Duration) BuilderOption {
	return func(b *Builder) {
		b.postValidityDelay = delay
	}
}

// WithPoetRetryInterval modifies time that builder will have to wait before retrying ATX build process
// if it failed due to issues with PoET server.
func WithPoetRetryInterval(interval time.Duration) BuilderOption {
	return func(b *Builder) {
		b.poetRetryInterval = interval
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

func WithValidator(v nipostValidator) BuilderOption {
	return func(b *Builder) {
		b.validator = v
	}
}

func WithPostStates(ps PostStates) BuilderOption {
	return func(b *Builder) {
		b.postStates = ps
	}
}

// NewBuilder returns an atx builder that will start a routine that will attempt to create an atx upon each new layer.
func NewBuilder(
	conf Config,
	db sql.Executor,
	localDB *localsql.Database,
	publisher pubsub.Publisher,
	nipostBuilder nipostBuilder,
	layerClock layerClock,
	syncer syncer,
	log *zap.Logger,
	opts ...BuilderOption,
) *Builder {
	b := &Builder{
		parentCtx:         context.Background(),
		signers:           make(map[types.NodeID]*signing.EdSigner),
		conf:              conf,
		db:                db,
		localDB:           localDB,
		publisher:         publisher,
		nipostBuilder:     nipostBuilder,
		layerClock:        layerClock,
		syncer:            syncer,
		log:               log,
		poetRetryInterval: defaultPoetRetryInterval,
		postValidityDelay: 12 * time.Hour,
		postStates:        NewPostStates(log),
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

func (b *Builder) Register(sig *signing.EdSigner) {
	b.smeshingMutex.Lock()
	defer b.smeshingMutex.Unlock()
	if _, exists := b.signers[sig.NodeID()]; exists {
		b.log.Error("signing key already registered", log.ZShortStringer("id", sig.NodeID()))
		return
	}

	b.log.Info("registered signing key", log.ZShortStringer("id", sig.NodeID()))
	b.signers[sig.NodeID()] = sig
	b.postStates.Set(sig.NodeID(), types.PostStateIdle)

	if b.stop != nil {
		b.startID(b.parentCtx, sig)
	}
}

// Smeshing returns true if atx builder is smeshing.
func (b *Builder) Smeshing() bool {
	b.smeshingMutex.Lock()
	defer b.smeshingMutex.Unlock()
	return b.stop != nil
}

// PostState returns the current state of the post service for each registered smesher.
func (b *Builder) PostStates() map[types.IdentityDescriptor]types.PostState {
	states := b.postStates.Get()
	res := make(map[types.IdentityDescriptor]types.PostState, len(states))
	b.smeshingMutex.Lock()
	defer b.smeshingMutex.Unlock()
	for id, state := range states {
		if sig, exists := b.signers[id]; exists {
			res[sig] = state
		}
	}
	return res
}

// StartSmeshing is the main entry point of the atx builder. It runs the main
// loop of the builder in a new go-routine and shouldn't be called more than
// once without calling StopSmeshing in between. If the post data is incomplete
// or missing, data creation session will be preceded. Changing of the post
// options (e.g., number of labels), after initial setup, is supported. If data
// creation fails for any reason then the go-routine will panic.
func (b *Builder) StartSmeshing(coinbase types.Address) error {
	b.smeshingMutex.Lock()
	defer b.smeshingMutex.Unlock()

	if b.stop != nil {
		return errors.New("already started")
	}

	b.coinbaseAccount = coinbase
	ctx, stop := context.WithCancel(b.parentCtx)
	b.stop = stop

	for _, sig := range b.signers {
		b.startID(ctx, sig)
	}
	return nil
}

func (b *Builder) startID(ctx context.Context, sig *signing.EdSigner) {
	b.eg.Go(func() error {
		b.run(ctx, sig)
		return nil
	})
	if b.conf.RegossipInterval == 0 {
		return
	}
	b.eg.Go(func() error {
		ticker := time.NewTicker(b.conf.RegossipInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				if err := b.Regossip(ctx, sig.NodeID()); err != nil {
					b.log.Warn("failed to re-gossip", zap.Error(err))
				}
			}
		}
	})
}

// StopSmeshing stops the atx builder.
func (b *Builder) StopSmeshing(deleteFiles bool) error {
	b.smeshingMutex.Lock()
	defer b.smeshingMutex.Unlock()

	if b.stop == nil {
		return errors.New("not started")
	}

	b.stop()
	err := b.eg.Wait()
	b.eg = errgroup.Group{}
	b.stop = nil
	switch {
	case err == nil || errors.Is(err, context.Canceled):
		if !deleteFiles {
			return nil
		}
		var resetErr error
		for _, sig := range b.signers {
			b.postStates.Set(sig.NodeID(), types.PostStateIdle)
			if err := b.nipostBuilder.ResetState(sig.NodeID()); err != nil {
				b.log.Error("failed to reset builder state", log.ZShortStringer("id", sig.NodeID()), zap.Error(err))
				err = fmt.Errorf("reset builder state for id %s: %w", sig.NodeID().ShortString(), err)
				resetErr = errors.Join(resetErr, err)
				continue
			}
			if err := nipost.RemoveChallenge(b.localDB, sig.NodeID()); err != nil {
				b.log.Error("failed to remove nipost challenge", zap.Error(err))
				err = fmt.Errorf("remove nipost challenge for id %s: %w", sig.NodeID().ShortString(), err)
				resetErr = errors.Join(resetErr, err)
			}
		}
		return resetErr
	default:
		return fmt.Errorf("failed to stop smeshing: %w", err)
	}
}

// SmesherID returns the ID of the smesher that created this activation.
func (b *Builder) SmesherIDs() []types.NodeID {
	b.smeshingMutex.Lock()
	defer b.smeshingMutex.Unlock()
	return maps.Keys(b.signers)
}

func (b *Builder) buildInitialPost(ctx context.Context, nodeID types.NodeID) error {
	// Generate the initial POST if we don't have an ATX...
	if _, err := atxs.GetLastIDByNodeID(b.db, nodeID); err == nil {
		return nil
	}
	// ...and if we haven't stored an initial post yet.
	_, err := nipost.InitialPost(b.localDB, nodeID)
	switch {
	case err == nil:
		b.log.Info("load initial post from db")
		return nil
	case errors.Is(err, sql.ErrNotFound):
		b.log.Info("creating initial post")
	default:
		return fmt.Errorf("get initial post: %w", err)
	}

	// Create the initial post and save it.
	startTime := time.Now()
	post, postInfo, err := b.nipostBuilder.Proof(ctx, nodeID, shared.ZeroChallenge)
	if err != nil {
		return fmt.Errorf("post execution: %w", err)
	}
	if postInfo.Nonce == nil {
		b.log.Error("initial PoST is invalid: missing VRF nonce. Check your PoST data",
			log.ZShortStringer("smesherID", nodeID),
		)
		return errors.New("nil VRF nonce")
	}
	initialPost := nipost.Post{
		Nonce:   post.Nonce,
		Indices: post.Indices,
		Pow:     post.Pow,

		NumUnits:      postInfo.NumUnits,
		CommitmentATX: postInfo.CommitmentATX,
		VRFNonce:      *postInfo.Nonce,
	}
	err = b.validator.Post(ctx, nodeID, postInfo.CommitmentATX, post, &types.PostMetadata{
		Challenge:     shared.ZeroChallenge,
		LabelsPerUnit: postInfo.LabelsPerUnit,
	}, postInfo.NumUnits)
	if err != nil {
		b.log.Error("initial POST is invalid", log.ZShortStringer("smesherID", nodeID), zap.Error(err))
		if err := nipost.RemoveInitialPost(b.localDB, nodeID); err != nil {
			b.log.Fatal("failed to remove initial post", log.ZShortStringer("smesherID", nodeID), zap.Error(err))
		}
		return fmt.Errorf("initial POST is invalid: %w", err)
	}

	metrics.PostDuration.Set(float64(time.Since(startTime).Nanoseconds()))
	public.PostSeconds.Set(float64(time.Since(startTime)))
	b.log.Info("created the initial post")

	return nipost.AddInitialPost(b.localDB, nodeID, initialPost)
}

func (b *Builder) run(ctx context.Context, sig *signing.EdSigner) {
	defer b.log.Info("atx builder stopped")

	for {
		err := b.buildInitialPost(ctx, sig.NodeID())
		if err == nil {
			break
		}
		b.log.Error("failed to generate initial proof:", zap.Error(err))
		currentLayer := b.layerClock.CurrentLayer()
		select {
		case <-ctx.Done():
			return
		case <-b.layerClock.AwaitLayer(currentLayer.Add(1)):
		}
	}

	for {
		err := b.PublishActivationTx(ctx, sig)
		if err == nil {
			continue
		} else if errors.Is(err, context.Canceled) {
			return
		}

		b.log.Warn("failed to publish atx", zap.Error(err))

		switch {
		case errors.Is(err, ErrATXChallengeExpired):
			b.log.Debug("retrying with new challenge after waiting for a layer")
			if err := b.nipostBuilder.ResetState(sig.NodeID()); err != nil {
				b.log.Error("failed to reset nipost builder state", zap.Error(err))
			}
			if err := nipost.RemoveChallenge(b.localDB, sig.NodeID()); err != nil {
				b.log.Error("failed to discard challenge", zap.Error(err))
			}
			// give node some time to sync in case selecting the positioning ATX caused the challenge to expire
			currentLayer := b.layerClock.CurrentLayer()
			select {
			case <-ctx.Done():
				return
			case <-b.layerClock.AwaitLayer(currentLayer.Add(1)):
			}
		case errors.Is(err, ErrPoetServiceUnstable):
			b.log.Warn("retrying after poet retry interval", zap.Duration("interval", b.poetRetryInterval))
			select {
			case <-ctx.Done():
				return
			case <-time.After(b.poetRetryInterval):
			}
		default:
			b.log.Warn("unknown error", zap.Error(err))
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

func (b *Builder) BuildNIPostChallenge(ctx context.Context, nodeID types.NodeID) (*types.NIPostChallenge, error) {
	logger := b.log.With(log.ZShortStringer("smesherID", nodeID))
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-b.syncer.RegisterForATXSynced():
	}
	current := b.layerClock.CurrentLayer().GetEpoch()

	challenge, err := nipost.Challenge(b.localDB, nodeID)
	switch {
	case errors.Is(err, sql.ErrNotFound):
		// build new challenge
		logger.Info("building new NiPOST challenge", zap.Uint32("current_epoch", current.Uint32()))
	case err != nil:
		logger.Info("failed to load NiPoST challenge from local state", zap.Error(err))
		return nil, fmt.Errorf("get nipost challenge: %w", err)
	case challenge.PublishEpoch < current:
		logger.Info(
			"existing NiPoST challenge is stale, resetting state",
			zap.Uint32("current_epoch", current.Uint32()),
			zap.Uint32("publish_epoch", challenge.PublishEpoch.Uint32()),
		)
		// Reset the state to idle because we won't be building POST until we get a new PoET proof
		// (typically more than epoch time from now).
		b.postStates.Set(nodeID, types.PostStateIdle)
		if err := b.nipostBuilder.ResetState(nodeID); err != nil {
			return nil, fmt.Errorf("reset nipost builder state: %w", err)
		}
		if err := nipost.RemoveChallenge(b.localDB, nodeID); err != nil {
			return nil, fmt.Errorf("remove stale nipost challenge: %w", err)
		}
	default:
		// challenge is fresh
		logger.Info("loaded NiPoST challenge from local state",
			zap.Uint32("current_epoch", current.Uint32()),
			zap.Uint32("publish_epoch", challenge.PublishEpoch.Uint32()),
		)
		return challenge, nil
	}

	prevAtx, err := b.GetPrevAtx(nodeID)
	switch {
	case err == nil:
		current = max(current, prevAtx.PublishEpoch)
	case errors.Is(err, sql.ErrNotFound):
		// no previous ATX
	case err != nil:
		return nil, fmt.Errorf("get last ATX: %w", err)
	}

	until := time.Until(b.poetRoundStart(current))
	if until <= 0 {
		metrics.PublishLateWindowLatency.Observe(-until.Seconds())
		current++
		until = time.Until(b.poetRoundStart(current))
	}
	publish := current + 1
	metrics.PublishOntimeWindowLatency.Observe(until.Seconds())
	wait := buildNipostChallengeStartDeadline(b.poetRoundStart(current), b.poetCfg.GracePeriod)
	if time.Until(wait) > 0 {
		logger.Info("paused building NiPoST challenge. Waiting until closer to poet start to get a better posATX",
			zap.Duration("till poet round", until),
			zap.Uint32("current epoch", current.Uint32()),
			zap.Time("waiting until", wait),
		)
		events.EmitPoetWaitRound(nodeID, current, publish, wait)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Until(wait)):
		}
	}

	posAtx, err := b.getPositioningAtx(ctx, nodeID, publish)
	if err != nil {
		return nil, fmt.Errorf("failed to get positioning ATX: %w", err)
	}
	logger.Info("selected positioning atx", log.ZShortStringer("atx_id", posAtx))

	prevAtx, err = b.GetPrevAtx(nodeID)
	switch {
	case errors.Is(err, sql.ErrNotFound):
		logger.Info("no previous ATX found, creating an initial nipost challenge")
		post, err := nipost.InitialPost(b.localDB, nodeID)
		if err != nil {
			return nil, fmt.Errorf("get initial post: %w", err)
		}
		logger.Info("verifying the initial post")
		initialPost := &types.Post{
			Nonce:   post.Nonce,
			Indices: post.Indices,
			Pow:     post.Pow,
		}
		err = b.validator.Post(ctx, nodeID, post.CommitmentATX, initialPost, &types.PostMetadata{
			Challenge:     shared.ZeroChallenge,
			LabelsPerUnit: b.conf.LabelsPerUnit,
		}, post.NumUnits)
		if err != nil {
			logger.Error("initial POST is invalid", zap.Error(err))
			if err := nipost.RemoveInitialPost(b.localDB, nodeID); err != nil {
				logger.Fatal("failed to remove initial post", zap.Error(err))
			}
			return nil, fmt.Errorf("initial POST is invalid: %w", err)
		}
		challenge = &types.NIPostChallenge{
			PublishEpoch:   publish,
			Sequence:       0,
			PrevATXID:      types.EmptyATXID,
			PositioningATX: posAtx,
			CommitmentATX:  &post.CommitmentATX,
			InitialPost: &types.Post{
				Nonce:   post.Nonce,
				Indices: post.Indices,
				Pow:     post.Pow,
			},
		}
	case err != nil:
		return nil, fmt.Errorf("get last ATX: %w", err)
	default:
		// regular ATX challenge
		challenge = &types.NIPostChallenge{
			PublishEpoch:   publish,
			Sequence:       prevAtx.Sequence + 1,
			PrevATXID:      prevAtx.ID(),
			PositioningATX: posAtx,
		}
	}
	logger.Info("persisting the new NiPOST challenge", zap.Object("challenge", challenge))
	if err := nipost.AddChallenge(b.localDB, nodeID, challenge); err != nil {
		return nil, fmt.Errorf("add nipost challenge: %w", err)
	}
	return challenge, nil
}

func (b *Builder) GetPrevAtx(nodeID types.NodeID) (*types.VerifiedActivationTx, error) {
	id, err := atxs.GetLastIDByNodeID(b.db, nodeID)
	if err != nil {
		return nil, fmt.Errorf("getting last ATXID: %w", err)
	}
	return atxs.Get(b.db, id)
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

// PublishActivationTx attempts to publish an atx, it returns an error if an atx cannot be created.
func (b *Builder) PublishActivationTx(ctx context.Context, sig *signing.EdSigner) error {
	challenge, err := b.BuildNIPostChallenge(ctx, sig.NodeID())
	if err != nil {
		return err
	}

	b.log.Info("atx challenge is ready",
		log.ZShortStringer("smesherID", sig.NodeID()),
		zap.Uint32("current_epoch", b.layerClock.CurrentLayer().GetEpoch().Uint32()),
		zap.Object("challenge", challenge),
	)
	targetEpoch := challenge.PublishEpoch.Add(1)
	ctx, cancel := context.WithDeadline(ctx, b.layerClock.LayerToTime(targetEpoch.FirstLayer()))
	defer cancel()
	atx, err := b.createAtx(ctx, sig, challenge)
	if err != nil {
		return fmt.Errorf("create ATX: %w", err)
	}

	for {
		size, err := b.broadcast(ctx, atx)
		if err == nil {
			b.log.Info("atx published", zap.Inline(atx), zap.Int("size", size))
			break
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("broadcast: %w", ctx.Err())
		default:
			// try again
		}
	}

	if err := b.nipostBuilder.ResetState(sig.NodeID()); err != nil {
		return fmt.Errorf("reset nipost builder state: %w", err)
	}
	if err := nipost.RemoveChallenge(b.localDB, sig.NodeID()); err != nil {
		return fmt.Errorf("discarding challenge after published ATX: %w", err)
	}
	events.EmitAtxPublished(
		sig.NodeID(),
		atx.PublishEpoch, atx.TargetEpoch(),
		atx.ID(),
		b.layerClock.LayerToTime(atx.TargetEpoch().FirstLayer()),
	)
	return nil
}

func (b *Builder) poetRoundStart(epoch types.EpochID) time.Time {
	return b.layerClock.LayerToTime(epoch.FirstLayer()).Add(b.poetCfg.PhaseShift)
}

func (b *Builder) createAtx(
	ctx context.Context,
	sig *signing.EdSigner,
	challenge *types.NIPostChallenge,
) (*types.ActivationTx, error) {
	pubEpoch := challenge.PublishEpoch
	// TODO: in future, encode the right NiPoST challenge version depending on the pubEpoch.
	challengeHash := wire.NIPostChallengeToWireV1(challenge).Hash()

	nipostState, err := b.nipostBuilder.BuildNIPost(ctx, sig, challenge.PublishEpoch, challengeHash)
	if err != nil {
		return nil, fmt.Errorf("build NIPost: %w", err)
	}

	b.log.Info("awaiting atx publication epoch",
		zap.Uint32("pub_epoch", pubEpoch.Uint32()),
		zap.Uint32("pub_epoch_first_layer", pubEpoch.FirstLayer().Uint32()),
		zap.Uint32("current_layer", b.layerClock.CurrentLayer().Uint32()),
		log.ZShortStringer("smesherID", sig.NodeID()),
	)
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for publication epoch: %w", ctx.Err())
	case <-b.layerClock.AwaitLayer(pubEpoch.FirstLayer()):
	}
	b.log.Debug("publication epoch has arrived!", log.ZShortStringer("smesherID", sig.NodeID()))

	if challenge.PublishEpoch < b.layerClock.CurrentLayer().GetEpoch() {
		if challenge.PrevATXID == types.EmptyATXID {
			// initial NIPoST challenge is not discarded; don't return ErrATXChallengeExpired
			return nil, errors.New("atx publish epoch has passed during nipost construction")
		}
		return nil, fmt.Errorf("%w: atx publish epoch has passed during nipost construction", ErrATXChallengeExpired)
	}

	var nonce *types.VRFPostIndex
	var atxNodeID *types.NodeID
	switch {
	case challenge.PrevATXID == types.EmptyATXID:
		atxNodeID = new(types.NodeID)
		*atxNodeID = sig.NodeID()
		nonce = &nipostState.VRFNonce
	default:
		oldNonce, err := atxs.NonceByID(b.db, challenge.PrevATXID)
		if err != nil {
			b.log.Warn("failed to get VRF nonce for ATX", zap.Error(err), log.ZShortStringer("smesherID", sig.NodeID()))
			break
		}
		if nipostState.VRFNonce != oldNonce {
			nonce = &nipostState.VRFNonce
		}
	}

	atx := types.NewActivationTx(
		*challenge,
		b.Coinbase(),
		nipostState.NIPost,
		nipostState.NumUnits,
		nonce,
	)
	atx.InnerActivationTx.NodeID = atxNodeID
	if err = SignAndFinalizeAtx(sig, atx); err != nil {
		return nil, fmt.Errorf("sign atx: %w", err)
	}
	return atx, nil
}

func (b *Builder) broadcast(ctx context.Context, atx *types.ActivationTx) (int, error) {
	b.log.Info("broadcasting", log.ZShortStringer("atx_id", atx.ID()), log.ZShortStringer("smesherID", atx.SmesherID))
	// TODO: in future, encode the right ATX version depending on the epoch.
	buf, err := codec.Encode(wire.ActivationTxToWireV1(atx))
	if err != nil {
		return 0, fmt.Errorf("failed to serialize ATX: %w", err)
	}
	if err := b.publisher.Publish(ctx, pubsub.AtxProtocol, buf); err != nil {
		return 0, fmt.Errorf("failed to broadcast ATX: %w", err)
	}
	return len(buf), nil
}

// getPositioningAtx returns atx id with the highest tick height.
// publish epoch is used for caching the positioning atx.
func (b *Builder) getPositioningAtx(
	ctx context.Context,
	nodeID types.NodeID,
	publish types.EpochID,
) (types.ATXID, error) {
	logger := b.log.With(log.ZShortStringer("smesherID", nodeID), zap.Uint32("publish epoch", publish.Uint32()))
	b.posAtxFinder.finding.Lock()
	defer b.posAtxFinder.finding.Unlock()
	if found := b.posAtxFinder.found; found != nil && found.forPublish == publish {
		logger.Debug("using cached positioning atx", log.ZShortStringer("atx_id", found.id))
		return found.id, nil
	}

	logger.Info("searching for positioning atx")
	id, err := findFullyValidHighTickAtx(
		ctx,
		b.db,
		nodeID,
		b.conf.GoldenATXID,
		b.validator,
		logger,
		VerifyChainOpts.AssumeValidBefore(time.Now().Add(-b.postValidityDelay)),
		VerifyChainOpts.WithTrustedID(nodeID),
		VerifyChainOpts.WithLogger(b.log),
	)
	switch {
	case err == nil:
		b.posAtxFinder.found = &struct {
			id         types.ATXID
			forPublish types.EpochID
		}{id, publish}
		return id, nil
	case errors.Is(err, sql.ErrNotFound):
		logger.Info("using golden atx as positioning atx")
		b.posAtxFinder.found = &struct {
			id         types.ATXID
			forPublish types.EpochID
		}{b.conf.GoldenATXID, publish}
		return b.conf.GoldenATXID, nil
	}
	return id, err
}

func (b *Builder) Regossip(ctx context.Context, nodeID types.NodeID) error {
	epoch := b.layerClock.CurrentLayer().GetEpoch()
	atx, err := atxs.GetIDByEpochAndNodeID(b.db, epoch, nodeID)
	if errors.Is(err, sql.ErrNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	var blob sql.Blob
	if err := atxs.LoadBlob(ctx, b.db, atx.Bytes(), &blob); err != nil {
		return fmt.Errorf("get blob %s: %w", atx.ShortString(), err)
	}
	if len(blob.Bytes) == 0 {
		return nil // checkpoint
	}
	if err := b.publisher.Publish(ctx, pubsub.AtxProtocol, blob.Bytes); err != nil {
		return fmt.Errorf("republish %s: %w", atx.ShortString(), err)
	}
	b.log.Debug("re-gossipped atx", log.ZShortStringer("smesherID", nodeID), log.ZShortStringer("atx", atx))
	return nil
}

// SignAndFinalizeAtx signs the atx with specified signer and calculates the ID of the ATX.
func SignAndFinalizeAtx(signer *signing.EdSigner, atx *types.ActivationTx) error {
	// FIXME - there is no need to sign types.ActivationTX (only ActivationTxVx)
	wireAtx := wire.ActivationTxToWireV1(atx)
	atx.Signature = signer.Sign(signing.ATX, wireAtx.SignedBytes())
	atx.SmesherID = signer.NodeID()
	atx.SetID(types.ATXID(wireAtx.HashInnerBytes()))
	return nil
}

func buildNipostChallengeStartDeadline(roundStart time.Time, gracePeriod time.Duration) time.Time {
	jitter := randomDurationInRange(time.Duration(0), gracePeriod*maxNipostChallengeBuildJitter/100.0)
	return roundStart.Add(jitter).Add(-gracePeriod)
}

func findFullyValidHighTickAtx(
	ctx context.Context,
	db sql.Executor,
	prefNodeID types.NodeID,
	goldenATXID types.ATXID,
	validator nipostValidator,
	logger *zap.Logger,
	opts ...VerifyChainOption,
) (types.ATXID, error) {
	rejectedAtxs := make(map[types.ATXID]struct{})
	filter := func(id types.ATXID) bool {
		_, ok := rejectedAtxs[id]
		return !ok
	}

	for {
		select {
		case <-ctx.Done():
			return types.ATXID{}, ctx.Err()
		default:
		}
		id, err := atxs.GetIDWithMaxHeight(db, prefNodeID, filter)
		if err != nil {
			return types.ATXID{}, err
		}
		logger.Info("found candidate for high-tick atx, verifying its chain", log.ZShortStringer("atx_id", id))
		if err := validator.VerifyChain(ctx, id, goldenATXID, opts...); err != nil {
			logger.Info("rejecting candidate for high-tick atx", zap.Error(err), log.ZShortStringer("atx_id", id))
			rejectedAtxs[id] = struct{}{}
		} else {
			return id, nil
		}
	}
}
