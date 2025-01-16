// Package node contains the main executable for go-spacemesh node
package node

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	pyroscope "github.com/grafana/pyroscope-go"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/api/node/client"
	nodeclient "github.com/spacemeshos/go-spacemesh/api/node/client"
	"github.com/spacemeshos/go-spacemesh/beacon"
	"github.com/spacemeshos/go-spacemesh/checkpoint"
	"github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/hare3"
	"github.com/spacemeshos/go-spacemesh/hare3/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare4"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/identity"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/metrics"
	"github.com/spacemeshos/go-spacemesh/metrics/public"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/syncer"
	"github.com/spacemeshos/go-spacemesh/timesync"
	timeCfg "github.com/spacemeshos/go-spacemesh/timesync/config"
	"github.com/spacemeshos/go-spacemesh/tortoise"
)

func GetActivationServiceCommand() *cobra.Command {
	conf := config.MainnetConfig()
	var configPath *string
	c := &cobra.Command{
		Use:   "activation",
		Short: "Start activation service",
		RunE: func(c *cobra.Command, args []string) error {
			if err := configure(c, *configPath, &conf); err != nil {
				return err
			}

			// NOTE(dshulyak) this needs to be max level so that child logger can can be current level or below.
			// otherwise it will fail later when child logger will try to increase level.
			encoder := zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())
			if conf.LOGGING.Encoder == config.JSONLogEncoder {
				encoder = zapcore.NewJSONEncoder(zap.NewDevelopmentEncoderConfig())
			}
			lg := log.NewWithLevel("node", zap.NewAtomicLevelAt(zap.DebugLevel), encoder, events.EventHook())

			app := New(WithConfig(&conf), WithLog(lg))

			// os.Interrupt for all systems, especially windows, syscall.SIGTERM is mainly for docker.
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			types.SetLayersPerEpoch(app.Config.LayersPerEpoch)
			// ensure all data folders exist
			if err := os.MkdirAll(app.Config.DataDir(), 0o700); err != nil {
				return fmt.Errorf("ensure folders exist: %w", err)
			}

			if err := app.Lock(); err != nil {
				return fmt.Errorf("getting exclusive file lock: %w", err)
			}
			defer app.Unlock()

			if err := app.Initialize(); err != nil {
				return fmt.Errorf("initializing app: %w", err)
			}

			err := app.LoadIdentities()
			switch {
			case errors.Is(err, fs.ErrNotExist):
				app.log.Info("Identity file not found. Creating new identity...")
				if err := app.NewIdentity(); err != nil {
					return fmt.Errorf("creating new identity: %w", err)
				}
			case err != nil:
				return fmt.Errorf("loading identities: %w", err)
			}

			// Don't print usage on error from this point forward
			c.SilenceUsage = true

			// This blocks until the context is finished or until an error is produced
			err = app.StartActivationService(ctx)
			cleanupCtx, cleanupCancel := context.WithTimeout(
				context.Background(),
				30*time.Second,
			)
			defer cleanupCancel()
			done := make(chan struct{}, 1)
			// FIXME: per https://github.com/spacemeshos/go-spacemesh/issues/3830
			go func() {
				app.Cleanup(cleanupCtx)
				close(done)
			}()
			select {
			case <-done:
			case <-cleanupCtx.Done():
				app.log.Error("app failed to clean up in time")
			}
			return err
		},
	}

	configPath = cmd.AddFlags(c.PersistentFlags(), &conf)

	// versionCmd returns the current version of spacemesh.
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Show version info",
		Run: func(c *cobra.Command, args []string) {
			fmt.Print(cmd.Version)
			fmt.Println()
		},
	}
	c.AddCommand(versionCmd)

	relayCmd := cobra.Command{
		Use:          "relay",
		Short:        "Run relay server",
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			if err := configure(c, *configPath, &conf); err != nil {
				return err
			}
			return runRelay(c.Context(), &conf)
		},
	}
	c.AddCommand(&relayCmd)

	return c
}

// Initialize parses and validates the node configuration and sets up logging.
func (app *App) InitializeActivationService() error {
	gpath := filepath.Join(app.Config.DataDir(), genesisFileName)
	var existing config.GenesisConfig
	if err := existing.LoadFromFile(gpath); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("failed to load genesis config at %s: %w", gpath, err)
		}
		if err := app.Config.Genesis.Validate(); err != nil {
			return err
		}
		if err := app.Config.Genesis.WriteToFile(gpath); err != nil {
			return fmt.Errorf("failed to write genesis config to %s: %w", gpath, err)
		}
	} else {
		diff := existing.Diff(&app.Config.Genesis)
		if len(diff) > 0 {
			app.log.Error("genesis config updated after node initialization, if this update is required delete config"+
				" at %s.\ndiff:\n%s", gpath, diff,
			)
			return errors.New("genesis config updated after node initialization")
		}
	}

	// override default config in timesync since timesync is using TimeConfigValues
	timeCfg.TimeConfigValues = app.Config.TIME

	app.setupLogging()
	app.log.Info("Welcome to Spacemesh. Spacemesh activation service is starting...")

	public.Version.WithLabelValues(cmd.Version).Set(1)
	public.SmeshingOptsProvingNonces.Set(float64(app.Config.SMESHING.ProvingOpts.Nonces))
	public.SmeshingOptsProvingThreads.Set(float64(app.Config.SMESHING.ProvingOpts.Threads))
	return nil
}

func (app *App) initActivationServiceServices(ctx context.Context) error {
	layerSize := app.Config.LayerAvgSize
	layersPerEpoch := types.GetLayersPerEpoch()
	lg := app.log

	var nodeServiceClient *client.NodeService
	listenAddress := app.Config.BaseConfig.NodeServiceAddress
	logger := app.addLogger(NodeServiceClientLogger, lg).Zap()
	cfg := &nodeclient.Config{
		RetryWaitMin: time.Millisecond * 500,
		RetryWaitMax: time.Second,
		RetryMax:     10,
	}
	var err error
	nodeServiceClient, err = nodeclient.NewNodeServiceClient(listenAddress, logger, cfg)
	if err != nil {
		return fmt.Errorf("creating node service client: %w", err)
	}

	poetDb, err := activation.NewPoetDb(
		app.db,
		app.addLogger(PoetDbLogger, lg).Zap(),
		activation.WithCacheSize(app.Config.POET.PoetProofsCache),
		activation.WithRemotePoetStorer(nodeServiceClient),
	)
	if err != nil {
		return fmt.Errorf("creating poet db: %w", err)
	}
	postStates := activation.NewPostStates(app.addLogger(PostLogger, lg).Zap())

	app.idStates = identity.NewIdentityStateStorage(app.localDB, app.log.Zap())

	opts := []activation.PostVerifierOpt{
		activation.WithVerifyingOpts(app.Config.SMESHING.VerifyingOpts),
		activation.WithAutoscaling(postStates),
	}
	for _, sig := range app.signers {
		opts = append(opts, activation.WithPrioritizedID(sig.NodeID()))
	}

	verifier, err := activation.NewPostVerifier(
		app.Config.POST,
		app.addLogger(NipostValidatorLogger, lg).Zap(),
		opts...,
	)
	if err != nil {
		return fmt.Errorf("creating post verifier: %w", err)
	}
	app.postVerifier = verifier

	validator := activation.NewValidator(
		app.db,
		poetDb,
		app.Config.POST,
		app.Config.SMESHING.Opts.Scrypt,
		app.postVerifier,
	)
	app.validator = validator

	goldenATXID := types.ATXID(app.Config.Genesis.GoldenATX())
	if goldenATXID == types.EmptyATXID {
		return errors.New("invalid golden atx id")
	}

	app.edVerifier = signing.NewEdVerifier(
		signing.WithVerifierPrefix(app.Config.Genesis.GenesisID().Bytes()),
	)

	vrfVerifier := signing.NewVRFVerifier()
	var beaconProtocol *beacon.ProtocolDriver

	trtlCfg := app.Config.Tortoise
	trtlCfg.LayerSize = layerSize
	if trtlCfg.BadBeaconVoteDelayLayers == 0 {
		trtlCfg.BadBeaconVoteDelayLayers = app.Config.LayersPerEpoch
	}
	trtlopts := []tortoise.Opt{
		tortoise.WithLogger(app.addLogger(TrtlLogger, lg).Zap()),
		tortoise.WithConfig(trtlCfg),
	}
	if trtlCfg.EnableTracer {
		app.log.With().Info("tortoise will trace execution")
		trtlopts = append(trtlopts, tortoise.WithTracer())
	}
	app.log.Info("initializing tortoise")
	start := time.Now()
	trtl, err := tortoise.Recover(
		ctx,
		app.db,
		app.atxsdata,
		app.clock.CurrentLayer(), trtlopts...,
	)
	if err != nil {
		return fmt.Errorf("can't recover tortoise state: %w", err)
	}
	app.log.With().Info("tortoise initialized", log.Duration("duration", time.Since(start)))
	if nodeServiceClient == nil {
		app.eg.Go(func() error {
			for rst := range beaconProtocol.Results() {
				events.EmitBeacon(rst.Epoch, rst.Beacon)
				trtl.OnBeacon(rst.Epoch, rst.Beacon)
			}
			app.log.Debug("beacon results watcher exited")
			return nil
		})
	}

	var msh *mesh.Mesh
	var atxHandler *activation.Handler

	// we can't have an epoch offset which is greater/equal than the number of layers in an epoch

	if app.Config.HareEligibility.ConfidenceParam >= app.Config.BaseConfig.LayersPerEpoch {
		return fmt.Errorf(
			"confidence param should be smaller than layers per epoch. eligibility-confidence-param: %d. "+
				"layers-per-epoch: %d",
			app.Config.HareEligibility.ConfidenceParam,
			app.Config.BaseConfig.LayersPerEpoch,
		)
	}

	hOracle, err := eligibility.New(
		nodeServiceClient,
		app.db,
		app.atxsdata,
		vrfVerifier,
		app.Config.LayersPerEpoch,
		eligibility.WithConfig(app.Config.HareEligibility),
		eligibility.WithLogger(app.addLogger(HareOracleLogger, lg).Zap()),
		eligibility.WithTotalWeightFunc(nodeServiceClient.TotalWeight),
		eligibility.WithMinerWeightFunc(nodeServiceClient.MinerWeight),
	)
	if err != nil {
		return fmt.Errorf("create hare oracle: %w", err)
	}

	var fetcher *fetch.Fetch
	var newSyncer *syncer.Syncer

	err = app.Config.HARE3.Validate(time.Duration(app.Config.Tortoise.Zdist) * app.Config.LayerDuration)
	if err != nil {
		return err
	}
	logger = app.addLogger(HareLogger, lg).Zap()

	// should be removed after hare4 transition is complete
	app.hareResultsChan = make(chan hare4.ConsensusOutput, 32)
	app.remoteHare = hare3.NewRemoteHare(
		app.Config.HARE3,
		app.clock,
		nodeServiceClient,
		hOracle,
		logger,
	)
	for _, sig := range app.signers {
		app.remoteHare.Register(sig)
	}
	app.remoteHare.Start(ctx)

	remoteProposalBuilder := miner.NewRemoteBuilder(
		app.clock,
		nodeServiceClient,
		nodeServiceClient,
		nodeServiceClient,
		layerSize,
		layersPerEpoch,
		app.addLogger(ProposalBuilderLogger, lg).Zap(),
		app.idStates,
	)
	for _, sig := range app.signers {
		remoteProposalBuilder.Register(sig)
	}
	app.remoteProposalBuilder = remoteProposalBuilder

	postSetupMgr, err := activation.NewPostSetupManager(
		app.Config.POST,
		app.addLogger(PostLogger, lg).Zap(),
		app.db,
		app.atxsdata,
		goldenATXID,
		newSyncer,
		app.validator,
		activation.PostValidityDelay(app.Config.PostValidDelay),
	)
	if err != nil {
		return fmt.Errorf("create post setup manager: %v", err)
	}

	grpcPostService, err := app.grpcService(grpcserver.Post, lg)
	if err != nil {
		return fmt.Errorf("init post grpc service: %w", err)
	}

	nipostLogger := app.addLogger(NipostBuilderLogger, lg).Zap()
	client := activation.NewCertifierClient(
		app.db,
		app.localDB,
		nipostLogger,
		activation.WithCertifierClientConfig(app.Config.Certifier.Client),
	)
	poetCertifier := activation.NewCertifier(app.localDB, nipostLogger, client)

	poetClients := make([]activation.PoetService, 0, len(app.Config.PoetServers))
	for _, server := range app.Config.PoetServers {
		client, err := activation.NewPoetService(
			poetDb,
			server,
			app.Config.POET,
			lg.Zap().Named("poet"),
			app.Config.TickSize,
			activation.WithCertifier(poetCertifier),
		)
		if err != nil {
			app.log.Panic("failed to create poet client with address %v: %v", server.Address, err)
		}
		poetClients = append(poetClients, client)
	}
	app.poetClients = poetClients

	nipostBuilder, err := activation.NewNIPostBuilder(
		app.localDB,
		grpcPostService.(*grpcserver.PostService),
		nipostLogger,
		app.Config.POET,
		app.clock,
		app.validator,
		activation.NipostbuilderWithPostStates(postStates),
		activation.NipostbuilderWithIdentityStates(app.idStates),
		activation.WithPoetServices(poetClients...),
	)
	if err != nil {
		return fmt.Errorf("create nipost builder: %w", err)
	}

	builderConfig := activation.Config{
		GoldenATXID:      goldenATXID,
		RegossipInterval: app.Config.RegossipAtxInterval,
	}

	var (
		atxBuilderLog = app.addLogger(ATXBuilderLogger, lg).Zap()
		syncer        activation.Syncer
	)
	atxService := nodeServiceClient
	atxPublisher := nodeServiceClient
	syncer = alwaysSyncedSyncer{}

	atxBuilder := activation.NewBuilder(
		builderConfig,
		app.localDB,
		atxService,
		atxPublisher,
		app.validator,
		nipostBuilder,
		app.clock,
		syncer,
		atxBuilderLog,
		activation.WithContext(ctx),
		activation.WithPoetConfig(app.Config.POET),
		// TODO(dshulyak) makes no sense. how we ended using it?
		activation.WithPoetRetryInterval(app.Config.HARE3.PreroundDelay),
		activation.WithPostStates(postStates),
		activation.WithIdentityStates(app.idStates),
		activation.WithPoets(poetClients...),
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
			atxBuilder.Register(sig)
		}
	}
	app.postSupervisor = activation.NewPostSupervisor(
		app.log.Zap(),
		app.Config.POST,
		app.Config.SMESHING.ProvingOpts,
		postSetupMgr,
		atxBuilder,
	)
	if err != nil {
		return fmt.Errorf("init post service: %w", err)
	}

	app.mesh = msh
	app.syncer = newSyncer
	app.atxBuilder = atxBuilder
	app.atxHandler = atxHandler
	app.poetDb = poetDb
	app.fetcher = fetcher
	app.beaconProtocol = beaconProtocol

	return nil
}

func (app *App) startActivationServiceServices(ctx context.Context) error {
	if app.fetcher != nil {
		if err := app.fetcher.Start(); err != nil {
			return fmt.Errorf("start fetcher: %w", err)
		}
	}
	if app.syncer != nil {
		app.syncer.Start()
	}

	if app.beaconProtocol != nil {
		app.beaconProtocol.Start(ctx)
	}

	if app.blockGen != nil {
		app.blockGen.Start(ctx)
	}
	if app.certifier != nil {
		app.certifier.Start(ctx)
	}
	if app.proposalBuilder != nil {
		app.eg.Go(func() error {
			return app.proposalBuilder.Run(ctx)
		})
	}
	if app.remoteProposalBuilder != nil {
		app.eg.Go(func() error {
			return app.remoteProposalBuilder.Run(ctx)
		})
	}

	if app.Config.SMESHING.CoinbaseAccount != "" {
		coinbaseAddr, err := types.StringToAddress(app.Config.SMESHING.CoinbaseAccount)
		if err != nil {
			return fmt.Errorf(
				"parse CoinbaseAccount address on start `%s`: %w",
				app.Config.SMESHING.CoinbaseAccount,
				err,
			)
		}
		if err := app.atxBuilder.StartSmeshing(coinbaseAddr); err != nil {
			return fmt.Errorf("start smeshing: %w", err)
		}
	}

	if app.ptimesync != nil {
		app.ptimesync.Start()
	}

	if app.updater != nil {
		app.listenToUpdates(ctx)
	}
	return nil
}

// StartActivationService starts the Spacemesh activation service and
// initializes all relevant services according to command line
// arguments provided.
func (app *App) StartActivationService(ctx context.Context) error {
	if !app.Config.IsNodeServiceClientMode() {
		return errors.New("attempt to start activation service using node service configuration")
	}
	if err := app.verifyVersionUpgrades(); err != nil {
		return fmt.Errorf("version upgrade verification failed: %w", err)
	}

	err := app.startActivationServiceSynchronous(ctx)
	if err != nil {
		app.log.With().Error("failed to start App", log.Err(err))
		return err
	}
	defer events.ReportError(events.NodeError{
		Msg:   "node is shutting down",
		Level: zapcore.InfoLevel,
	})
	// TODO: pass app.eg to components and wait for them collectively
	if app.ptimesync != nil {
		app.eg.Go(func() error {
			app.errCh <- app.ptimesync.Wait()
			return nil
		})
	}

	// app blocks until it receives a signal to exit
	// this signal may come from the node or from sig-abort (ctrl-c)
	select {
	case <-ctx.Done():
		return nil
	case err = <-app.errCh:
		return err
	}
}

func (app *App) startActivationServiceSynchronous(ctx context.Context) (err error) {
	// notify anyone who might be listening that the app has finished starting.
	// this can be used by, e.g., app tests.
	defer close(app.started)

	// Create a contextual logger for local usage (lower-level modules will create their own contextual loggers
	// using context passed down to them)
	logger := app.log.WithContext(ctx)

	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("error reading hostname: %w", err)
	}

	logger.With().Info("starting spacemesh",
		log.String("data-dir", app.Config.DataDir()),
		log.String("post-dir", app.Config.SMESHING.Opts.DataDir),
		log.String("hostname", hostname),
	)

	if err := os.MkdirAll(app.Config.DataDir(), 0o700); err != nil {
		return fmt.Errorf(
			"data-dir %s not found or could not be created: %w",
			app.Config.DataDir(),
			err,
		)
	}

	/* Setup monitoring */
	app.errCh = make(chan error, 100)
	if app.Config.PprofHTTPServer {
		logger.With().Info("starting pprof server", log.String("address", app.Config.PprofHTTPServerListener))
		app.pprofService = &http.Server{Addr: app.Config.PprofHTTPServerListener}
		app.eg.Go(func() error {
			if err := app.pprofService.ListenAndServe(); err != nil {
				app.errCh <- fmt.Errorf("cannot start pprof http server: %w", err)
			}
			return nil
		})
		if app.Config.PprofMutexProfile {
			// this will set the mutex profiling to sample a third of all lock events
			runtime.SetMutexProfileFraction(3)
		}
		if app.Config.PprofBlockProfile {
			// record block sample for every block event that takes more than 10 milliseconds
			runtime.SetBlockProfileRate(int(10 * time.Millisecond))
		}
	}

	if app.Config.ProfilerURL != "" {
		app.profilerService, err = pyroscope.Start(pyroscope.Config{
			ApplicationName: app.Config.ProfilerName,
			// app.Config.ProfilerURL should be the pyroscope server address
			// TODO: AuthToken? no need right now since server isn't public
			ServerAddress: app.Config.ProfilerURL,
			// by default all profilers are enabled,
		})
		if err != nil {
			return fmt.Errorf("cannot start profiling client: %w", err)
		}
	}

	var preserved *checkpoint.PreservedData
	if app.Config.Recovery.Uri != "" {
		preserved, err = app.loadCheckpoint(ctx)
		if err != nil {
			return fmt.Errorf("loading checkpoint: %w", err)
		}
	}

	/* Initialize all protocol services */
	app.clock, err = timesync.NewClock(
		timesync.WithLayerDuration(app.Config.LayerDuration),
		timesync.WithTickInterval(1*time.Second),
		timesync.WithGenesisTime(app.Config.Genesis.GenesisTime.Time()),
		timesync.WithLogger(app.addLogger(ClockLogger, logger).Zap()),
	)
	if err != nil {
		return fmt.Errorf("cannot create clock: %w", err)
	}

	if err := app.setupDBs(ctx, logger); err != nil {
		return err
	}

	if err := app.initActivationServiceServices(ctx); err != nil {
		return fmt.Errorf("init services: %w", err)
	}

	if app.Config.CollectMetrics {
		metrics.StartMetricsServer(app.Config.MetricsPort)
	}

	if app.Config.PublicMetrics.MetricsURL != "" {
		id := hash.Sum([]byte(app.host.ID()))
		metrics.StartPushingMetrics(
			app.Config.PublicMetrics.MetricsURL,
			app.Config.PublicMetrics.MetricsPushUser,
			app.Config.PublicMetrics.MetricsPushPass,
			app.Config.PublicMetrics.MetricsPushHeader,
			app.Config.PublicMetrics.MetricsPushPeriod,
			types.Hash32(id).ShortString(), app.Config.Genesis.GenesisID().ShortString())
	}

	if err := app.startActivationServiceServices(ctx); err != nil {
		return fmt.Errorf("start services: %w", err)
	}

	// need post verifying service to start first
	if preserved != nil {
		app.preserveAfterRecovery(ctx, *preserved)
	} else {
		app.log.Info("no need to preserve data after recovery")
	}

	if err := app.startAPIServices(ctx); err != nil {
		return err
	}

	if err := app.launchStandalone(ctx); err != nil {
		return err
	}

	events.SubscribeToLayers(app.clock)
	app.log.Info("app started")

	return nil
}
