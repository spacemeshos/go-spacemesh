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
	"go.uber.org/atomic"

	atypes "github.com/spacemeshos/go-spacemesh/activation/types"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
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

type nipostBuilder interface {
	updatePoETProvers([]PoetProvingServiceClient)
	BuildNIPost(ctx context.Context, challenge *types.Hash32, commitmentAtx types.ATXID, poetProofDeadline time.Time) (*types.NIPost, time.Duration, error)
}

type atxHandler interface {
	GetPosAtxID() (types.ATXID, error)
	AwaitAtx(id types.ATXID) chan struct{}
	UnsubscribeAtx(id types.ATXID)
}

type signer interface {
	Sign(m []byte) []byte
}

type syncer interface {
	RegisterForATXSynced() chan struct{}
}

// SmeshingProvider defines the functionality required for the node's Smesher API.
type SmeshingProvider interface {
	Smeshing() bool
	StartSmeshing(types.Address, atypes.PostSetupOpts) error
	StopSmeshing(bool) error
	SmesherID() types.NodeID
	Coinbase() types.Address
	SetCoinbase(coinbase types.Address)
}

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

	signer
	accountLock       sync.RWMutex
	nodeID            types.NodeID
	coinbaseAccount   types.Address
	goldenATXID       types.ATXID
	layersPerEpoch    uint32
	cdb               *datastore.CachedDB
	atxHandler        atxHandler
	publisher         pubsub.Publisher
	nipostBuilder     nipostBuilder
	postSetupProvider PostSetupProvider
	challenge         *types.NIPostChallenge
	initialPost       *types.Post

	// commitmentAtx caches the ATX ID used for the PoST commitment by this node. It is set / fetched
	// from the DB by calling `getCommitmentAtx()` and cAtxMutex protects its access.
	commitmentAtx *types.ATXID
	cAtxMutex     sync.Mutex

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
	// lastPostGenDuration is the duration that the last PoST proof
	// took to build. It is used to predict a safe moment in time
	// when we need to collect the PoET proofs and start building
	// PoST to complete building NiPoST.
	lastPostGenDuration time.Duration
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
func NewBuilder(conf Config, nodeID types.NodeID, signer signer, cdb *datastore.CachedDB, hdlr atxHandler, publisher pubsub.Publisher,
	nipostBuilder nipostBuilder, postSetupProvider PostSetupProvider, layerClock layerClock,
	syncer syncer, log log.Log, opts ...BuilderOption,
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
		lastPostGenDuration:   0,
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
func (b *Builder) StartSmeshing(coinbase types.Address, opts atypes.PostSetupOpts) error {
	b.smeshingMutex.Lock()
	defer b.smeshingMutex.Unlock()

	if !b.started.CompareAndSwap(false, true) {
		return errors.New("already started")
	}

	b.coinbaseAccount = coinbase
	ctx, stop := context.WithCancel(b.parentCtx)
	b.stop = stop

	commitmentAtx, err := b.getCommitmentAtx(ctx)
	if err != nil {
		b.started.Store(false)
		return fmt.Errorf("failed to start post setup session: %w", err)
	}

	doneChan, err := b.postSetupProvider.StartSession(opts, *commitmentAtx)
	if err != nil {
		b.started.Store(false)
		return fmt.Errorf("failed to start post setup session: %w", err)
	}
	go func() {
		// Signal that smeshing is finished. StartSmeshing will refuse to restart until this
		// goroutine finished.
		defer b.started.Store(false)
		select {
		case <-ctx.Done():
			return
		case <-doneChan:
		}

		if s := b.postSetupProvider.Status(); s.State != atypes.PostSetupStateComplete {
			b.log.WithContext(ctx).With().Error("failed to complete post setup", log.Err(b.postSetupProvider.LastError()))
			return
		}
		b.run(ctx)
	}()

	return nil
}

// findCommitmentAtx determines the best commitment ATX to use for the node.
// It will use the ATX with the highest height seen by the node and defaults to the goldenATX,
// when no ATXs have yet been published.
func (b *Builder) findCommitmentAtx() (types.ATXID, error) {
	atx, err := atxs.GetAtxIDWithMaxHeight(b.cdb)
	if errors.Is(err, sql.ErrNotFound) {
		b.log.With().Info("using golden atx as commitment atx")
		return b.goldenATXID, nil
	}
	if err != nil {
		return *types.EmptyATXID, fmt.Errorf("get commitment atx: %w", err)
	}
	return atx, nil
}

// StopSmeshing stops the atx builder.
// It doesn't wait for the smeshing to stop.
func (b *Builder) StopSmeshing(deleteFiles bool) error {
	b.smeshingMutex.Lock()
	defer b.smeshingMutex.Unlock()

	if !b.started.Load() {
		return errors.New("not started")
	}

	if err := b.postSetupProvider.StopSession(deleteFiles); err != nil {
		return fmt.Errorf("failed to stop post data creation session: %w", err)
	}

	b.stop()
	return nil
}

// SmesherID returns the ID of the smesher that created this activation.
func (b *Builder) SmesherID() types.NodeID {
	return b.nodeID
}

// SignAtx signs the atx and assigns the signature into atx.Sig
// this function returns an error if atx could not be converted to bytes.
func (b *Builder) SignAtx(atx *types.ActivationTx) error {
	if err := SignAtx(b, atx); err != nil {
		return err
	}
	if err := atx.CalcAndSetID(); err != nil {
		return err
	}
	atx.SetNodeID(&b.nodeID)
	return nil
}

func (b *Builder) run(ctx context.Context) {
	if err := b.generateProof(ctx); err != nil {
		b.log.Error("Failed to generate proof: %w", err)
		return
	}

	// ensure layer 1 has arrived
	select {
	case <-ctx.Done():
		return
	case <-b.layerClock.AwaitLayer(types.NewLayerID(1)):
	}

	b.waitForFirstATX(ctx)
	b.loop(ctx)
}

func (b *Builder) generateProof(ctx context.Context) error {
	err := b.loadChallenge()
	if err != nil {
		b.log.Info("challenge not loaded: %s", err)
	}

	commitmentAtx, err := b.getCommitmentAtx(ctx)
	if err != nil {
		return fmt.Errorf("failed to get commitment atx: %w", err)
	}

	// don't generate the commitment every time smeshing is starting, but once only.
	if _, err := b.cdb.GetPrevAtx(b.nodeID); err != nil {
		// Once initialized, run the execution phase with zero-challenge,
		// to create the initial proof (the commitment).
		startTime := time.Now()
		b.initialPost, _, err = b.postSetupProvider.GenerateProof(shared.ZeroChallenge, *commitmentAtx)
		if err != nil {
			return fmt.Errorf("post execution: %w", err)
		}
		b.lastPostGenDuration = time.Since(startTime)
	}

	return nil
}

func (b *Builder) waitForFirstATX(ctx context.Context) bool {
	currentLayer := b.layerClock.GetCurrentLayer()
	currEpoch := currentLayer.GetEpoch()
	if currEpoch == 0 { // genesis miner
		return false
	}
	if prev, err := b.cdb.GetPrevAtx(b.nodeID); err == nil {
		if prev.PublishEpoch() == currEpoch {
			// miner has published in the current epoch
			return false
		}
	}

	// miner didn't publish ATX that targets current epoch.

	// this estimate work if the majority of the nodes use poet servers that are configured the same way.
	// TODO: do better when nodes use different poet services
	window := b.poetCfg.PhaseShift - b.poetCfg.CycleGap + b.poetCfg.GracePeriod
	windowEnd := b.layerClock.LayerToTime(currEpoch.FirstLayer()).Add(window)
	wait := time.Until(windowEnd)
	if wait <= 0 { // missed the window. wait for the next epoch
		waitTill := (currEpoch + 1).FirstLayer()
		b.log.WithContext(ctx).With().Info("waiting till next epoch to build atx",
			log.Stringer("current_epoch", currEpoch),
			log.Stringer("wait_till", waitTill),
			log.Stringer("wait_till_epoch", waitTill.GetEpoch()))
		select {
		case <-ctx.Done():
			return false
		case <-b.layerClock.AwaitLayer(waitTill):
			wait = window
		}
	}
	timer := time.NewTimer(wait)
	b.log.WithContext(ctx).With().Info("waiting for the poet window expires",
		log.Duration("wait", wait))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
	}
	b.log.WithContext(ctx).With().Info("ready to build first atx",
		log.Stringer("current_layer", b.layerClock.GetCurrentLayer()),
		log.Stringer("current_epoch", b.layerClock.GetCurrentLayer().GetEpoch()))
	return true
}

func (b *Builder) receivePendingPoetClients() *[]PoetProvingServiceClient {
	return b.pendingPoetClients.Swap(nil)
}

// loop is the main loop that tries to create an atx per tick received from the global clock.
func (b *Builder) loop(ctx context.Context) {
	var poetRetryTimer *time.Timer
	defer func() {
		if poetRetryTimer != nil {
			poetRetryTimer.Stop()
		}
	}()
	defer b.log.Info("atx builder is stopped")
	for {
		if poetClients := b.receivePendingPoetClients(); poetClients != nil {
			b.nipostBuilder.updatePoETProvers(*poetClients)
		}

		ctx := log.WithNewSessionID(ctx)
		if err := b.PublishActivationTx(ctx); err != nil {
			b.log.With().Warning("PublishActivationTx failed", log.Err(err))
			if errors.Is(err, ErrStopRequested) {
				return
			}
			b.log.WithContext(ctx).With().Error("error attempting to publish atx",
				b.layerClock.GetCurrentLayer(),
				b.currentEpoch(),
				log.Err(err))

			switch {
			case errors.Is(err, ErrATXChallengeExpired):
				b.log.Info("Discarding challenge")
				b.discardChallenge()
				// can be retried immediately with a new challenge
			case errors.Is(err, ErrPoetServiceUnstable):
				b.log.Info("Setting up poet retry timer")
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
				b.log.Warning("Unknown error")
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
	select {
	case <-ctx.Done():
		return ErrStopRequested
	case <-b.syncer.RegisterForATXSynced():
	}
	challenge := &types.NIPostChallenge{}
	atxID, pubLayerID, err := b.GetPositioningAtxInfo()
	if err != nil {
		return fmt.Errorf("failed to get positioning ATX: %w", err)
	}
	challenge.PositioningATX = atxID
	challenge.PubLayerID = pubLayerID.Add(b.layersPerEpoch)
	if prevAtx, err := b.cdb.GetPrevAtx(b.nodeID); err != nil {
		commitmentAtx, err := b.getCommitmentAtx(ctx)
		if err != nil {
			return fmt.Errorf("failed to get commitment ATX: %w", err)
		}

		challenge.CommitmentATX = commitmentAtx
		challenge.InitialPostIndices = b.initialPost.Indices
	} else {
		challenge.PrevATXID = prevAtx.ID
		challenge.Sequence = prevAtx.Sequence + 1
	}
	b.challenge = challenge
	if err := kvstore.AddNIPostChallenge(b.cdb, b.challenge); err != nil {
		return fmt.Errorf("failed to store nipost challenge: %w", err)
	}
	return nil
}

// UpdatePoETServer updates poet client. Context is used to verify that the target is responsive.
func (b *Builder) UpdatePoETServers(ctx context.Context, endpoints []string) error {
	b.log.With().Debug("request to update poet services", log.Array("endpoints", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
		for _, endpoint := range endpoints {
			encoder.AppendString(endpoint)
		}
		return nil
	})))

	clients := make([]PoetProvingServiceClient, 0, len(endpoints))
	for _, endpoint := range endpoints {
		client := b.poetClientInitializer(endpoint)
		// TODO(dshulyak) not enough information to verify that PoetServiceID matches with an expected one.
		// Maybe it should be provided during update.
		sid, err := client.PoetServiceID(ctx)
		if err != nil {
			return &PoetSvcUnstableError{source: fmt.Errorf("failed to query Poet '%s' for ID (%w)", endpoint, err)}
		}
		b.log.With().Debug("preparing to update poet service", log.String("poet_id", util.Bytes2Hex(sid)))
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

func (b *Builder) loadChallenge() error {
	nipost, err := kvstore.GetNIPostChallenge(b.cdb)
	if err != nil {
		return err
	}
	b.challenge = nipost
	return nil
}

func (b *Builder) getCommitmentAtx(ctx context.Context) (*types.ATXID, error) {
	b.cAtxMutex.Lock()
	defer b.cAtxMutex.Unlock()

	if b.commitmentAtx != nil {
		return b.commitmentAtx, nil
	}

	select {
	case <-ctx.Done():
		return nil, ErrStopRequested
	case <-b.syncer.RegisterForATXSynced():
	}

	// if this node has already published an ATX, get its initial ATX and from it the commitment ATX
	atxId, err := atxs.GetFirstIDByNodeID(b.cdb, b.nodeID)
	if err == nil {
		atx, err := atxs.Get(b.cdb, atxId)
		if err == nil {
			b.commitmentAtx = atx.CommitmentATX
			return b.commitmentAtx, nil
		}
	}

	// if this node has not published an ATX, get the commitment ATX id from the kvstore (if it exists)
	// otherwise select the best ATX with `findCommitmentAtx`
	atxId, err = kvstore.GetCommitmentATXForNode(b.cdb, b.nodeID)
	switch {
	case errors.Is(err, sql.ErrNotFound):
		atxId, err := b.findCommitmentAtx()
		if err != nil {
			return nil, fmt.Errorf("failed to determine commitment ATX: %w", err)
		}
		if err := kvstore.AddCommitmentATXForNode(b.cdb, atxId, b.nodeID); err != nil {
			return nil, fmt.Errorf("failed to store commitment ATX: %w", err)
		}
		b.commitmentAtx = &atxId
		return b.commitmentAtx, nil
	case err != nil:
		return nil, fmt.Errorf("failed to get commitment ATX: %w", err)
	}

	b.commitmentAtx = &atxId
	return b.commitmentAtx, nil
}

// PublishActivationTx attempts to publish an atx, it returns an error if an atx cannot be created.
func (b *Builder) PublishActivationTx(ctx context.Context) error {
	b.discardChallengeIfStale()
	logger := b.log.WithContext(ctx)

	if b.challenge != nil {
		logger.With().Info("using existing atx challenge", log.Stringer("current_epoch", b.currentEpoch()))
	} else {
		logger.With().Info("building new atx challenge", log.Stringer("current_epoch", b.currentEpoch()))
		err := b.buildNIPostChallenge(ctx)
		if err != nil {
			return fmt.Errorf("failed to build new atx challenge: %w", err)
		}
	}

	logger.With().Info("new atx challenge is ready",
		log.Stringer("current_epoch", b.currentEpoch()),
		log.Stringer("publish_epoch", b.challenge.PublishEpoch()),
		log.Stringer("target_epoch", b.challenge.TargetEpoch()))

	if b.pendingATX == nil {
		var err error
		b.pendingATX, err = b.createAtx(ctx)
		if err != nil {
			return fmt.Errorf("create ATX: %w", err)
		}
	}

	atx := b.pendingATX
	if err := b.SignAtx(atx); err != nil {
		return fmt.Errorf("sign: %w", err)
	}

	atxReceived := b.atxHandler.AwaitAtx(atx.ID())
	defer b.atxHandler.UnsubscribeAtx(atx.ID())
	size, err := b.broadcast(ctx, atx)
	if err != nil {
		return fmt.Errorf("broadcast: %w", err)
	}

	logger.Event().Info("atx published", log.Inline(atx), log.Int("size", size))

	select {
	case <-atxReceived:
		logger.With().Info(fmt.Sprintf("received atx in db %v", atx.ID().ShortString()), atx.ID())
	case <-b.layerClock.AwaitLayer((atx.TargetEpoch() + 1).FirstLayer()):
		select {
		case <-atxReceived:
			logger.With().Info(fmt.Sprintf("received atx in db %v (in the last moment)", atx.ID().ShortString()), atx.ID())
		case <-b.syncer.RegisterForATXSynced(): // ensure we've seen all ATXs before concluding that the ATX was lost
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

	hash, err := b.challenge.Hash()
	if err != nil {
		return nil, fmt.Errorf("getting challenge hash failed: %w", err)
	}

	pubEpoch := b.challenge.PublishEpoch()
	// Calculate deadline for waiting for poet proofs.
	// Deadline must fir between current poet round end and
	// start of the next. It must also accommodate for PoST duration.
	//                               PoST
	//                                ┌┐
	//         ┌─────────────────────┐││  ┌─────────────────────┐
	//         │     POET ROUND      │││  │     POET ROUND      │
	// ┌───────┴──────────────────┬──┴┴┴▲─┴─────────────────▲┬──┴───► time
	// │           EPOCH          │     │      EPOCH        ││
	// └──────────────────────────┴─────┼───────────────────┼┴──────
	//                                  │				    │
	//                              DEADLINE FOR	   ATX PUBLICATION
	//                            WAITING FOR POET       DEADLINE
	//                                PROOFS
	postDurationWithMargin := time.Duration(float64(b.lastPostGenDuration.Nanoseconds()) * 1.1)
	// Aim no later than the middle of the cycle gap
	if postDurationWithMargin < b.poetCfg.CycleGap/2 {
		postDurationWithMargin = b.poetCfg.CycleGap / 2
	}
	poetProofDeadline := b.layerClock.LayerToTime(b.challenge.PubLayerID).Add(b.poetCfg.PhaseShift).Add(-postDurationWithMargin)
	if time.Now().After(poetProofDeadline) {
		b.log.With().Error("Poet proof deadline is already up!")
	}

	b.log.With().Info("building NIPost",
		log.Stringer("pub_epoch", pubEpoch),
		log.Time("deadline time", poetProofDeadline),
		log.Duration("last PoST duration", postDurationWithMargin),
	)

	commitmentAtx, err := b.getCommitmentAtx(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting commitment atx failed: %w", err)
	}

	nipost, postDuration, err := b.nipostBuilder.BuildNIPost(ctx, hash, *commitmentAtx, poetProofDeadline)
	if err != nil {
		return nil, fmt.Errorf("failed to build NIPost: %w", err)
	}
	b.lastPostGenDuration = postDuration

	b.log.With().Info("awaiting atx publication epoch",
		log.FieldNamed("pub_epoch", pubEpoch),
		log.FieldNamed("pub_epoch_first_layer", pubEpoch.FirstLayer()),
		log.FieldNamed("current_layer", b.layerClock.GetCurrentLayer()),
	)
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("failed to wait for publication epoch: %w", err)
	case <-b.layerClock.AwaitLayer(pubEpoch.FirstLayer()):
	}
	b.log.Info("publication epoch has arrived!")
	if discarded := b.discardChallengeIfStale(); discarded {
		return nil, fmt.Errorf("%w: atx target epoch has passed during nipost construction", ErrATXChallengeExpired)
	}

	// when we reach here an epoch has passed
	// we've completed the sequential work, now before publishing the atx,
	// we need to provide number of atx seen in the epoch of the positioning atx.

	// ensure we are synced before generating the ATX's view
	select {
	case <-ctx.Done():
		return nil, ErrStopRequested
	case <-b.syncer.RegisterForATXSynced():
	}

	var initialPost *types.Post
	if b.challenge.PrevATXID == *types.EmptyATXID {
		initialPost = b.initialPost
	}

	atx := types.NewActivationTx(
		*b.challenge,
		b.Coinbase(),
		nipost,
		b.postSetupProvider.LastOpts().NumUnits,
		initialPost,
	)
	return atx, nil
}

func (b *Builder) currentEpoch() types.EpochID {
	return b.layerClock.GetCurrentLayer().GetEpoch()
}

func (b *Builder) discardChallenge() {
	b.challenge = nil
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
			b.log.With().Info("using golden atx as positioning atx", id)
			return b.goldenATXID, types.LayerID{}, nil
		}
		return types.ATXID{}, types.LayerID{}, fmt.Errorf("cannot find pos atx: %w", err)
	}
	atx, err := b.cdb.GetAtxHeader(id)
	if err != nil {
		return types.ATXID{}, types.LayerID{}, fmt.Errorf("inconsistent state: failed to get atx header: %w", err)
	}
	return id, atx.PubLayerID, nil
}

func (b *Builder) discardChallengeIfStale() bool {
	if b.challenge != nil && b.challenge.TargetEpoch() < b.currentEpoch() {
		b.log.With().Info("atx target epoch has already passed -- starting over",
			log.FieldNamed("target_epoch", b.challenge.TargetEpoch()),
			log.FieldNamed("current_epoch", b.currentEpoch()),
		)
		b.discardChallenge()
		return true
	}
	return false
}

// SignAtx signs the atx with specified signer and assigns the signature into atx.Sig
// this function returns an error if atx could not be converted to bytes.
func SignAtx(signer signer, atx *types.ActivationTx) error {
	bts, err := atx.InnerBytes()
	if err != nil {
		return fmt.Errorf("inner bytes of ATX: %w", err)
	}
	atx.Sig = signer.Sign(bts)
	return nil
}
