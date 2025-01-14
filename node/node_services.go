package node

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/beacon"
	"github.com/spacemeshos/go-spacemesh/blocks"
	"github.com/spacemeshos/go-spacemesh/bootstrap"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/fetch/peers"
	vm "github.com/spacemeshos/go-spacemesh/genvm"
	"github.com/spacemeshos/go-spacemesh/hare3"
	"github.com/spacemeshos/go-spacemesh/hare3/compat"
	"github.com/spacemeshos/go-spacemesh/hare3/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare4"
	"github.com/spacemeshos/go-spacemesh/layerpatrol"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/malfeasance"
	"github.com/spacemeshos/go-spacemesh/malfeasance2"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/proposals"
	"github.com/spacemeshos/go-spacemesh/proposals/store"
	"github.com/spacemeshos/go-spacemesh/prune"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/syncer"
	"github.com/spacemeshos/go-spacemesh/syncer/atxsync"
	"github.com/spacemeshos/go-spacemesh/syncer/blockssync"
	"github.com/spacemeshos/go-spacemesh/syncer/malsync"
	"github.com/spacemeshos/go-spacemesh/timesync/peersync"
	"github.com/spacemeshos/go-spacemesh/tortoise"
	"github.com/spacemeshos/go-spacemesh/txs"
)

type initState struct {
	poetDb           *activation.PoetDb
	postStates       *activation.PostStatesWrapper
	stateDb          *vm.VM
	goldenATXID      types.ATXID
	vrfVerifier      signing.VRFVerifier
	beaconProtocol   *beacon.ProtocolDriver
	trtl             *tortoise.Tortoise
	executor         *mesh.Executor
	mesh             *mesh.Mesh
	pruner           *prune.Pruner
	proposalsStore   *store.Store
	fetcher          *fetch.Fetch
	peerCache        *peers.Peers
	hareOracle       *eligibility.Oracle
	certifier        *blocks.Certifier
	patrol           *layerpatrol.LayerPatrol
	syncer           *syncer.Syncer
	atxHandler       *activation.Handler
	proposalsHandler *proposals.Handler
	proposalBuilder  *miner.ProposalBuilder
	postSetupMgr     *activation.PostSetupManager
	grpcPostService  *grpcserver.PostService
	poetClients      []activation.PoetService
	nipostBuilder    *activation.NIPostBuilder
	atxBuilder       *activation.Builder

	mlog                    *zap.Logger
	legacyMalfeasanceLogger *zap.Logger
	nipostLogger            *zap.Logger
}

type initializerFunc func(ctx context.Context, app *App, state *initState) error

func (app *App) initServices(ctx context.Context) error {
	var state initState
	state.peerCache = peers.New()
	state.patrol = layerpatrol.New()

	for _, initializer := range []initializerFunc{
		initPoetDb,
		initPostVerifier,
		initValidator,
		initStates,
		initGoldenATX,
		initVerifiers,
		initBeacon,
		initTortoise,
		initMesh,
		initPruner,
		initProposalsStore,
		initFetcher,
		initHareOracle,
		initCertifier,
		initSyncer,
		initATXHandler,
		initUpdater,
		initHare,
		initProposalsHandler,
		initBlocksGenerator,
		initProposalBuilder,
		initPostService,
		initPoetClients,
		initNIPostBuilder,
		initATXBuilder,
	} {
		if err := initializer(ctx, app, &state); err != nil {
			return err
		}
	}

	app.postSupervisor = activation.NewPostSupervisor(
		app.log.Zap(),
		app.Config.POST,
		app.Config.SMESHING.ProvingOpts,
		state.postSetupMgr,
		state.atxBuilder,
	)

	activationMH := activation.NewMalfeasanceHandler(
		app.cachedDB,
		state.legacyMalfeasanceLogger,
		app.edVerifier,
	)
	meshMH := mesh.NewMalfeasanceHandler(
		app.cachedDB,
		app.edVerifier,
		mesh.WithMalfeasanceLogger(state.legacyMalfeasanceLogger),
	)
	hareMH := hare3.NewMalfeasanceHandler(
		app.cachedDB,
		app.edVerifier,
		hare3.WithMalfeasanceLogger(state.legacyMalfeasanceLogger),
	)
	invalidPostMH := activation.NewInvalidPostIndexHandler(
		app.cachedDB,
		app.edVerifier,
		app.postVerifier,
	)
	invalidPrevMH := activation.NewInvalidPrevATXHandler(app.cachedDB, app.edVerifier)

	nodeIDs := make([]types.NodeID, 0, len(app.signers))
	for _, s := range app.signers {
		nodeIDs = append(nodeIDs, s.NodeID())
	}
	malHandler := malfeasance.NewHandler(
		app.cachedDB,
		state.legacyMalfeasanceLogger,
		app.host.ID(),
		nodeIDs,
		state.trtl,
	)
	malHandler.RegisterHandler(malfeasance.MultipleATXs, activationMH)
	malHandler.RegisterHandler(malfeasance.MultipleBallots, meshMH)
	malHandler.RegisterHandler(malfeasance.HareEquivocation, hareMH)
	malHandler.RegisterHandler(malfeasance.InvalidPostIndex, invalidPostMH)
	malHandler.RegisterHandler(malfeasance.InvalidPrevATX, invalidPrevMH)

	malfeasanceLogger := app.addLogger(Malfeasance2Logger, app.log).Zap()
	malHandler2 := malfeasance2.NewHandler(
		app.cachedDB,
		malfeasanceLogger,
		app.host.ID(),
		nodeIDs,
		app.edVerifier,
		state.trtl,
	)
	malHandler2.RegisterHandler(
		malfeasance2.InvalidActivation,
		activation.NewMalfeasanceHandlerV2())

	app.txHandler = txs.NewTxHandler(
		app.conState,
		app.host.ID(),
		app.addLogger(TxHandlerLogger, app.log).Zap(),
	)

	blockHandler := blocks.NewHandler(
		state.fetcher,
		app.db,
		state.trtl,
		state.mesh,
		blocks.WithLogger(app.addLogger(BlockHandlerLogger, app.log).Zap()),
	)

	state.fetcher.SetValidators(
		fetch.ValidatorFunc(
			pubsub.DropPeerOnSyncValidationReject(state.atxHandler.HandleSyncedAtx, app.host, app.log.Zap()),
		),
		fetch.ValidatorFunc(
			pubsub.DropPeerOnSyncValidationReject(state.poetDb.ValidateAndStoreMsg, app.host, app.log.Zap()),
		),
		fetch.ValidatorFunc(
			pubsub.DropPeerOnSyncValidationReject(
				state.proposalsHandler.HandleSyncedBallot,
				app.host,
				app.log.Zap(),
			),
		),
		fetch.ValidatorFunc(
			pubsub.DropPeerOnSyncValidationReject(state.proposalsHandler.HandleActiveSet, app.host, app.log.Zap()),
		),
		fetch.ValidatorFunc(
			pubsub.DropPeerOnSyncValidationReject(blockHandler.HandleSyncedBlock, app.host, app.log.Zap()),
		),
		fetch.ValidatorFunc(
			pubsub.DropPeerOnSyncValidationReject(
				state.proposalsHandler.HandleSyncedProposal,
				app.host,
				app.log.Zap(),
			),
		),
		fetch.ValidatorFunc(
			pubsub.DropPeerOnSyncValidationReject(
				app.txHandler.HandleBlockTransaction,
				app.host,
				app.log.Zap(),
			),
		),
		fetch.ValidatorFunc(
			pubsub.DropPeerOnSyncValidationReject(
				app.txHandler.HandleProposalTransaction,
				app.host,
				app.log.Zap(),
			),
		),
		fetch.ValidatorFunc(
			pubsub.DropPeerOnSyncValidationReject(
				malHandler.HandleSyncedMalfeasanceProof,
				app.host,
				app.log.Zap(),
			),
		),
	)

	checkSynced := func(_ context.Context, _ p2p.Peer, _ []byte) error {
		if state.syncer.ListenToGossip() {
			return nil
		}
		return errors.New("not synced for gossip")
	}
	checkAtxSynced := func(_ context.Context, _ p2p.Peer, _ []byte) error {
		if state.syncer.ListenToATXGossip() {
			return nil
		}
		return errors.New("not synced for gossip")
	}

	if app.Config.Beacon.RoundsNumber > 0 {
		app.host.Register(
			pubsub.BeaconWeakCoinProtocol,
			pubsub.ChainGossipHandler(checkSynced, state.beaconProtocol.HandleWeakCoinProposal),
			pubsub.WithValidatorInline(true),
		)
		app.host.Register(
			pubsub.BeaconProposalProtocol,
			pubsub.ChainGossipHandler(checkSynced, state.beaconProtocol.HandleProposal),
			pubsub.WithValidatorInline(true),
		)
		app.host.Register(
			pubsub.BeaconFirstVotesProtocol,
			pubsub.ChainGossipHandler(checkSynced, state.beaconProtocol.HandleFirstVotes),
			pubsub.WithValidatorInline(true),
		)
		app.host.Register(
			pubsub.BeaconFollowingVotesProtocol,
			pubsub.ChainGossipHandler(checkSynced, state.beaconProtocol.HandleFollowingVotes),
			pubsub.WithValidatorInline(true),
		)
	}
	app.host.Register(
		pubsub.ProposalProtocol,
		pubsub.ChainGossipHandler(checkSynced, state.proposalsHandler.HandleProposal),
	)
	app.host.Register(
		pubsub.AtxProtocol,
		pubsub.ChainGossipHandler(checkAtxSynced, state.atxHandler.HandleGossipAtx),
		pubsub.WithValidatorConcurrency(app.Config.P2P.GossipAtxValidationThrottle),
	)
	app.host.Register(
		pubsub.TxProtocol,
		pubsub.ChainGossipHandler(checkSynced, app.txHandler.HandleGossipTransaction),
	)
	app.host.Register(
		pubsub.BlockCertify,
		pubsub.ChainGossipHandler(checkSynced, state.certifier.HandleCertifyMessage),
	)
	app.host.Register(
		pubsub.MalfeasanceProof,
		pubsub.ChainGossipHandler(checkAtxSynced, malHandler.HandleMalfeasanceProof),
	)

	app.proposalBuilder = state.proposalBuilder
	app.mesh = state.mesh
	app.syncer = state.syncer
	app.atxBuilder = state.atxBuilder
	app.atxHandler = state.atxHandler
	app.malfeasanceHandler = malHandler
	app.malfeasance2Handler = malHandler2
	app.poetDb = state.poetDb
	app.fetcher = state.fetcher
	app.beaconProtocol = state.beaconProtocol
	app.hOracle = state.hareOracle
	app.certifier = state.certifier
	if !app.Config.TIME.Peersync.Disable {
		app.ptimesync = peersync.New(
			app.host,
			app.host,
			peersync.WithLog(app.addLogger(TimeSyncLogger, app.log).Zap()),
			peersync.WithConfig(app.Config.TIME.Peersync),
		)
	}
	if err := app.host.Start(); err != nil {
		return err
	}
	return nil
}

func initPoetDb(_ context.Context, app *App, state *initState) error {
	var err error
	state.poetDb, err = activation.NewPoetDb(
		app.db,
		app.addLogger(PoetDbLogger, app.log).Zap(),
		activation.WithCacheSize(app.Config.POET.PoetProofsCache),
	)
	if err != nil {
		return fmt.Errorf("creating poet db: %w", err)
	}
	return nil
}

func initPostVerifier(_ context.Context, app *App, state *initState) error {
	state.postStates = activation.NewPostStates(app.addLogger(PostLogger, app.log).Zap())

	opts := []activation.PostVerifierOpt{
		activation.WithVerifyingOpts(app.Config.SMESHING.VerifyingOpts),
		activation.WithAutoscaling(state.postStates),
	}
	for _, sig := range app.signers {
		opts = append(opts, activation.WithPrioritizedID(sig.NodeID()))
	}

	verifier, err := activation.NewPostVerifier(
		app.Config.POST,
		app.addLogger(NipostValidatorLogger, app.log).Zap(),
		opts...,
	)
	if err != nil {
		return fmt.Errorf("creating post verifier: %w", err)
	}
	app.postVerifier = verifier
	return nil
}

func initValidator(_ context.Context, app *App, state *initState) error {
	app.validator = activation.NewValidator(
		app.db,
		state.poetDb,
		app.Config.POST,
		app.Config.SMESHING.Opts.Scrypt,
		app.postVerifier,
	)
	return nil
}

func initStates(_ context.Context, app *App, state *initState) error {
	cfg := vm.DefaultConfig()
	cfg.GasLimit = app.Config.BlockGasLimit
	cfg.GenesisID = app.Config.Genesis.GenesisID()
	state.stateDb = vm.New(app.db,
		vm.WithConfig(cfg),
		vm.WithLogger(app.addLogger(VMLogger, app.log).Zap()))
	app.conState = txs.NewConservativeState(state.stateDb, app.db,
		txs.WithCSConfig(txs.CSConfig{
			BlockGasLimit:     app.Config.BlockGasLimit,
			NumTXsPerProposal: app.Config.TxsPerProposal,
		}),
		txs.WithLogger(app.addLogger(ConStateLogger, app.log).Zap()))

	genesisAccts := app.Config.Genesis.ToAccounts()
	if len(genesisAccts) > 0 {
		exists, err := state.stateDb.AccountExists(genesisAccts[0].Address)
		if err != nil {
			return fmt.Errorf(
				"failed to check genesis account %v: %w",
				genesisAccts[0].Address,
				err,
			)
		}
		if !exists {
			if err = state.stateDb.ApplyGenesis(genesisAccts); err != nil {
				return fmt.Errorf("setup genesis: %w", err)
			}
		}
	}

	return nil
}

func initGoldenATX(_ context.Context, app *App, state *initState) error {
	state.goldenATXID = types.ATXID(app.Config.Genesis.GoldenATX())
	if state.goldenATXID == types.EmptyATXID {
		return errors.New("invalid golden atx id")
	}

	return nil
}

func initVerifiers(_ context.Context, app *App, state *initState) error {
	app.edVerifier = signing.NewEdVerifier(
		signing.WithVerifierPrefix(app.Config.Genesis.GenesisID().Bytes()),
	)

	state.vrfVerifier = signing.NewVRFVerifier()

	return nil
}

func initBeacon(_ context.Context, app *App, state *initState) error {
	state.beaconProtocol = beacon.New(
		app.host,
		app.edVerifier,
		state.vrfVerifier,
		app.cachedDB,
		app.clock,
		beacon.WithConfig(app.Config.Beacon),
		beacon.WithLogger(app.addLogger(BeaconLogger, app.log).Zap()),
	)
	for _, sig := range app.signers {
		state.beaconProtocol.Register(sig)
	}

	return nil
}

func initTortoise(ctx context.Context, app *App, state *initState) error {
	var err error
	trtlCfg := app.Config.Tortoise
	trtlCfg.LayerSize = app.Config.LayerAvgSize
	if trtlCfg.BadBeaconVoteDelayLayers == 0 {
		trtlCfg.BadBeaconVoteDelayLayers = app.Config.LayersPerEpoch
	}
	trtlopts := []tortoise.Opt{
		tortoise.WithLogger(app.addLogger(TrtlLogger, app.log).Zap()),
		tortoise.WithConfig(trtlCfg),
	}
	if trtlCfg.EnableTracer {
		app.log.With().Info("tortoise will trace execution")
		trtlopts = append(trtlopts, tortoise.WithTracer())
	}
	app.log.Info("initializing tortoise")
	start := time.Now()
	state.trtl, err = tortoise.Recover(
		ctx,
		app.db,
		app.atxsdata,
		app.clock.CurrentLayer(), trtlopts...,
	)
	if err != nil {
		return fmt.Errorf("can't recover tortoise state: %w", err)
	}
	app.log.With().Info("tortoise initialized", log.Duration("duration", time.Since(start)))
	app.eg.Go(func() error {
		for rst := range state.beaconProtocol.Results() {
			events.EmitBeacon(rst.Epoch, rst.Beacon)
			state.trtl.OnBeacon(rst.Epoch, rst.Beacon)
		}
		app.log.Debug("beacon results watcher exited")
		return nil
	})

	return nil
}

func initMesh(ctx context.Context, app *App, state *initState) error {
	var err error
	state.executor = mesh.NewExecutor(
		app.db,
		app.atxsdata,
		state.stateDb,
		app.conState,
		app.addLogger(ExecutorLogger, app.log).Zap(),
	)
	state.mlog = app.addLogger(MeshLogger, app.log).Zap()
	state.mesh, err = mesh.NewMesh(
		app.db, app.atxsdata, state.trtl, state.executor,
		app.conState, state.mlog)
	if err != nil {
		return fmt.Errorf("create mesh: %w", err)
	}

	app.eg.Go(func() error {
		state.mesh.Start(ctx)
		return nil
	})

	return nil
}

func initPruner(ctx context.Context, app *App, state *initState) error {
	state.pruner = prune.New(
		app.db,
		app.Config.Tortoise.Hdist,
		app.Config.PruneActivesetsFrom,
		prune.WithLogger(state.mlog),
	)
	if err := state.pruner.Prune(app.clock.CurrentLayer()); err != nil {
		return fmt.Errorf("pruner %w", err)
	}
	app.eg.Go(func() error {
		prune.Run(ctx, state.pruner, app.clock, app.Config.DatabasePruneInterval)
		return nil
	})

	return nil
}

func initProposalsStore(_ context.Context, app *App, state *initState) error {
	state.proposalsStore = store.New(
		store.WithEvictedLayer(app.clock.CurrentLayer()),
		store.WithLogger(app.addLogger(ProposalStoreLogger, app.log).Zap()),
		store.WithCapacity(app.Config.Tortoise.Zdist+1),
	)

	return nil
}

func initFetcher(ctx context.Context, app *App, state *initState) error {
	flog := app.addLogger(Fetcher, app.log).Zap()
	fetcher, err := fetch.NewFetch(
		app.cachedDB,
		state.proposalsStore,
		app.host,
		state.peerCache,
		fetch.WithContext(ctx),
		fetch.WithConfig(app.Config.FETCH),
		fetch.WithLogger(flog),
	)
	if err != nil {
		return fmt.Errorf("create fetcher: %w", err)
	}
	app.eg.Go(func() error {
		return blockssync.Sync(ctx, flog, state.mesh.MissingBlocks(), fetcher)
	})

	return nil
}

func initHareOracle(_ context.Context, app *App, state *initState) error {
	var err error
	state.hareOracle, err = eligibility.New(
		state.beaconProtocol,
		app.db,
		app.atxsdata,
		state.vrfVerifier,
		app.Config.LayersPerEpoch,
		eligibility.WithConfig(app.Config.HareEligibility),
		eligibility.WithLogger(app.addLogger(HareOracleLogger, app.log).Zap()),
	)
	if err != nil {
		return fmt.Errorf("create hare oracle: %w", err)
	}

	return nil
}

func initCertifier(_ context.Context, app *App, state *initState) error {
	if app.Config.Certificate.CommitteeSize == 0 {
		app.log.With().Debug("certificate committee size is not set, defaulting to hare committee size",
			log.Uint16("size", app.Config.HARE3.Committee),
		)
		app.Config.Certificate.CommitteeSize = int(app.Config.HARE3.Committee)
	}
	app.Config.Certificate.CertifyThreshold = app.Config.Certificate.CommitteeSize/2 + 1
	app.Config.Certificate.LayerBuffer = app.Config.Tortoise.Zdist
	app.Config.Certificate.NumLayersToKeep = app.Config.Tortoise.Zdist * 2
	state.certifier = blocks.NewCertifier(
		app.db,
		state.hareOracle,
		app.edVerifier,
		app.host,
		app.clock,
		state.beaconProtocol,
		state.trtl,
		blocks.WithCertConfig(app.Config.Certificate),
		blocks.WithCertifierLogger(app.addLogger(BlockCertLogger, app.log).Zap()),
	)
	for _, sig := range app.signers {
		state.certifier.Register(sig)
	}

	return nil
}

func initSyncer(_ context.Context, app *App, state *initState) error {
	var err error
	syncerConf := app.Config.Sync
	syncerConf.HareDelayLayers = app.Config.Tortoise.Zdist
	syncerConf.SyncCertDistance = app.Config.Tortoise.Hdist
	syncerConf.Standalone = app.Config.Standalone

	if app.Config.P2P.MinPeers < app.Config.Sync.MalSync.MinSyncPeers {
		app.Config.Sync.MalSync.MinSyncPeers = max(1, app.Config.P2P.MinPeers)
	}
	app.syncLogger = app.addLogger(SyncLogger, app.log)
	state.syncer, err = syncer.NewSyncer(
		app.cachedDB,
		app.clock,
		state.mesh,
		state.trtl,
		state.fetcher,
		state.peerCache,
		app.host,
		state.patrol,
		state.certifier,
		atxsync.New(state.fetcher, app.db, app.localDB,
			atxsync.WithConfig(app.Config.Sync.AtxSync),
			atxsync.WithLogger(app.syncLogger.Zap()),
		),
		malsync.New(state.fetcher, app.db, app.localDB,
			malsync.WithConfig(app.Config.Sync.MalSync),
			malsync.WithLogger(app.syncLogger.Zap()),
			malsync.WithPeerErrMetric(syncer.MalPeerError),
		),
		syncer.WithConfig(syncerConf),
		syncer.WithLogger(app.syncLogger.Zap()),
	)
	if err != nil {
		return fmt.Errorf("create syncer: %w", err)
	}
	// TODO(dshulyak) this needs to be improved, but dependency graph is a bit complicated
	state.beaconProtocol.SetSyncState(state.syncer)
	state.hareOracle.SetSync(state.syncer)

	return nil
}

func initATXHandler(_ context.Context, app *App, state *initState) error {
	legacyMalfeasanceLogger := app.addLogger(MalfeasanceLogger, app.log).Zap()
	legacyMalPublisher := malfeasance.NewPublisher(
		legacyMalfeasanceLogger,
		app.cachedDB,
		state.syncer,
		state.trtl,
		app.host,
	)

	state.atxHandler = activation.NewHandler(
		app.host.ID(),
		app.cachedDB,
		app.atxsdata,
		app.edVerifier,
		app.clock,
		state.fetcher,
		state.goldenATXID,
		app.validator,
		legacyMalPublisher,
		state.beaconProtocol,
		state.trtl,
		app.addLogger(ATXHandlerLogger, app.log).Zap(),
		activation.WithTickSize(app.Config.TickSize),
		activation.WithAtxVersions(app.Config.AtxVersions),
	)
	for _, sig := range app.signers {
		state.atxHandler.Register(sig)
	}

	return nil
}

func initUpdater(_ context.Context, app *App, _ *initState) error {
	bscfg := app.Config.Bootstrap
	bscfg.DataDir = app.Config.DataDir()
	bscfg.Interval = app.Config.LayerDuration / 5
	app.updater = bootstrap.New(
		app.clock,
		bootstrap.WithConfig(bscfg),
		bootstrap.WithLogger(app.addLogger(BootstrapLogger, app.log).Zap()),
	)

	return nil
}

func initHare(ctx context.Context, app *App, state *initState) error {
	err := app.Config.HARE3.Validate(time.Duration(app.Config.Tortoise.Zdist) * app.Config.LayerDuration)
	if err != nil {
		return err
	}
	logger := app.addLogger(HareLogger, app.log).Zap()

	// should be removed after hare4 transition is complete
	app.hareResultsChan = make(chan hare4.ConsensusOutput, 32)
	if app.Config.HARE3.Enable {
		app.hare3 = hare3.New(
			app.clock,
			app.host,
			app.db,
			app.atxsdata,
			state.proposalsStore,
			app.edVerifier,
			state.hareOracle,
			state.syncer,
			state.patrol,
			hare3.WithLogger(logger),
			hare3.WithConfig(app.Config.HARE3),
			hare3.WithResultsChan(app.hareResultsChan),
		)
		for _, sig := range app.signers {
			app.hare3.Register(sig)
		}
		app.hare3.Start()
		app.eg.Go(func() error {
			compat.ReportWeakcoin(
				ctx,
				logger,
				app.hare3.Coins(),
				tortoiseWeakCoin{db: app.cachedDB, tortoise: state.trtl},
			)
			return nil
		})
	}

	if app.Config.HARE4.Enable {
		app.hare4 = hare4.New(
			app.clock,
			app.host,
			app.db,
			app.atxsdata,
			state.proposalsStore,
			app.edVerifier,
			state.hareOracle,
			state.syncer,
			state.patrol,
			app.host,
			hare4.WithLogger(logger),
			hare4.WithConfig(app.Config.HARE4),
			hare4.WithResultsChan(app.hareResultsChan),
		)
		for _, sig := range app.signers {
			app.hare4.Register(sig)
		}
		app.hare4.Start()
		app.eg.Go(func() error {
			compat.ReportWeakcoin(
				ctx,
				logger,
				app.hare4.Coins(),
				tortoiseWeakCoin{db: app.cachedDB, tortoise: state.trtl},
			)
			return nil
		})
	}

	return nil
}

func initProposalsHandler(_ context.Context, app *App, state *initState) error {
	propHare := &proposalConsumerHare{
		hare3:          app.hare3,
		h3DisableLayer: app.Config.HARE3.DisableLayer,
		hare4:          app.hare4,
	}

	state.proposalsHandler = proposals.NewHandler(
		app.db,
		app.atxsdata,
		propHare,
		app.edVerifier,
		app.host,
		state.fetcher,
		state.beaconProtocol,
		state.mesh,
		state.trtl,
		state.vrfVerifier,
		app.clock,
		proposals.WithLogger(app.addLogger(ProposalListenerLogger, app.log).Zap()),
		proposals.WithConfig(proposals.Config{
			LayerSize:              app.Config.LayerAvgSize,
			LayersPerEpoch:         types.GetLayersPerEpoch(),
			GoldenATXID:            state.goldenATXID,
			MaxExceptions:          app.Config.Tortoise.MaxExceptions,
			Hdist:                  app.Config.Tortoise.Hdist,
			MinimalActiveSetWeight: app.Config.Tortoise.MinimalActiveSetWeight,
		}),
	)

	return nil
}

func initBlocksGenerator(_ context.Context, app *App, state *initState) error {
	app.blockGen = blocks.NewGenerator(
		app.db,
		app.atxsdata,
		state.proposalsStore,
		state.executor,
		state.mesh,
		state.fetcher,
		state.certifier,
		state.patrol,
		blocks.WithConfig(blocks.Config{
			BlockGasLimit:      app.Config.BlockGasLimit,
			OptFilterThreshold: app.Config.OptFilterThreshold,
			GenBlockInterval:   500 * time.Millisecond,
		}),
		blocks.WithHareOutputChan(app.hareResultsChan),
		blocks.WithGeneratorLogger(app.addLogger(BlockGenLogger, app.log).Zap()),
	)

	return nil
}

func initProposalBuilder(_ context.Context, app *App, state *initState) error {
	minerGoodAtxPct := 90
	if app.Config.MinerGoodAtxsPercent > 0 {
		minerGoodAtxPct = app.Config.MinerGoodAtxsPercent
	}

	state.proposalBuilder = miner.New(
		app.clock,
		app.db,
		app.localDB,
		app.atxsdata,
		app.host,
		state.trtl,
		state.syncer,
		app.conState,
		miner.WithLayerSize(app.Config.LayerAvgSize),
		miner.WithLayerPerEpoch(types.GetLayersPerEpoch()),
		miner.WithMinimalActiveSetWeight(app.Config.Tortoise.MinimalActiveSetWeight),
		miner.WithHdist(app.Config.Tortoise.Hdist),
		miner.WithNetworkDelay(app.Config.ATXGradeDelay),
		miner.WithMinGoodAtxPercent(minerGoodAtxPct),
		miner.WithLogger(app.addLogger(ProposalBuilderLogger, app.log).Zap()),
		miner.WithActivesetPreparation(app.Config.ActiveSet),
	)
	for _, sig := range app.signers {
		state.proposalBuilder.Register(sig)
	}

	return nil
}

func initPostService(_ context.Context, app *App, state *initState) error {
	var err error
	state.postSetupMgr, err = activation.NewPostSetupManager(
		app.Config.POST,
		app.addLogger(PostLogger, app.log).Zap(),
		app.db,
		app.atxsdata,
		state.goldenATXID,
		state.syncer,
		app.validator,
		activation.PostValidityDelay(app.Config.PostValidDelay),
	)
	if err != nil {
		return fmt.Errorf("create post setup manager: %v", err)
	}

	grpcPostService, err := app.grpcService(grpcserver.Post, app.log)
	if err != nil {
		return fmt.Errorf("init post grpc service: %w", err)
	}
	state.grpcPostService = grpcPostService.(*grpcserver.PostService)

	return nil
}

func initPoetClients(_ context.Context, app *App, state *initState) error {
	state.nipostLogger = app.addLogger(NipostBuilderLogger, app.log).Zap()
	client := activation.NewCertifierClient(
		app.db,
		app.localDB,
		state.nipostLogger,
		activation.WithCertifierClientConfig(app.Config.Certifier.Client),
	)
	poetCertifier := activation.NewCertifier(app.localDB, state.nipostLogger, client)

	state.poetClients = make([]activation.PoetService, 0, len(app.Config.PoetServers))
	for _, server := range app.Config.PoetServers {
		client, err := activation.NewPoetService(
			state.poetDb,
			server,
			app.Config.POET,
			app.log.Zap().Named("poet"),
			app.Config.TickSize,
			activation.WithCertifier(poetCertifier),
		)
		if err != nil {
			app.log.Panic("failed to create poet client with address %v: %v", server.Address, err)
		}
		state.poetClients = append(state.poetClients, client)
	}

	return nil
}

func initNIPostBuilder(_ context.Context, app *App, state *initState) error {
	var err error
	state.nipostBuilder, err = activation.NewNIPostBuilder(
		app.localDB,
		state.grpcPostService,
		state.nipostLogger,
		app.Config.POET,
		app.clock,
		app.validator,
		activation.NipostbuilderWithPostStates(state.postStates),
		activation.WithPoetServices(state.poetClients...),
	)
	if err != nil {
		return fmt.Errorf("create nipost builder: %w", err)
	}

	return nil
}

func initATXBuilder(ctx context.Context, app *App, state *initState) error {
	builderConfig := activation.Config{
		GoldenATXID:      state.goldenATXID,
		RegossipInterval: app.Config.RegossipAtxInterval,
	}
	state.atxBuilder = activation.NewBuilder(
		builderConfig,
		app.db,
		app.atxsdata,
		app.localDB,
		app.host,
		state.nipostBuilder,
		app.clock,
		state.syncer,
		app.addLogger(ATXBuilderLogger, app.log).Zap(),
		activation.WithContext(ctx),
		activation.WithPoetConfig(app.Config.POET),
		// TODO(dshulyak) makes no sense. how we ended using it?
		activation.WithPoetRetryInterval(app.Config.HARE3.PreroundDelay),
		activation.WithValidator(app.validator),
		activation.WithPostValidityDelay(app.Config.PostValidDelay),
		activation.WithPostStates(state.postStates),
		activation.WithPoets(state.poetClients...),
		activation.BuilderAtxVersions(app.Config.AtxVersions),
	)
	if len(app.signers) > 1 || app.signers[0].Name() != supervisedIDKeyFileName {
		// in a remote setup we register eagerly so the atxBuilder can warn about missing connections asap.
		// Any setup with more than one signer is considered a remote setup. If there is only one signer it
		// is considered a remote setup if the key for the signer has not been sourced from `supervisedIDKeyFileName`.
		//
		// In a supervised setup the postSetupManager will register at the atxBuilder when
		// it finished initializing, to avoid warning about a missing connection when the supervised post
		// service isn't ready yet.
		for _, sig := range app.signers {
			state.atxBuilder.Register(sig)
		}
	}

	return nil
}
