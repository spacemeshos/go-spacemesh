// Package activation is responsible for creating activation transactions and running the mining flow, coordinating
// Post building, sending proofs to PoET and building NIPost structs.
package activation

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/post/shared"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/exp/maps"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation/metrics"
	"github.com/spacemeshos/go-spacemesh/activation/wire"
	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/metrics/public"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/localsql/nipost"
)

var (
	ErrNotFound    = errors.New("not found")
	errNilVrfNonce = errors.New("nil VRF nonce")
)

// PoetConfig is the configuration to interact with the poet server.
type PoetConfig struct {
	// Offset from the epoch start when the poet round starts
	PhaseShift time.Duration `mapstructure:"phase-shift"`
	// CycleGap gives the duration between the end of a PoET round and the start of the next
	CycleGap time.Duration `mapstructure:"cycle-gap"`
	// GracePeriod defines the time before the start of the next PoET round until the node
	// waits before building its NiPoST challenge. Shorter durations allow the node to
	// possibly pick a better positioning ATX, but come with the risk that the node might
	// not be able to validate that ATX and has to fall back to using its own previous ATX.
	GracePeriod       time.Duration `mapstructure:"grace-period"`
	RequestTimeout    time.Duration `mapstructure:"poet-request-timeout"`
	RequestRetryDelay time.Duration `mapstructure:"retry-delay"`
	// Period to find positioning ATX. Must be less, than GracePeriod
	PositioningATXSelectionTimeout time.Duration `mapstructure:"positioning-atx-selection-timeout"`
	CertifierInfoCacheTTL          time.Duration `mapstructure:"certifier-info-cache-ttl"`
	PowParamsCacheTTL              time.Duration `mapstructure:"pow-params-cache-ttl"`
	MaxRequestRetries              int           `mapstructure:"retry-max"`
}

func DefaultPoetConfig() PoetConfig {
	return PoetConfig{
		RequestRetryDelay:     400 * time.Millisecond,
		MaxRequestRetries:     10,
		CertifierInfoCacheTTL: 5 * time.Minute,
		PowParamsCacheTTL:     5 * time.Minute,
	}
}

const (
	defaultPoetRetryInterval = 5 * time.Second
)

// Config defines configuration for Builder.
type Config struct {
	GoldenATXID      types.ATXID
	RegossipInterval time.Duration
}

// Builder struct is the struct that orchestrates the creation of activation transactions
// it is responsible for initializing post, receiving poet proof and orchestrating nipost. after which it will
// calculate total weight and providing relevant view as proof.
type Builder struct {
	accountLock       sync.RWMutex
	coinbaseAccount   types.Address
	conf              Config
	db                sql.Executor
	atxsdata          *atxsdata.Data
	localDB           sql.LocalDatabase
	publisher         pubsub.Publisher
	nipostBuilder     nipostBuilder
	validator         nipostValidator
	layerClock        layerClock
	syncer            syncer
	logger            *zap.Logger
	parentCtx         context.Context
	poets             []PoetService
	poetCfg           PoetConfig
	poetRetryInterval time.Duration
	// delay before PoST in ATX is considered valid (counting from the time it was received)
	postValidityDelay time.Duration
	// ATX versions
	versions []atxVersion

	posAtxFinder positioningAtxFinder

	// post states of each known identity
	postStates PostStates

	// identity states of each known identity
	identitiesStates IdentityStates

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

func WithPoets(poets ...PoetService) BuilderOption {
	return func(b *Builder) {
		b.poets = poets
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

func WithIdentityStates(is IdentityStates) BuilderOption {
	return func(b *Builder) {
		b.identitiesStates = is
	}
}

func BuilderAtxVersions(v AtxVersions) BuilderOption {
	return func(h *Builder) {
		h.versions = append([]atxVersion{{0, types.AtxV1}}, v.asSlice()...)
	}
}

// NewBuilder returns an atx builder that will start a routine that will attempt to create an atx upon each new layer.
func NewBuilder(
	conf Config,
	db sql.Executor,
	atxsdata *atxsdata.Data,
	localDB sql.LocalDatabase,
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
		atxsdata:          atxsdata,
		localDB:           localDB,
		publisher:         publisher,
		nipostBuilder:     nipostBuilder,
		layerClock:        layerClock,
		syncer:            syncer,
		logger:            log,
		poetRetryInterval: defaultPoetRetryInterval,
		postValidityDelay: 12 * time.Hour,
		postStates:        NewPostStates(log),
		identitiesStates:  NewIdentityStateStorage(),
		versions:          []atxVersion{{0, types.AtxV1}},
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
		b.logger.Error("signing key already registered", log.ZShortStringer("id", sig.NodeID()))
		return
	}

	b.logger.Info("registered signing key", log.ZShortStringer("id", sig.NodeID()))
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

// PostStates returns the current state of the post service for each registered smesher.
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

// IdentityStates returns the current state of the identity for each smesher.
func (b *Builder) IdentityStates() map[types.IdentityDescriptor]types.IdentityState {
	states := b.identitiesStates.All()
	res := make(map[types.IdentityDescriptor]types.IdentityState, len(states))
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
					b.logger.Warn("failed to re-gossip", zap.Error(err))
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
				b.logger.Error("failed to reset builder state", log.ZShortStringer("id", sig.NodeID()), zap.Error(err))
				err = fmt.Errorf("reset builder state for id %s: %w", sig.NodeID().ShortString(), err)
				resetErr = errors.Join(resetErr, err)
				continue
			}
			if err := nipost.RemoveChallenge(b.localDB, sig.NodeID()); err != nil {
				b.logger.Error("failed to remove nipost challenge", zap.Error(err))
				err = fmt.Errorf("remove nipost challenge for id %s: %w", sig.NodeID().ShortString(), err)
				resetErr = errors.Join(resetErr, err)
			}
		}
		return resetErr
	default:
		return fmt.Errorf("failed to stop smeshing: %w", err)
	}
}

// SmesherIDs returns the ID of the smesher that created this activation.
func (b *Builder) SmesherIDs() []types.NodeID {
	b.smeshingMutex.Lock()
	defer b.smeshingMutex.Unlock()
	return maps.Keys(b.signers)
}

func (b *Builder) BuildInitialPost(ctx context.Context, nodeID types.NodeID) error {
	// Generate the initial POST if we don't have an ATX...
	if _, err := atxs.GetLastIDByNodeID(b.db, nodeID); err == nil {
		return nil
	}
	// ...and if we haven't stored an initial post yet.
	_, err := nipost.GetPost(b.localDB, nodeID)
	switch {
	case err == nil:
		b.logger.Info("load initial post from db")
		return nil
	case errors.Is(err, sql.ErrNotFound):
		b.logger.Info("creating initial post")
	default:
		return fmt.Errorf("get initial post: %w", err)
	}
	// Create the initial post and save it.
	startTime := time.Now()
	post, postInfo, err := b.nipostBuilder.Proof(ctx, nodeID, shared.ZeroChallenge, nil)
	if err != nil {
		return fmt.Errorf("post execution: %w", err)
	}
	if postInfo.Nonce == nil {
		b.logger.Error("initial PoST is invalid: missing VRF nonce. Check your PoST data",
			log.ZShortStringer("smesherID", nodeID),
		)
		return errNilVrfNonce
	}

	initialPost := nipost.Post{
		Nonce:     post.Nonce,
		Indices:   post.Indices,
		Pow:       post.Pow,
		Challenge: shared.ZeroChallenge,

		NumUnits:      postInfo.NumUnits,
		CommitmentATX: postInfo.CommitmentATX,
		VRFNonce:      *postInfo.Nonce,
	}
	err = b.validator.PostV2(ctx, nodeID, postInfo.CommitmentATX, post, shared.ZeroChallenge, postInfo.NumUnits)
	if err != nil {
		b.logger.Error("initial POST is invalid", log.ZShortStringer("smesherID", nodeID), zap.Error(err))
		if err := nipost.RemovePost(b.localDB, nodeID); err != nil {
			b.logger.Fatal("failed to remove initial post", log.ZShortStringer("smesherID", nodeID), zap.Error(err))
		}
		return fmt.Errorf("initial POST is invalid: %w", err)
	}

	metrics.PostDuration.Set(float64(time.Since(startTime).Nanoseconds()))
	public.PostSeconds.Set(float64(time.Since(startTime)))
	b.logger.Info("created the initial post")

	return nipost.AddPost(b.localDB, nodeID, initialPost)
}

func (b *Builder) buildPost(ctx context.Context, nodeID types.NodeID) error {
	for {
		err := b.BuildInitialPost(ctx, nodeID)
		if err == nil {
			return nil
		}
		b.logger.Error("failed to generate initial proof:", zap.Error(err))
		currentLayer := b.layerClock.CurrentLayer()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-b.layerClock.AwaitLayer(currentLayer.Add(1)):
		}
	}
}

func (b *Builder) run(ctx context.Context, sig *signing.EdSigner) {
	defer b.logger.Info("atx builder stopped")
	if err := b.buildPost(ctx, sig.NodeID()); err != nil {
		b.logger.Error("failed to build initial post:", zap.Error(err))
		return
	}
	var eg errgroup.Group
	for _, poet := range b.poets {
		eg.Go(func() error {
			_, err := poet.Certify(ctx, sig.NodeID())
			switch {
			case errors.Is(err, ErrCertificatesNotSupported):
				b.logger.Debug("not certifying (not supported in poet)",
					log.ZShortStringer("smesherID", sig.NodeID()),
					zap.String("poet", poet.Address()),
				)
			case err != nil:
				b.logger.Warn("failed to certify poet", zap.Error(err), log.ZShortStringer("smesherID", sig.NodeID()))
			}
			return nil
		})
	}
	eg.Wait()

	for {
		err := b.PublishActivationTx(ctx, sig)
		if err == nil {
			continue
		} else if errors.Is(err, context.Canceled) {
			return
		}

		b.logger.Warn("failed to publish atx", zap.Error(err))

		poetErr := &PoetSvcUnstableError{}
		switch {
		case errors.Is(err, ErrATXChallengeExpired):
			b.logger.Debug("retrying with new challenge after waiting for a layer")
			if err := b.nipostBuilder.ResetState(sig.NodeID()); err != nil {
				b.logger.Error("failed to reset nipost builder state", zap.Error(err))
			}
			if err := nipost.RemoveChallenge(b.localDB, sig.NodeID()); err != nil {
				b.logger.Error("failed to discard challenge", zap.Error(err))
			}
			// give node some time to sync in case selecting the positioning ATX caused the challenge to expire
			currentLayer := b.layerClock.CurrentLayer()
			select {
			case <-ctx.Done():
				return
			case <-b.layerClock.AwaitLayer(currentLayer.Add(1)):
			}
		case errors.As(err, &poetErr):
			b.logger.Warn("retrying after poet retry interval",
				zap.Duration("interval", b.poetRetryInterval),
				zap.Error(poetErr.source),
			)
			select {
			case <-ctx.Done():
				return
			case <-time.After(b.poetRetryInterval):
			}
		case errors.Is(err, ErrInvalidInitialPost):
			// delete the existing db post
			// call build initial post again
			b.logger.Error("initial post is no longer valid. regenerating initial post")
			if err := b.nipostBuilder.ResetState(sig.NodeID()); err != nil {
				b.logger.Error("failed to reset nipost builder state", zap.Error(err))
			}
			if err := nipost.RemoveChallenge(b.localDB, sig.NodeID()); err != nil {
				b.logger.Error("failed to discard challenge", zap.Error(err))
			}
			if err := nipost.RemovePost(b.localDB, sig.NodeID()); err != nil {
				b.logger.Error("failed to remove existing post from db", zap.Error(err))
			}
			if err := b.buildPost(ctx, sig.NodeID()); err != nil {
				b.logger.Error("failed to regenerate initial post:", zap.Error(err))
			}
		default:
			b.logger.Warn("unknown error", zap.Error(err))
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
	logger := b.logger.With(log.ZShortStringer("smesherID", nodeID))

	if err := b.identitiesStates.Set(nodeID, types.IdentityStateWaitForATXSyncing); err != nil {
		b.logger.Warn("failed to switch identity state",
			zap.Stringer("smesherID", nodeID),
			zap.Error(err),
		)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-b.syncer.RegisterForATXSynced():
	}
	currentEpochId := b.layerClock.CurrentLayer().GetEpoch()

	if err := b.identitiesStates.Set(nodeID, types.IdentityStateWaitForPoetRoundStart); err != nil {
		b.logger.Warn("failed to switch identity state",
			zap.Stringer("smesherID", nodeID),
			zap.Error(err),
		)
	}

	// Try to get existing challenge
	existingChallenge, err := b.getExistingChallenge(logger, currentEpochId, nodeID)
	if err != nil {
		return nil, fmt.Errorf("getting existing NiPoST challenge: %w", err)
	}

	if existingChallenge != nil {
		return existingChallenge, nil
	}

	// Start building new challenge:
	// 1. get previous ATX
	prevAtx, err := b.GetPrevAtx(nodeID)
	switch {
	case err == nil:
		currentEpochId = max(currentEpochId, prevAtx.PublishEpoch)
	case errors.Is(err, sql.ErrNotFound):
		// no previous ATX
	case err != nil:
		return nil, fmt.Errorf("get last ATX: %w", err)
	}

	// 2. check if we didn't miss beginning of PoET round
	until := time.Until(b.poetRoundStart(currentEpochId))
	if until <= 0 {
		metrics.PublishLateWindowLatency.Observe(-until.Seconds())
		currentEpochId++
		until = time.Until(b.poetRoundStart(currentEpochId))
	}

	metrics.PublishOntimeWindowLatency.Observe(until.Seconds())

	publishEpochId := currentEpochId + 1

	// 3. wait if needed till getting closer to PoET round start
	poetStartsAt := b.poetRoundStart(currentEpochId)
	wait := poetStartsAt.Add(-b.poetCfg.GracePeriod)
	if time.Until(wait) > 0 {
		logger.Info("paused building NiPoST challenge. Waiting until closer to poet start to get a better posATX",
			zap.Duration("till poet round", until),
			zap.Uint32("current epoch", currentEpochId.Uint32()),
			zap.Time("waiting until", wait),
		)
		events.EmitPoetWaitRound(nodeID, currentEpochId, publishEpochId, wait)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Until(wait)):
		}
	}
	if b.poetCfg.PositioningATXSelectionTimeout > 0 {
		var cancel context.CancelFunc

		deadline := poetStartsAt.Add(-b.poetCfg.GracePeriod).Add(b.poetCfg.PositioningATXSelectionTimeout)
		ctx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
	}

	// 4. build new challenge
	logger.Info("building new NiPOST challenge", zap.Uint32("current_epoch", currentEpochId.Uint32()))

	prevAtx, err = b.GetPrevAtx(nodeID)

	var challenge *types.NIPostChallenge
	switch {
	case errors.Is(err, sql.ErrNotFound):
		logger.Info("no previous ATX found, creating an initial nipost challenge")

		challenge, err = b.buildInitialNIPostChallenge(ctx, logger, nodeID, publishEpochId)
		if err != nil {
			return nil, err
		}

	case err != nil:
		return nil, fmt.Errorf("get last ATX: %w", err)
	default:
		// regular ATX challenge
		posAtx, err := b.getPositioningAtx(ctx, nodeID, publishEpochId, prevAtx)
		if err != nil {
			return nil, fmt.Errorf("failed to get positioning ATX: %w", err)
		}
		challenge = &types.NIPostChallenge{
			PublishEpoch:   publishEpochId,
			Sequence:       prevAtx.Sequence + 1,
			PrevATXID:      prevAtx.ID(),
			PositioningATX: posAtx,
		}
	}
	logger.Debug("persisting the new NiPOST challenge", zap.Object("challenge", challenge))
	if err := nipost.AddChallenge(b.localDB, nodeID, challenge); err != nil {
		return nil, fmt.Errorf("add nipost challenge: %w", err)
	}
	return challenge, nil
}

func (b *Builder) getExistingChallenge(
	logger *zap.Logger,
	currentEpochId types.EpochID,
	nodeID types.NodeID,
) (*types.NIPostChallenge, error) {
	challenge, err := nipost.Challenge(b.localDB, nodeID)

	switch {
	case errors.Is(err, sql.ErrNotFound):
		return nil, nil

	case err != nil:
		return nil, fmt.Errorf("get nipost challenge: %w", err)

	case challenge.PublishEpoch < currentEpochId:
		logger.Info(
			"existing NiPoST challenge is stale, resetting state",
			zap.Uint32("current_epoch", currentEpochId.Uint32()),
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
		return nil, nil
	}

	// challenge is fresh
	logger.Debug("loaded NiPoST challenge from local state",
		zap.Uint32("current_epoch", currentEpochId.Uint32()),
		zap.Uint32("publish_epoch", challenge.PublishEpoch.Uint32()),
	)
	return challenge, nil
}

func (b *Builder) buildInitialNIPostChallenge(
	ctx context.Context,
	logger *zap.Logger,
	nodeID types.NodeID,
	publishEpochId types.EpochID,
) (*types.NIPostChallenge, error) {
	post, err := nipost.GetPost(b.localDB, nodeID)
	if err != nil {
		return nil, fmt.Errorf("get initial post: %w", err)
	}
	logger.Info("verifying the initial post")
	initialPost := &types.Post{
		Nonce:   post.Nonce,
		Indices: post.Indices,
		Pow:     post.Pow,
	}
	err = b.validator.PostV2(ctx, nodeID, post.CommitmentATX, initialPost, shared.ZeroChallenge, post.NumUnits)
	if err != nil {
		logger.Error("initial POST is invalid", zap.Error(err))
		if err := nipost.RemovePost(b.localDB, nodeID); err != nil {
			logger.Fatal("failed to remove initial post", zap.Error(err))
		}
		return nil, fmt.Errorf("initial POST is invalid: %w", err)
	}
	posAtx, err := b.getPositioningAtx(ctx, nodeID, publishEpochId, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get positioning ATX: %w", err)
	}
	return &types.NIPostChallenge{
		PublishEpoch:   publishEpochId,
		Sequence:       0,
		PrevATXID:      types.EmptyATXID,
		PositioningATX: posAtx,
		CommitmentATX:  &post.CommitmentATX,
		InitialPost: &types.Post{
			Nonce:   post.Nonce,
			Indices: post.Indices,
			Pow:     post.Pow,
		},
	}, nil
}

func (b *Builder) GetPrevAtx(nodeID types.NodeID) (*types.ActivationTx, error) {
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

	b.logger.Info("atx challenge is ready",
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

	b.logger.Info("awaiting atx publication epoch",
		zap.Uint32("pub_epoch", challenge.PublishEpoch.Uint32()),
		zap.Uint32("pub_epoch_first_layer", challenge.PublishEpoch.FirstLayer().Uint32()),
		zap.Uint32("current_layer", b.layerClock.CurrentLayer().Uint32()),
		log.ZShortStringer("smesherID", sig.NodeID()),
	)
	select {
	case <-ctx.Done():
		return fmt.Errorf("wait for publication epoch: %w", ctx.Err())
	case <-b.layerClock.AwaitLayer(challenge.PublishEpoch.FirstLayer()):
	}

	for {
		b.logger.Info(
			"broadcasting ATX",
			log.ZShortStringer("atx_id", atx.ID()),
			log.ZShortStringer("smesherID", sig.NodeID()),
			log.DebugField(b.logger, zap.Object("atx", atx)),
		)
		size, err := b.broadcast(ctx, atx)
		if err == nil {
			b.logger.Info("atx published", log.ZShortStringer("atx_id", atx.ID()), zap.Int("size", size))
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
	target := challenge.PublishEpoch + 1
	events.EmitAtxPublished(
		sig.NodeID(),
		challenge.PublishEpoch, target,
		atx.ID(),
		b.layerClock.LayerToTime(target.FirstLayer()),
	)
	return nil
}

func (b *Builder) poetRoundStart(epoch types.EpochID) time.Time {
	return b.layerClock.LayerToTime(epoch.FirstLayer()).Add(b.poetCfg.PhaseShift)
}

type builtAtx interface {
	ID() types.ATXID

	scale.Encodable
	zapcore.ObjectMarshaler
}

func (b *Builder) createAtx(
	ctx context.Context,
	sig *signing.EdSigner,
	challenge *types.NIPostChallenge,
) (builtAtx, error) {
	var challengeHash types.Hash32

	version := b.version(challenge.PublishEpoch)
	switch version {
	case types.AtxV1:
		challengeHash = wire.NIPostChallengeToWireV1(challenge).Hash()
	case types.AtxV2:
		challengeHash = wire.NIPostChallengeToWireV2(challenge).Hash()
	default:
		return nil, fmt.Errorf("unknown ATX version: %v", version)
	}
	b.logger.Info("building ATX", zap.Stringer("smesherID", sig.NodeID()), zap.Stringer("version", version))

	nipostState, err := b.nipostBuilder.BuildNIPost(ctx, sig, challengeHash, challenge)
	if err != nil {
		return nil, fmt.Errorf("build NIPost: %w", err)
	}

	if challenge.PublishEpoch < b.layerClock.CurrentLayer().GetEpoch() {
		if challenge.PrevATXID == types.EmptyATXID {
			// initial NIPoST challenge is not discarded; don't return ErrATXChallengeExpired
			return nil, errors.New("atx publish epoch has passed during nipost construction")
		}
		return nil, fmt.Errorf("%w: atx publish epoch has passed during nipost construction", ErrATXChallengeExpired)
	}

	switch version {
	case types.AtxV1:
		atx := wire.ActivationTxV1{
			InnerActivationTxV1: wire.InnerActivationTxV1{
				NIPostChallengeV1: *wire.NIPostChallengeToWireV1(challenge),
				Coinbase:          b.Coinbase(),
				NumUnits:          nipostState.NumUnits,
				NIPost:            wire.NiPostToWireV1(nipostState.NIPost),
			},
		}

		switch {
		case challenge.PrevATXID == types.EmptyATXID:
			atx.VRFNonce = (*uint64)(&nipostState.VRFNonce)
		default:
			oldNonce, err := atxs.NonceByID(b.db, challenge.PrevATXID)
			if err != nil {
				b.logger.Warn("failed to get VRF nonce for ATX",
					zap.Error(err),
					log.ZShortStringer("smesherID", sig.NodeID()),
				)
				break
			}
			if nipostState.VRFNonce != oldNonce {
				b.logger.Info(
					"attaching a new VRF nonce in ATX",
					log.ZShortStringer("smesherID", sig.NodeID()),
					zap.Uint64("new nonce", uint64(nipostState.VRFNonce)),
					zap.Uint64("old nonce", uint64(oldNonce)),
				)
				atx.VRFNonce = (*uint64)(&nipostState.VRFNonce)
			}
		}
		atx.Sign(sig)

		return &atx, nil
	case types.AtxV2:
		atx := &wire.ActivationTxV2{
			PublishEpoch:   challenge.PublishEpoch,
			PositioningATX: challenge.PositioningATX,
			Coinbase:       b.Coinbase(),
			VRFNonce:       (uint64)(nipostState.VRFNonce),
			NiPosts: []wire.NiPostsV2{
				{
					Membership: wire.MerkleProofV2{
						Nodes: nipostState.Membership.Nodes,
					},
					Challenge: types.Hash32(nipostState.NIPost.PostMetadata.Challenge),
					Posts: []wire.SubPostV2{
						{
							Post:                *wire.PostToWireV1(nipostState.Post),
							NumUnits:            nipostState.NumUnits,
							MembershipLeafIndex: nipostState.Membership.LeafIndex,
						},
					},
				},
			},
		}

		if challenge.InitialPost != nil {
			atx.Initial = &wire.InitialAtxPartsV2{
				Post:          *wire.PostToWireV1(challenge.InitialPost),
				CommitmentATX: *challenge.CommitmentATX,
			}
		} else {
			atx.PreviousATXs = []types.ATXID{challenge.PrevATXID}
		}
		atx.Sign(sig)
		return atx, nil
	default:
		// `version` is already checked in the beginning of the function
		// and it cannot have a different value.
		panic("unreachable")
	}
}

func (b *Builder) broadcast(ctx context.Context, atx scale.Encodable) (int, error) {
	buf, err := codec.Encode(atx)
	if err != nil {
		return 0, fmt.Errorf("failed to serialize ATX: %w", err)
	}
	if err := b.publisher.Publish(ctx, pubsub.AtxProtocol, buf); err != nil {
		return 0, fmt.Errorf("failed to broadcast ATX: %w", err)
	}
	return len(buf), nil
}

// searchPositioningAtx returns atx id with the highest tick height.
// publish epoch is used for caching the positioning atx.
func (b *Builder) searchPositioningAtx(
	ctx context.Context,
	nodeID types.NodeID,
	publish types.EpochID,
) (types.ATXID, error) {
	logger := b.logger.With(log.ZShortStringer("smesherID", nodeID), zap.Uint32("publish epoch", publish.Uint32()))

	b.posAtxFinder.finding.Lock()
	defer b.posAtxFinder.finding.Unlock()

	if found := b.posAtxFinder.found; found != nil && found.forPublish == publish {
		logger.Debug("using cached positioning atx", log.ZShortStringer("atx_id", found.id))
		return found.id, nil
	}

	latestPublished, err := atxs.LatestEpoch(b.db)
	if err != nil {
		return types.EmptyATXID, fmt.Errorf("get latest epoch: %w", err)
	}

	logger.Info("searching for positioning atx", zap.Uint32("latest_epoch", latestPublished.Uint32()))

	// positioning ATX publish epoch must be lower than the publish epoch of built ATX
	positioningAtxPublished := min(latestPublished, publish-1)
	id, err := findFullyValidHighTickAtx(
		ctx,
		b.atxsdata,
		positioningAtxPublished,
		b.conf.GoldenATXID,
		b.validator,
		logger,
		VerifyChainOpts.AssumeValidBefore(time.Now().Add(-b.postValidityDelay)),
		VerifyChainOpts.WithTrustedID(nodeID),
		VerifyChainOpts.WithLogger(b.logger),
	)
	if err != nil {
		logger.Info("search failed - using golden atx as positioning atx", zap.Error(err))
		id = b.conf.GoldenATXID
	}

	b.posAtxFinder.found = &struct {
		id         types.ATXID
		forPublish types.EpochID
	}{id, publish}

	return id, nil
}

// getPositioningAtx returns the positioning ATX.
// The provided previous ATX is picked if it has a greater or equal
// tick count as the ATX selected in `searchPositioningAtx`.
func (b *Builder) getPositioningAtx(
	ctx context.Context,
	nodeID types.NodeID,
	publish types.EpochID,
	previous *types.ActivationTx,
) (types.ATXID, error) {
	id, err := b.searchPositioningAtx(ctx, nodeID, publish)
	if err != nil {
		return types.EmptyATXID, err
	}

	b.logger.Debug("found candidate positioning atx",
		log.ZShortStringer("id", id),
		log.ZShortStringer("smesherID", nodeID),
	)

	if previous == nil {
		b.logger.Info("selected positioning atx",
			log.ZShortStringer("id", id),
			log.ZShortStringer("smesherID", nodeID))
		return id, nil
	}

	if id == b.conf.GoldenATXID {
		id = previous.ID()
		b.logger.Info("selected previous as positioning atx",
			log.ZShortStringer("id", id),
			log.ZShortStringer("smesherID", nodeID),
		)
		return id, nil
	}

	candidate, err := atxs.Get(b.db, id)
	if err != nil {
		return types.EmptyATXID, fmt.Errorf("get candidate pos ATX %s: %w", id.ShortString(), err)
	}

	if previous.TickHeight() >= candidate.TickHeight() {
		id = previous.ID()
		b.logger.Info("selected previous as positioning atx",
			log.ZShortStringer("id", id),
			log.ZShortStringer("smesherID", nodeID),
		)
		return id, nil
	}

	b.logger.Info("selected positioning atx", log.ZShortStringer("id", id), log.ZShortStringer("smesherID", nodeID))
	return id, nil
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
	if _, err := atxs.LoadBlob(ctx, b.db, atx.Bytes(), &blob); err != nil {
		return fmt.Errorf("get blob %s: %w", atx.ShortString(), err)
	}
	if len(blob.Bytes) == 0 {
		return nil // checkpoint
	}
	if err := b.publisher.Publish(ctx, pubsub.AtxProtocol, blob.Bytes); err != nil {
		return fmt.Errorf("republish %s: %w", atx.ShortString(), err)
	}
	b.logger.Debug("re-gossipped atx", log.ZShortStringer("smesherID", nodeID), log.ZShortStringer("atx", atx))
	return nil
}

func (b *Builder) version(publish types.EpochID) types.AtxVersion {
	version := types.AtxV1
	for _, v := range b.versions {
		if publish >= v.publish {
			version = v.AtxVersion
		}
	}
	return version
}

func findFullyValidHighTickAtx(
	ctx context.Context,
	atxdata *atxsdata.Data,
	publish types.EpochID,
	goldenATXID types.ATXID,
	validator nipostValidator,
	logger *zap.Logger,
	opts ...VerifyChainOption,
) (types.ATXID, error) {
	var found *types.ATXID

	// iterate trough epochs, to get first valid, not malicious ATX with the biggest height
	atxdata.IterateHighTicksInEpoch(publish+1, func(id types.ATXID) (contSearch bool) {
		logger.Debug("found candidate for high-tick atx", log.ZShortStringer("id", id))
		if ctx.Err() != nil {
			return false
		}
		// verify ATX-candidate by getting their dependencies (previous Atx, positioning ATX etc.)
		// and verifying PoST for every dependency
		if err := validator.VerifyChain(ctx, id, goldenATXID, opts...); err != nil {
			logger.Debug("rejecting candidate for high-tick atx", zap.Error(err), log.ZShortStringer("id", id))
			return true
		}
		found = &id
		return false
	})

	if ctx.Err() != nil {
		return types.ATXID{}, ctx.Err()
	}

	if found == nil {
		return types.ATXID{}, ErrNotFound
	}

	return *found, nil
}
