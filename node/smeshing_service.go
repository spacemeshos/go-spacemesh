// Package node contains the main executable for go-spacemesh node
package node

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"syscall"
	"time"

	pyroscope "github.com/grafana/pyroscope-go"
	grpczap "github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver/v2beta1"
	"github.com/spacemeshos/go-spacemesh/api/node/client"
	nodeclient "github.com/spacemeshos/go-spacemesh/api/node/client"
	"github.com/spacemeshos/go-spacemesh/api/proxy"
	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/beacon"
	"github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/hare3"
	"github.com/spacemeshos/go-spacemesh/hare3/eligibility"
	"github.com/spacemeshos/go-spacemesh/identity"
	"github.com/spacemeshos/go-spacemesh/metrics"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

func GetSmeshingServiceCommand() *cobra.Command {
	conf := config.MainnetSmeshingServiceConfig()
	var configPath *string
	c := &cobra.Command{
		Use:   "smeshing",
		Short: "Start smeshing service",
		RunE: func(c *cobra.Command, args []string) error {
			if err := configure(c, *configPath, &conf); err != nil {
				return err
			}

			encoder := zapcore.NewConsoleEncoder(zap.NewDevelopmentEncoderConfig())
			if conf.LOGGING.Encoder == config.JSONLogEncoder {
				encoder = zapcore.NewJSONEncoder(zap.NewDevelopmentEncoderConfig())
			}
			core := zapcore.NewCore(encoder, zapcore.AddSync(os.Stdout), zap.DebugLevel)
			lg := zap.New(zapcore.RegisterHooks(core, events.EventHook())).Named("node")

			events.InitializeReporter()
			lg.Info(getAppInfo(conf.Genesis))
			lg.Info("Welcome to Spacemesh. Spacemesh activation service is starting...")

			app, err := NewSmeshingService(&conf, lg)
			if err != nil {
				return fmt.Errorf("creating smeshing service app: %w", err)
			}
			unlock, err := lock(conf.FileLock)
			if err != nil {
				return err
			}
			defer func() {
				if err := unlock(); err != nil {
					lg.Error("failed to unlock file", zap.String("path", conf.FileLock), zap.Error(err))
				}
			}()

			// os.Interrupt for all systems, especially windows, syscall.SIGTERM is mainly for docker.
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			// This blocks until the context is finished or until an error is produced
			if err := app.Start(ctx); err != nil {
				lg.Error("app failed", zap.Error(err))
			}

			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cleanupCancel()
			done := make(chan struct{}, 1)
			// FIXME: per https://github.com/spacemeshos/go-spacemesh/issues/3830
			go func() {
				app.stopServices(cleanupCtx)
				close(done)
			}()
			select {
			case <-done:
			case <-cleanupCtx.Done():
				app.log.Error("app failed to clean up in time")
			}
			return nil
		},
	}

	configPath = cmd.AddSmeshingServiceFlags(c.PersistentFlags(), &conf)

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

	return c
}

type SmeshingService struct {
	log     *zap.Logger
	loggers *loggers
	signers []*signing.EdSigner
	config  *config.Config
	db      sql.StateDatabase // FIXME: remove
	localDB sql.LocalDatabase

	grpcPublicServer  *grpcserver.Server
	grpcPrivateServer *grpcserver.Server
	grpcPostServer    *grpcserver.Server
	grpcTLSServer     *grpcserver.Server
	jsonAPIServer     *grpcserver.JSONHTTPServer
	grpcServices      map[grpcserver.Service]grpcserver.ServiceAPI
	pprofService      *http.Server
	profilerService   *pyroscope.Profiler
	apiProxy          *proxy.Server

	proposalsBuilder *miner.RemoteProposalBuilder
	clock            *timesync.NodeClock
	remoteHare       *hare3.RemoteHare
	atxBuilder       *activation.Builder
	postVerifier     activation.PostVerifier
	postSupervisor   *activation.PostSupervisor
	idStates         *identity.StateStorage
	poetClients      []activation.PoetService

	started chan struct{}
	errCh   chan error
	eg      errgroup.Group
}

func NewSmeshingService(cfg *config.Config, logger *zap.Logger) (*SmeshingService, error) {
	loggers, err := newLoggers(&cfg.LOGGING)
	if err != nil {
		return nil, fmt.Errorf("parsing loggers config: %w", err)
	}

	types.SetLayersPerEpoch(cfg.LayersPerEpoch)
	types.SetNetworkHRP(cfg.NetworkHRP)

	// ensure all data folders exist
	if err := os.MkdirAll(cfg.DataDir(), 0o700); err != nil {
		return nil, fmt.Errorf("ensure folders exist: %w", err)
	}

	if err := verifyLocalDbMigrations(cfg); err != nil {
		return nil, fmt.Errorf("version upgrade verification failed: %w", err)
	}

	localDB, stateDB, err := setupDBs(cfg, loggers.add(StateDbLogger, logger))
	if err != nil {
		return nil, err
	}

	signers, err := loadIdentities(cfg.DataDir(), cfg.Genesis.GenesisID(), logger)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		logger.Info("Identity file not found. Creating new identity...")
		signer, err := newIdentity(cfg.DataDir(), cfg.Genesis.GenesisID(), logger)
		if err != nil {
			return nil, fmt.Errorf("creating new identity: %w", err)
		}
		signers = []*signing.EdSigner{signer}
	case err != nil:
		return nil, fmt.Errorf("loading identities: %w", err)
	}

	gpath := filepath.Join(cfg.DataDir(), genesisFileName)
	if err := applyGenesis(gpath, cfg.Genesis); err != nil {
		return nil, err
	}

	layerSize := cfg.LayerAvgSize
	layersPerEpoch := types.GetLayersPerEpoch()

	var nodeServiceClient *client.NodeService
	listenAddress := cfg.BaseConfig.NodeServiceAddress
	nodeClientLog := loggers.add(NodeServiceClientLogger, logger)
	nodeClientCfg := &nodeclient.Config{
		RetryWaitMin: time.Second,
		RetryWaitMax: time.Second * 30,
		RetryMax:     10,
	}
	nodeServiceClient, err = nodeclient.NewNodeServiceClient(listenAddress, nodeClientLog, nodeClientCfg)
	if err != nil {
		return nil, fmt.Errorf("creating node service client: %w", err)
	}

	poetDb, err := activation.NewPoetDb(
		stateDB,
		loggers.add(PoetDbLogger, logger),
		activation.WithCacheSize(cfg.POET.PoetProofsCache),
	)
	if err != nil {
		return nil, fmt.Errorf("creating poet db: %w", err)
	}
	idStates := identity.NewIdentityStateStorage(localDB, logger)

	postVerifier, err := activation.NewPostVerifier(
		cfg.POST,
		loggers.add(NipostValidatorLogger, logger),
		activation.WithVerifyingOpts(cfg.SMESHING.VerifyingOpts),
	)
	if err != nil {
		return nil, fmt.Errorf("creating post verifier: %w", err)
	}

	validator := activation.NewValidator(
		stateDB,
		poetDb,
		cfg.POST,
		cfg.SMESHING.Opts.Scrypt,
		postVerifier,
	)

	goldenATXID := types.ATXID(cfg.Genesis.GoldenATX())
	if goldenATXID == types.EmptyATXID {
		return nil, errors.New("invalid golden atx id")
	}

	// we can't have an epoch offset which is greater/equal than the number of layers in an epoch
	if cfg.HareEligibility.ConfidenceParam >= cfg.BaseConfig.LayersPerEpoch {
		return nil, fmt.Errorf(
			"confidence param should be smaller than layers per epoch. eligibility-confidence-param: %d. "+
				"layers-per-epoch: %d",
			cfg.HareEligibility.ConfidenceParam,
			cfg.BaseConfig.LayersPerEpoch,
		)
	}

	beaconProvider := beacon.NewBeaconCache(nodeServiceClient)
	cachedWeights := eligibility.NewCachedWeights(nodeServiceClient)
	hOracle, err := eligibility.New(
		cachedWeights,
		beaconProvider,
		signing.NewVRFVerifier(),
		cfg.LayersPerEpoch,
		eligibility.WithConfig(cfg.HareEligibility),
		eligibility.WithLogger(loggers.add(HareOracleLogger, logger)),
	)
	if err != nil {
		return nil, fmt.Errorf("create hare oracle: %w", err)
	}

	clock, err := timesync.NewClock(
		timesync.WithLayerDuration(cfg.LayerDuration),
		timesync.WithTickInterval(1*time.Second),
		timesync.WithGenesisTime(cfg.Genesis.GenesisTime.Time()),
		timesync.WithLogger(loggers.add(ClockLogger, logger)),
	)
	if err != nil {
		return nil, fmt.Errorf("cannot create clock: %w", err)
	}

	if err := cfg.HARE3.Validate(time.Duration(cfg.Tortoise.Zdist) * cfg.LayerDuration); err != nil {
		return nil, err
	}
	remoteHare := hare3.NewRemoteHare(
		cfg.HARE3,
		clock,
		nodeServiceClient,
		beaconProvider,
		hOracle,
		loggers.add(HareLogger, logger),
	)
	for _, sig := range signers {
		remoteHare.Register(sig)
	}

	proposalBuilder := miner.NewRemoteBuilder(
		clock,
		nodeServiceClient,
		beaconProvider,
		nodeServiceClient,
		layerSize,
		layersPerEpoch,
		loggers.add(ProposalBuilderLogger, logger),
		idStates,
	)
	for _, sig := range signers {
		proposalBuilder.Register(sig)
	}

	postSetupMgr, err := activation.NewPostSetupManager(
		cfg.POST,
		loggers.add(PostLogger, logger),
		stateDB,
		atxsdata.New(), // FIXME: remove this dependency
		goldenATXID,
		nil,
		validator,
		activation.PostValidityDelay(cfg.PostValidDelay),
	)
	if err != nil {
		return nil, fmt.Errorf("create post setup manager: %v", err)
	}

	nipostLogger := loggers.add(NipostBuilderLogger, logger)
	client := activation.NewCertifierClient(
		stateDB,
		localDB,
		nipostLogger,
		activation.WithCertifierClientConfig(cfg.Certifier.Client),
	)
	poetCertifier := activation.NewCertifier(localDB, nipostLogger, client)

	poetClients := make([]activation.PoetService, 0, len(cfg.PoetServers))
	for _, server := range cfg.PoetServers {
		client, err := activation.NewPoetService(
			poetDb,
			server,
			cfg.POET,
			loggers.add(PoetLogger, logger),
			cfg.TickSize,
			activation.WithCertifier(poetCertifier),
		)
		if err != nil {
			return nil, fmt.Errorf("creating poet client for %s: %w", server, err)
		}
		poetClients = append(poetClients, client)
	}
	grpcServices := make(map[grpcserver.Service]grpcserver.ServiceAPI)

	grpcPostService := grpcserver.NewPostService(loggers.add(PostServiceLogger, logger))
	isCoinbaseSet := cfg.SMESHING.CoinbaseAccount != ""
	if !isCoinbaseSet {
		logger.Warn("coinbase account is not set, connections from remote post services will be rejected")
	}
	grpcPostService.AllowConnections(isCoinbaseSet)
	grpcServices[grpcserver.Post] = grpcPostService

	nipostBuilder, err := activation.NewNIPostBuilder(
		localDB,
		grpcPostService,
		nipostLogger,
		cfg.POET,
		clock,
		validator,
		activation.NipostbuilderWithIdentityStates(idStates),
		activation.WithPoetServices(poetClients...),
	)
	if err != nil {
		return nil, fmt.Errorf("create nipost builder: %w", err)
	}

	builderConfig := activation.Config{
		GoldenATXID:      goldenATXID,
		RegossipInterval: cfg.RegossipAtxInterval,
	}

	atxBuilder := activation.NewBuilder(
		builderConfig,
		localDB,
		poetDb,
		nodeServiceClient,
		nodeServiceClient,
		validator,
		nipostBuilder,
		clock,
		alwaysSyncedSyncer{},
		loggers.add(ATXBuilderLogger, logger),
		activation.WithPoetConfig(cfg.POET),
		// TODO(dshulyak) makes no sense. how we ended using it?
		activation.WithPoetRetryInterval(cfg.HARE3.PreroundDelay),
		activation.WithIdentityStates(idStates),
		activation.WithPoets(poetClients...),
		activation.BuilderAtxVersions(cfg.AtxVersions),
	)

	if len(signers) > 1 || signers[0].Name() != supervisedIDKeyFileName {
		// in a remote setup we register eagerly so the atxBuilder can warn about missing connections asap.
		// Any setup with more than one signer is considered a remote setup. If there is only one signer it
		// is considered a remote setup if the key for the signer has not been sourced from `supervisedIDKeyFileName`.
		//
		// In a supervised setup the postSetupManager will register at the atxBuilder when
		// it finished initializing, to avoid warning about a missing connection when the supervised post
		// service isn't ready yet.
		for _, sig := range signers {
			atxBuilder.Register(sig)
		}
	}
	postSupervisor := activation.NewPostSupervisor(
		logger,
		cfg.POST,
		cfg.SMESHING.ProvingOpts,
		postSetupMgr,
		atxBuilder,
	)
	if err != nil {
		return nil, fmt.Errorf("init post service: %w", err)
	}

	return &SmeshingService{
		db:           stateDB,
		localDB:      localDB,
		config:       cfg,
		loggers:      loggers,
		log:          logger,
		signers:      signers,
		grpcServices: grpcServices,

		proposalsBuilder: proposalBuilder,
		clock:            clock,
		remoteHare:       remoteHare,
		atxBuilder:       atxBuilder,
		postVerifier:     postVerifier,
		postSupervisor:   postSupervisor,
		idStates:         idStates,
		poetClients:      poetClients,

		errCh:   make(chan error, 5),
		started: make(chan struct{}),
	}, nil
}

func (app *SmeshingService) startServices(ctx context.Context) error {
	app.remoteHare.Start(ctx)
	app.eg.Go(func() error {
		return app.proposalsBuilder.Run(ctx)
	})

	if app.config.SMESHING.CoinbaseAccount != "" {
		coinbaseAddr, err := types.StringToAddress(app.config.SMESHING.CoinbaseAccount)
		if err != nil {
			return fmt.Errorf(
				"parse CoinbaseAccount address on start `%s`: %w",
				app.config.SMESHING.CoinbaseAccount,
				err,
			)
		}
		if err := app.atxBuilder.StartSmeshing(coinbaseAddr); err != nil {
			return fmt.Errorf("start smeshing: %w", err)
		}
	}

	return nil
}

// StartSmeshingService starts the Spacemesh activation service and
// initializes all relevant services according to command line
// arguments provided.
func (app *SmeshingService) Start(ctx context.Context) error {
	err := app.start(ctx)
	if err != nil {
		app.log.Error("failed to start App", zap.Error(err))
		return err
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

func (app *SmeshingService) start(ctx context.Context) (err error) {
	defer close(app.started)

	hostname, err := os.Hostname()
	if err != nil {
		app.log.Warn("couldn't read hostname", zap.Error(err))
		hostname = "unknown"
	}

	app.log.Info("starting smeshing service",
		zap.String("data-dir", app.config.DataDir()),
		zap.String("post-dir", app.config.SMESHING.Opts.DataDir),
		zap.String("hostname", hostname),
	)

	/* Setup monitoring */
	if app.config.PprofHTTPServer {
		app.log.Info("starting pprof server", zap.String("address", app.config.PprofHTTPServerListener))
		lis, err := net.Listen("tcp", app.config.PprofHTTPServerListener)
		if err != nil {
			return fmt.Errorf("starting pprof server: %w", err)
		}
		app.pprofService = &http.Server{
			Addr: lis.Addr().String(),
		}
		app.eg.Go(func() error {
			err := app.pprofService.Serve(lis)
			if err != nil {
				app.errCh <- fmt.Errorf("cannot start pprof http server: %w", err)
			}
			return err
		})
		if app.config.PprofMutexProfile {
			// this will set the mutex profiling to sample a third of all lock events
			runtime.SetMutexProfileFraction(3)
		}
		if app.config.PprofBlockProfile {
			// record block sample for every block event that takes more than 10 milliseconds
			runtime.SetBlockProfileRate(int(10 * time.Millisecond))
		}
	}

	if app.config.ProfilerURL != "" {
		app.profilerService, err = pyroscope.Start(pyroscope.Config{
			ApplicationName: app.config.ProfilerName,
			ServerAddress:   app.config.ProfilerURL,
			// by default all profilers are enabled,
		})
		if err != nil {
			return fmt.Errorf("cannot start profiling client: %w", err)
		}
	}

	if app.config.CollectMetrics {
		metrics.StartMetricsServer(app.config.MetricsPort)
	}

	if app.config.PublicMetrics.MetricsURL != "" {
		metrics.StartPushingMetrics(
			app.config.PublicMetrics.MetricsURL,
			app.config.PublicMetrics.MetricsPushUser,
			app.config.PublicMetrics.MetricsPushPass,
			app.config.PublicMetrics.MetricsPushHeader,
			app.config.PublicMetrics.MetricsPushPeriod,
			app.signers[0].NodeID().ShortString(),
			app.config.Genesis.GenesisID().ShortString(),
		)
	}

	if err := app.startServices(ctx); err != nil {
		return fmt.Errorf("start services: %w", err)
	}

	if err := app.startAPIServices(ctx); err != nil {
		return err
	}

	events.SubscribeToLayers(app.clock)
	app.log.Info("app started")

	return nil
}

func (app *SmeshingService) grpcService(svc grpcserver.Service, logger *zap.Logger) (grpcserver.ServiceAPI, error) {
	if service, ok := app.grpcServices[svc]; ok {
		return service, nil
	}

	switch svc {
	case grpcserver.Debug:
		service := grpcserver.NewSmeshingServiceDebugService(app.loggers.levels)
		app.grpcServices[svc] = service
		return service, nil
	case grpcserver.Smesher:
		var sig *signing.EdSigner
		if len(app.signers) == 1 && app.signers[0].Name() == supervisedIDKeyFileName {
			// StartSmeshing is only supported in a supervised setup (single signer)
			sig = app.signers[0]
		}
		postService, err := app.grpcService(grpcserver.Post, logger)
		if err != nil {
			return nil, err
		}
		service := grpcserver.NewSmesherService(
			app.atxBuilder,
			app.postSupervisor,
			postService.(*grpcserver.PostService),
			app.config.API.SmesherStreamInterval,
			app.config.SMESHING.Opts,
			sig,
		)
		app.grpcServices[svc] = service
		return service, nil
	case grpcserver.Post:
		service := grpcserver.NewPostService(app.loggers.add(PostServiceLogger, logger))
		isCoinbaseSet := app.config.SMESHING.CoinbaseAccount != ""
		if !isCoinbaseSet {
			app.log.Warn("coinbase account is not set, connections from remote post services will be rejected")
		}
		service.AllowConnections(isCoinbaseSet)
		app.grpcServices[svc] = service
		return service, nil
	case grpcserver.PostInfo:
		service := grpcserver.NewPostInfoService(app.atxBuilder)
		app.grpcServices[svc] = service
		return service, nil
	case v2beta1.SmeshingIdentities:
		service := v2beta1.NewSmeshingIdentitiesService(app.idStates, app.poetClients, app.config.POET)
		app.grpcServices[svc] = service
		return service, nil
	case v2beta1.Smeshing:
		service := v2beta1.NewSmeshingService(cmd.Version, cmd.Commit)
		app.grpcServices[svc] = service
		return service, nil
	}
	return nil, fmt.Errorf("unknown service %s", svc)
}

func (app *SmeshingService) startAPIServices(ctx context.Context) error {
	logger := app.loggers.add(GRPCLogger, app.log)
	grpczap.SetGrpcLoggerV2(grpcLog, logger)

	var (
		publicSvcs        = make(map[grpcserver.Service]grpcserver.ServiceAPI, len(app.config.API.PublicServices))
		privateSvcs       = make(map[grpcserver.Service]grpcserver.ServiceAPI, len(app.config.API.PrivateServices))
		postSvcs          = make(map[grpcserver.Service]grpcserver.ServiceAPI, len(app.config.API.PostServices))
		authenticatedSvcs = make(map[grpcserver.Service]grpcserver.ServiceAPI, len(app.config.API.TLSServices))
	)

	// check services for uniques across all endpoints
	for _, svc := range app.config.API.PublicServices {
		if _, exists := publicSvcs[svc]; exists {
			return fmt.Errorf("can't start more than one %s on public grpc endpoint", svc)
		}
		gsvc, err := app.grpcService(svc, app.log)
		if err != nil {
			return err
		}
		logger.Info("registering public service", zap.String("name", svc))
		publicSvcs[svc] = gsvc
	}
	for _, svc := range app.config.API.PrivateServices {
		if _, exists := privateSvcs[svc]; exists {
			return fmt.Errorf("can't start more than one %s on private grpc endpoint", svc)
		}
		gsvc, err := app.grpcService(svc, app.log)
		if err != nil {
			return err
		}
		logger.Info("registering private service", zap.String("name", svc))
		privateSvcs[svc] = gsvc
	}
	for _, svc := range app.config.API.PostServices {
		if _, exists := postSvcs[svc]; exists {
			return fmt.Errorf("can't start more than one %s on post grpc endpoint", svc)
		}
		gsvc, err := app.grpcService(svc, app.log)
		if err != nil {
			return err
		}
		logger.Info("registering post service", zap.String("name", svc))
		postSvcs[svc] = gsvc
	}
	for _, svc := range app.config.API.TLSServices {
		if _, exists := authenticatedSvcs[svc]; exists {
			return fmt.Errorf("can't start more than one %s on authenticated grpc endpoint", svc)
		}
		gsvc, err := app.grpcService(svc, app.log)
		if err != nil {
			return err
		}
		logger.Info("registering authenticated service", zap.String("name", svc))
		authenticatedSvcs[svc] = gsvc
	}

	// start servers if at least one endpoint is defined for them
	if len(publicSvcs) > 0 {
		var err error
		app.grpcPublicServer, err = grpcserver.NewWithServices(
			app.config.API.PublicListener,
			logger,
			app.config.API,
			slices.Collect(maps.Values(publicSvcs)),
			// public server needs restriction on max connection age to prevent attacks
			grpc.KeepaliveParams(keepalive.ServerParameters{
				MaxConnectionIdle:     2 * time.Hour,
				MaxConnectionAge:      3 * time.Hour,
				MaxConnectionAgeGrace: 10 * time.Minute,
				Time:                  time.Minute,
				Timeout:               10 * time.Second,
			}),
		)
		if err != nil {
			return err
		}
		if err := app.grpcPublicServer.Start(); err != nil {
			return err
		}
		logger.Info("public grpc service started",
			zap.String("address", app.config.API.PublicListener),
			zap.Array("services", zapcore.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
				for _, svc := range slices.Sorted(maps.Keys(publicSvcs)) {
					encoder.AppendString(svc)
				}
				return nil
			})),
		)
	}
	if len(privateSvcs) > 0 {
		var err error
		app.grpcPrivateServer, err = grpcserver.NewWithServices(
			app.config.API.PrivateListener,
			logger,
			app.config.API,
			slices.Collect(maps.Values(privateSvcs)),
		)
		if err != nil {
			return err
		}
		if err := app.grpcPrivateServer.Start(); err != nil {
			return err
		}
		logger.Info("private grpc service started",
			zap.String("address", app.config.API.PrivateListener),
			zap.Array("services", zapcore.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
				for _, svc := range slices.Sorted(maps.Keys(privateSvcs)) {
					encoder.AppendString(svc)
				}
				return nil
			})),
		)
	}
	if len(postSvcs) > 0 && app.config.API.PostListener != "" {
		var err error
		app.grpcPostServer, err = grpcserver.NewWithServices(
			app.config.API.PostListener,
			logger,
			app.config.API,
			slices.Collect(maps.Values(postSvcs)),
		)
		if err != nil {
			return err
		}
		if err := app.grpcPostServer.Start(); err != nil {
			return err
		}
		logger.Info("post grpc service started",
			zap.String("address", app.config.API.PostListener),
			zap.Array("services", zapcore.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
				for _, svc := range slices.Sorted(maps.Keys(postSvcs)) {
					encoder.AppendString(svc)
				}
				return nil
			})),
		)

		host, port, err := net.SplitHostPort(app.grpcPostServer.BoundAddress)
		if err != nil {
			return fmt.Errorf("parse grpc-post-listener: %w", err)
		}
		ip := net.ParseIP(host)
		if ip.IsUnspecified() { // 0.0.0.0 isn't a valid address to connect to on windows
			host = "127.0.0.1"
		}
		app.config.POSTService.NodeAddress = fmt.Sprintf("http://%s:%s", host, port)
		svc, err := app.grpcService(grpcserver.Smesher, app.log)
		if err != nil {
			return err
		}
		svc.(*grpcserver.SmesherService).SetPostServiceConfig(app.config.POSTService)
		if app.config.SMESHING.Start {
			if app.config.SMESHING.CoinbaseAccount == "" {
				return errors.New("smeshing enabled but no coinbase account provided")
			}
			if len(app.signers) > 1 || app.signers[0].Name() != supervisedIDKeyFileName {
				app.log.Error("supervised smeshing cannot be started in a remote or multi-smeshing setup")
				app.log.Sugar().Errorf(
					"if you run a supervised node ensure your key file is named %s and try again",
					supervisedIDKeyFileName,
				)
				return errors.New("smeshing enabled in remote setup")
			}
			if err := app.postSupervisor.Start(
				app.config.POSTService,
				app.config.SMESHING.Opts,
				app.signers[0],
			); err != nil {
				return fmt.Errorf("start post service: %w", err)
			}
		} else if len(app.signers) == 1 && app.signers[0].Name() == supervisedIDKeyFileName {
			// supervised setup but not started
			app.log.Info("smeshing not started, waiting to be triggered via smesher api")
		}
	}

	if len(authenticatedSvcs) > 0 && app.config.API.TLSListener != "" {
		var err error
		app.grpcTLSServer, err = grpcserver.NewTLS(
			logger,
			app.config.API,
			slices.Collect(maps.Values(authenticatedSvcs)),
		)
		if err != nil {
			return err
		}
		if err := app.grpcTLSServer.Start(); err != nil {
			return err
		}
		logger.Info("authenticated grpc service started",
			zap.String("address", app.config.API.TLSListener),
			zap.Array("services", zapcore.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
				for _, svc := range slices.Sorted(maps.Keys(authenticatedSvcs)) {
					encoder.AppendString(svc)
				}
				return nil
			})),
		)
	}

	if len(app.config.API.JSONListener) > 0 {
		if len(publicSvcs) == 0 {
			return errors.New("start json server without public services")
		}
		if len(app.config.API.ProxyApiV2Address) > 0 {
			var localSvcs []proxy.Service
			for _, svcName := range app.config.API.NonProxiedServices {
				svc, err := app.grpcService(svcName, logger)
				if err != nil {
					return fmt.Errorf("creating smeshing id service: %w", err)
				}
				if svc, ok := svc.(proxy.Service); ok {
					localSvcs = append(localSvcs, svc)
				} else {
					return fmt.Errorf("cannot use service %q as non-proxied local service", svcName)
				}
			}

			p, err := proxy.NewServer(
				app.config.API.JSONListener,
				app.config.API.ProxyApiV2Address,
				app.config.API.JSONCorsEverywhere,
				logger,
				localSvcs...,
			)
			if err != nil {
				return err
			}
			app.apiProxy = p

			if err = p.Start(); err != nil {
				return err
			}
			logger.Info("json proxy listener started",
				zap.String("address", app.config.API.JSONListener),
				zap.String("proxying to", app.config.API.ProxyApiV2Address),
				zap.Array("services", zapcore.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
					for _, svc := range slices.Sorted(maps.Keys(publicSvcs)) {
						encoder.AppendString(svc)
					}
					return nil
				})),
			)
		} else {
			app.jsonAPIServer = grpcserver.NewJSONHTTPServer(
				logger.Named("JSON"),
				app.config.API.JSONListener,
				app.config.API.JSONCorsAllowedOrigins,
				app.config.API.JSONCorsEverywhere,
				app.config.CollectMetrics,
			)

			if err := app.jsonAPIServer.StartService(slices.Collect(maps.Values(publicSvcs))...); err != nil {
				return fmt.Errorf("start listen server: %w", err)
			}
			logger.Info("json listener started",
				zap.String("address", app.config.API.JSONListener),
				zap.Array("services", zapcore.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
					for _, svc := range slices.Sorted(maps.Keys(publicSvcs)) {
						encoder.AppendString(svc)
					}
					return nil
				})),
			)
		}
	}

	return nil
}

func setupDBs(cfg *config.Config, logger *zap.Logger) (sql.LocalDatabase, sql.StateDatabase, error) {
	// FIXME: remove need for state DB
	db, err := openStateDB(cfg, logger)
	if err != nil {
		return nil, nil, err
	}

	localDB, err := openLocalDb(cfg, logger)
	if err != nil {
		return nil, nil, err
	}

	return localDB, db, nil
}

func (app *SmeshingService) stopServices(ctx context.Context) {
	app.log.Info("app stopping services...")
	defer app.log.Info("...finished stopping services")

	if app.jsonAPIServer != nil {
		if err := app.jsonAPIServer.Shutdown(ctx); err != nil {
			app.log.Error("error stopping json gateway server", zap.Error(err))
		}
	}
	if app.grpcPublicServer != nil {
		app.log.Info("stopping public grpc service")
		app.grpcPublicServer.Close() // err is always nil
	}
	if app.grpcPrivateServer != nil {
		app.log.Info("stopping private grpc service")
		app.grpcPrivateServer.Close() // err is always nil
	}
	if app.grpcPostServer != nil {
		app.log.Info("stopping local grpc service")
		app.grpcPostServer.Close() // err is always nil
	}
	if app.grpcTLSServer != nil {
		app.log.Info("stopping tls grpc service")
		app.grpcTLSServer.Close() // err is always nil
	}

	app.clock.Close()
	app.atxBuilder.StopSmeshing(false)
	app.postVerifier.Close()

	if err := app.postSupervisor.Stop(false); err != nil {
		app.log.Error("error stopping local post service", zap.Error(err))
	}

	if err := app.db.Close(); err != nil {
		app.log.Warn("db exited with error", zap.Error(err))
	}
	if err := app.localDB.Close(); err != nil {
		app.log.Warn("local db exited with error", zap.Error(err))
	}

	if app.pprofService != nil {
		if err := app.pprofService.Close(); err != nil {
			app.log.Warn("pprof service exited with error", zap.Error(err))
		}
	}
	if app.profilerService != nil {
		if err := app.profilerService.Stop(); err != nil {
			app.log.Warn("profiler service exited with error", zap.Error(err))
		}
	}
	if app.apiProxy != nil {
		if err := app.apiProxy.Stop(); err != nil {
			app.log.Warn("proxy server exited with error", zap.Error(err))
		}
	}
	app.eg.Wait()

	events.CloseEventReporter()
	// SetGrpcLogger unfortunately is global
	// this ensures that a test-logger isn't used after the app shuts down
	// by e.g. a grpc connection to the node that is still open - like in TestSpacemeshApp_NodeService
	grpczap.SetGrpcLoggerV2(grpcLog, zap.NewNop())
}
