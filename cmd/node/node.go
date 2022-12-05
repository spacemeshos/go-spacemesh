// Package node contains the main executable for go-spacemesh node
package node

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/gofrs/flock"
	grpcmw "github.com/grpc-ecosystem/go-grpc-middleware"
	grpczap "github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	grpctags "github.com/grpc-ecosystem/go-grpc-middleware/tags"
	"github.com/mitchellh/mapstructure"
	"github.com/pyroscope-io/pyroscope/pkg/agent/profiler"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/beacon"
	"github.com/spacemeshos/go-spacemesh/blocks"
	"github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/cmd/mapstructureutil"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/config/presets"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/fetch"
	vm "github.com/spacemeshos/go-spacemesh/genvm"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/hare/eligibility"
	"github.com/spacemeshos/go-spacemesh/layerpatrol"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/metrics"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/proposals"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	dbmetrics "github.com/spacemeshos/go-spacemesh/sql/metrics"
	"github.com/spacemeshos/go-spacemesh/syncer"
	"github.com/spacemeshos/go-spacemesh/system"
	"github.com/spacemeshos/go-spacemesh/timesync"
	timeCfg "github.com/spacemeshos/go-spacemesh/timesync/config"
	"github.com/spacemeshos/go-spacemesh/timesync/peersync"
	"github.com/spacemeshos/go-spacemesh/tortoise"
	"github.com/spacemeshos/go-spacemesh/txs"
)

const (
	edKeyFileName   = "key.bin"
	genesisFileName = "genesis.json"
	lockFile        = "LOCK"
)

// Logger names.
const (
	AppLogger              = "app"
	P2PLogger              = "p2p"
	PostLogger             = "post"
	StateDbLogger          = "stateDbStore"
	BeaconLogger           = "beacon"
	CachedDBLogger         = "cachedDB"
	PoetDbLogger           = "poetDb"
	TrtlLogger             = "trtl"
	ATXHandlerLogger       = "atxHandler"
	MeshLogger             = "mesh"
	SyncLogger             = "sync"
	HareOracleLogger       = "hareOracle"
	HareLogger             = "hare"
	BlockCertLogger        = "blockCert"
	BlockGenLogger         = "blockGenerator"
	BlockHandlerLogger     = "blockHandler"
	TxHandlerLogger        = "txHandler"
	ProposalBuilderLogger  = "proposalBuilder"
	ProposalListenerLogger = "proposalListener"
	PoetListenerLogger     = "poetListener"
	NipostBuilderLogger    = "nipostBuilder"
	Fetcher                = "fetcher"
	TimeSyncLogger         = "timesync"
	VMLogger               = "vm"
	GRPCLogger             = "grpc"
	ConStateLogger         = "conState"
)

func GetCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "node",
		Short: "start node",
		Run: func(c *cobra.Command, args []string) {
			conf, err := loadConfig(c)
			if err != nil {
				log.With().Fatal("failed to initialize config", log.Err(err))
			}

			if conf.LOGGING.Encoder == config.JSONLogEncoder {
				log.JSONLog(true)
			}
			app := New(
				WithConfig(conf),
				// NOTE(dshulyak) this needs to be max level so that child logger can can be current level or below.
				// otherwise it will fail later when child logger will try to increase level.
				WithLog(log.RegisterHooks(
					log.NewWithLevel("", zap.NewAtomicLevelAt(zapcore.DebugLevel)),
					events.EventHook())),
			)

			run := func(ctx context.Context) error {
				if err = app.Initialize(); err != nil {
					return err
				}
				// This blocks until the context is finished or until an error is produced
				err = app.Start(ctx)

				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
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
					log.With().Error("app failed to clean up in time")
				}
				return err
			}
			// os.Interrupt for all systems, especially windows, syscall.SIGTERM is mainly for docker.
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			if err = run(ctx); err != nil {
				log.With().Fatal(err.Error())
			}
		},
	}

	cmd.AddCommands(c)

	// versionCmd returns the current version of spacemesh.
	versionCmd := cobra.Command{
		Use:   "version",
		Short: "Show version info",
		Run: func(c *cobra.Command, args []string) {
			fmt.Print(cmd.Version)
			if cmd.Commit != "" {
				fmt.Printf("+%s", cmd.Commit)
			}
			fmt.Println()
		},
	}
	c.AddCommand(&versionCmd)

	return c
}

var (
	appLog  log.Log
	grpcLog *zap.Logger
)

func init() {
	appLog = log.NewNop()
	grpcLog = appLog.WithName(GRPCLogger).WithFields(log.String("module", GRPCLogger)).Zap()
	grpczap.ReplaceGrpcLoggerV2(grpcLog)
}

// Service is a general service interface that specifies the basic start/stop functionality.
type Service interface {
	Start(ctx context.Context) error
	Close()
}

// TickProvider is an interface to a glopbal system clock that releases ticks on each layer.
type TickProvider interface {
	Subscribe() timesync.LayerTimer
	Unsubscribe(timesync.LayerTimer)
	GetCurrentLayer() types.LayerID
	StartNotifying()
	GetGenesisTime() time.Time
	timesync.LayerConverter
	Close()
	AwaitLayer(types.LayerID) chan struct{}
}

func loadConfig(c *cobra.Command) (*config.Config, error) {
	conf, err := LoadConfigFromFile()
	if err != nil {
		return nil, fmt.Errorf("loading config from file: %w", err)
	}
	if err := cmd.EnsureCLIFlags(c, conf); err != nil {
		return nil, fmt.Errorf("mapping cli flags to config: %w", err)
	}
	return conf, nil
}

// LoadConfigFromFile tries to load configuration file if the config parameter was specified.
func LoadConfigFromFile() (*config.Config, error) {
	fileLocation := viper.GetString("config")

	// read in default config if passed as param using viper
	if err := config.LoadConfig(fileLocation, viper.GetViper()); err != nil {
		log.Error(fmt.Sprintf("couldn't load config file at location: %s switching to defaults \n error: %v.",
			fileLocation, err))
		// return err
	}

	conf := config.DefaultConfig()

	if name := viper.GetString("preset"); len(name) > 0 {
		preset, err := presets.Get(name)
		if err != nil {
			return nil, err
		}
		conf = preset
	}

	hook := mapstructure.ComposeDecodeHookFunc(
		mapstructure.StringToTimeDurationHookFunc(),
		mapstructure.StringToSliceHookFunc(","),
		mapstructureutil.BigRatDecodeFunc(),
	)

	// load config if it was loaded to the viper
	if err := viper.Unmarshal(&conf, viper.DecodeHook(hook)); err != nil {
		return nil, fmt.Errorf("unmarshal viper: %w", err)
	}
	return &conf, nil
}

// Option to modify an App instance.
type Option func(app *App)

// WithLog enables logger for an App.
func WithLog(logger log.Log) Option {
	return func(app *App) {
		app.log = logger
	}
}

// WithConfig overwrites default App config.
func WithConfig(conf *config.Config) Option {
	return func(app *App) {
		app.Config = conf
	}
}

// New creates an instance of the spacemesh app.
func New(opts ...Option) *App {
	defaultConfig := config.DefaultConfig()
	app := &App{
		Config:  &defaultConfig,
		log:     appLog,
		loggers: make(map[string]*zap.AtomicLevel),
		started: make(chan struct{}),
	}
	for _, opt := range opts {
		opt(app)
	}
	lvl := zap.NewAtomicLevelAt(zap.InfoLevel)
	log.SetupGlobal(app.log.SetLevel(&lvl))
	return app
}

// App is the cli app singleton.
type App struct {
	*cobra.Command
	fileLock         *flock.Flock
	nodeID           types.NodeID
	Config           *config.Config
	db               *sql.Database
	dbMetrics        *dbmetrics.DBMetricsCollector
	grpcAPIService   *grpcserver.Server
	jsonAPIService   *grpcserver.JSONHTTPServer
	syncer           *syncer.Syncer
	proposalListener *proposals.Handler
	proposalBuilder  *miner.ProposalBuilder
	mesh             *mesh.Mesh
	atxDB            datastore.CachedDB
	clock            TickProvider
	hare             *hare.Hare
	blockGen         *blocks.Generator
	certifier        *blocks.Certifier
	postSetupMgr     *activation.PostSetupManager
	atxBuilder       *activation.Builder
	atxHandler       *activation.Handler
	poetListener     *activation.PoetListener
	edSgn            *signing.EdSigner
	beaconProtocol   *beacon.ProtocolDriver
	log              log.Log
	svm              *vm.VM
	conState         *txs.ConservativeState
	fetcher          *fetch.Fetch
	ptimesync        *peersync.Sync
	tortoise         *tortoise.Tortoise

	host *p2p.Host

	loggers map[string]*zap.AtomicLevel
	started chan struct{} // this channel is closed once the app has finished starting
}

func (app *App) Started() chan struct{} {
	return app.started
}

func (app *App) introduction() {
	log.Info("Welcome to Spacemesh. Spacemesh full node is starting...")
}

// Initialize sets up an exit signal, logging and checks the clock, returns error if clock is not in sync.
func (app *App) Initialize() (err error) {
	// ensure all data folders exist
	if err := os.MkdirAll(app.Config.DataDir(), 0o700); err != nil {
		return fmt.Errorf("ensure folders exist: %w", err)
	}
	lockName := filepath.Join(app.Config.DataDir(), lockFile)
	fl := flock.New(lockName)
	locked, err := fl.TryLock()
	if err != nil {
		return fmt.Errorf("flock %s: %w", lockName, err)
	} else if !locked {
		return fmt.Errorf("only one spacemesh instance should be running (locking file %s)", fl.Path())
	}
	app.fileLock = fl

	gpath := filepath.Join(app.Config.DataDir(), genesisFileName)
	var existing config.GenesisConfig
	if err := existing.LoadFromFile(gpath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("failed to load genesis config at %s: %w", gpath, err)
		}
		if err := app.Config.Genesis.Validate(); err != nil {
			return err
		}
		if err := app.Config.Genesis.WriteToFile(gpath); err != nil {
			return fmt.Errorf("failed to write genesis config to %s: %w", gpath, err)
		}
	} else {
		diff := existing.Diff(app.Config.Genesis)
		if len(diff) > 0 {
			return fmt.Errorf("genesis config was updated after initializing a node, if you know that update is required delete config at %s.\ndiff:\n%s", gpath, diff)
		}
	}
	signing.DefaultVerifier = signing.NewEDVerifier(
		signing.WithVerifierPrefix(app.Config.Genesis.GenesisID().Bytes()))

	// tortoise wait zdist layers for hare to timeout for a layer. once hare timeout, tortoise will
	// vote against all blocks in that layer. so it's important to make sure zdist takes longer than
	// hare's max time duration to run consensus for a layer
	maxHareRoundsPerLayer := 1 + app.Config.HARE.LimitIterations*hare.RoundsPerIteration // pre-round + 4 rounds per iteration
	maxHareLayerDurationSec := app.Config.HARE.WakeupDelta + maxHareRoundsPerLayer*app.Config.HARE.RoundDuration
	if app.Config.LayerDurationSec*int(app.Config.Tortoise.Zdist) <= maxHareLayerDurationSec {
		log.With().Error("incompatible params",
			log.Uint32("tortoise_zdist", app.Config.Tortoise.Zdist),
			log.Int("layer_duration", app.Config.LayerDurationSec),
			log.Int("hare_wakeup_delta", app.Config.HARE.WakeupDelta),
			log.Int("hare_limit_iterations", app.Config.HARE.LimitIterations),
			log.Int("hare_round_duration", app.Config.HARE.RoundDuration))

		return errors.New("incompatible tortoise hare params")
	}

	// override default config in timesync since timesync is using TimeConfigValues
	timeCfg.TimeConfigValues = app.Config.TIME

	app.setupLogging()

	app.introduction()

	return nil
}

// setupLogging configured the app logging system.
func (app *App) setupLogging() {
	log.Info("%s", app.getAppInfo())
	events.InitializeReporter()
}

func (app *App) getAppInfo() string {
	return fmt.Sprintf("App version: %s. Git: %s - %s . Go Version: %s. OS: %s-%s ",
		cmd.Version, cmd.Branch, cmd.Commit, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

// Cleanup stops all app services.
func (app *App) Cleanup(ctx context.Context) {
	log.Info("app cleanup starting...")
	if app.fileLock != nil {
		if err := app.fileLock.Unlock(); err != nil {
			log.With().Error("failed to unlock file",
				log.String("path", app.fileLock.Path()),
				log.Err(err),
			)
		}
	}
	app.stopServices(ctx)
	// add any other Cleanup tasks here....
	log.Info("app cleanup completed")
}

// Wrap the top-level logger to add context info and set the level for a
// specific module.
func (app *App) addLogger(name string, logger log.Log) log.Log {
	lvl := zap.NewAtomicLevel()
	loggers, err := decodeLoggers(app.Config.LOGGING)
	if err != nil {
		app.log.With().Panic("unable to decode loggers into map[string]string", log.Err(err))
	}
	level, ok := loggers[name]
	if ok {
		if err := lvl.UnmarshalText([]byte(level)); err != nil {
			app.log.Error("cannot parse logging for %v error %v", name, err)
			lvl.SetLevel(log.DefaultLevel())
		}
	} else {
		lvl.SetLevel(log.DefaultLevel())
	}

	if logger.Check(lvl.Level()) {
		app.loggers[name] = &lvl
		logger = logger.SetLevel(&lvl)
	}
	return logger.WithName(name).WithFields(log.String("module", name))
}

func (app *App) getLevel(name string) log.Level {
	alvl, exist := app.loggers[name]
	if !exist {
		return 0
	}
	return alvl.Level()
}

// SetLogLevel updates the log level of an existing logger.
func (app *App) SetLogLevel(name, loglevel string) error {
	lvl, ok := app.loggers[name]
	if !ok {
		return fmt.Errorf("cannot find logger %v", name)
	}

	if err := lvl.UnmarshalText([]byte(loglevel)); err != nil {
		return fmt.Errorf("unmarshal text: %w", err)
	}

	return nil
}

func (app *App) initServices(ctx context.Context,
	nodeID types.NodeID,
	dbStorepath string,
	sgn *signing.EdSigner,
	layerSize uint32,
	poetClients []activation.PoetProvingServiceClient,
	vrfSigner *signing.VRFSigner,
	layersPerEpoch uint32, clock TickProvider,
) error {
	app.nodeID = nodeID

	lg := app.log.Named(nodeID.ShortString()).WithFields(nodeID)
	types.SetLayersPerEpoch(app.Config.LayersPerEpoch)

	app.log = app.addLogger(AppLogger, lg)

	sqlDB, err := sql.Open("file:" + filepath.Join(dbStorepath, "state.sql"))
	if err != nil {
		return fmt.Errorf("open sqlite db %w", err)
	}
	app.db = sqlDB
	if app.Config.CollectMetrics {
		app.dbMetrics = dbmetrics.NewDBMetricsCollector(ctx, sqlDB, app.addLogger(StateDbLogger, lg), 5*time.Minute)
	}

	cdb := datastore.NewCachedDB(sqlDB, app.addLogger(CachedDBLogger, lg))
	app.atxDB = *cdb
	poetDb := activation.NewPoetDb(sqlDB, app.addLogger(PoetDbLogger, lg))
	validator := activation.NewValidator(poetDb, app.Config.POST)

	if err := os.MkdirAll(dbStorepath, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create %s: %w", dbStorepath, err)
	}

	cfg := vm.DefaultConfig()
	cfg.GasLimit = app.Config.BlockGasLimit
	cfg.GenesisID = app.Config.Genesis.GenesisID()
	state := vm.New(sqlDB,
		vm.WithConfig(cfg),
		vm.WithLogger(app.addLogger(VMLogger, lg)))
	app.conState = txs.NewConservativeState(state, sqlDB,
		txs.WithCSConfig(txs.CSConfig{
			BlockGasLimit:      app.Config.BlockGasLimit,
			NumTXsPerProposal:  app.Config.TxsPerProposal,
			OptFilterThreshold: app.Config.OptFilterThreshold,
		}),
		txs.WithLogger(app.addLogger(ConStateLogger, lg)))

	genesisAccts := app.Config.Genesis.ToAccounts()
	if len(genesisAccts) > 0 {
		exists, err := state.AccountExists(genesisAccts[0].Address)
		if err != nil {
			return fmt.Errorf("failed to check genesis account %v: %w", genesisAccts[0].Address, err)
		}
		if !exists {
			if err = state.ApplyGenesis(genesisAccts); err != nil {
				return fmt.Errorf("setup genesis: %w", err)
			}
		}
	}

	goldenATXID := types.ATXID(app.Config.Genesis.GenesisID().ToHash32())
	if goldenATXID == *types.EmptyATXID {
		return errors.New("invalid golden atx id")
	}

	beaconProtocol := beacon.New(nodeID, app.host, sgn, vrfSigner, cdb, clock,
		beacon.WithContext(ctx),
		beacon.WithConfig(app.Config.Beacon),
		beacon.WithLogger(app.addLogger(BeaconLogger, lg)))

	trtlCfg := app.Config.Tortoise
	trtlCfg.LayerSize = layerSize
	trtlCfg.BadBeaconVoteDelayLayers = app.Config.LayersPerEpoch
	trtl := tortoise.New(cdb, beaconProtocol,
		tortoise.WithContext(ctx),
		tortoise.WithLogger(app.addLogger(TrtlLogger, lg)),
		tortoise.WithConfig(trtlCfg),
	)

	msh, err := mesh.NewMesh(cdb, trtl, app.conState, app.addLogger(MeshLogger, lg))
	if err != nil {
		return fmt.Errorf("failed to create mesh: %w", err)
	}

	fetcherWrapped := &layerFetcher{}
	atxHandler := activation.NewHandler(cdb, fetcherWrapped, layersPerEpoch, app.Config.TickSize, goldenATXID, validator, trtl, app.addLogger(ATXHandlerLogger, lg))

	// we can't have an epoch offset which is greater/equal than the number of layers in an epoch

	if app.Config.HareEligibility.EpochOffset >= app.Config.BaseConfig.LayersPerEpoch {
		return fmt.Errorf("epoch offset cannot be greater than or equal to the number of layers per epoch. epoch_offset: %d. layers_per_epoch: %d",
			app.Config.HareEligibility.EpochOffset, app.Config.BaseConfig.LayersPerEpoch)
	}

	proposalListener := proposals.NewHandler(cdb, fetcherWrapped, beaconProtocol, msh, trtl,
		proposals.WithLogger(app.addLogger(ProposalListenerLogger, lg)),
		proposals.WithConfig(proposals.Config{
			LayerSize:      layerSize,
			LayersPerEpoch: layersPerEpoch,
			GoldenATXID:    goldenATXID,
			MaxExceptions:  trtlCfg.MaxExceptions,
			Hdist:          trtlCfg.Hdist,
		}))

	blockHandler := blocks.NewHandler(fetcherWrapped, sqlDB, msh,
		blocks.WithLogger(app.addLogger(BlockHandlerLogger, lg)))

	txHandler := txs.NewTxHandler(app.conState, app.addLogger(TxHandlerLogger, lg))

	hOracle := eligibility.New(beaconProtocol, cdb, signing.VRFVerify, vrfSigner, app.Config.LayersPerEpoch, app.Config.HareEligibility, app.addLogger(HareOracleLogger, lg))
	// TODO: genesisMinerWeight is set to app.Config.SpaceToCommit, because PoET ticks are currently hardcoded to 1

	app.certifier = blocks.NewCertifier(sqlDB, hOracle, nodeID, sgn, app.host, clock, beaconProtocol, trtl,
		blocks.WithCertContext(ctx),
		blocks.WithCertConfig(blocks.CertConfig{
			CommitteeSize:    app.Config.HARE.N,
			CertifyThreshold: app.Config.HARE.F + 1,
			WaitSigLayers:    app.Config.Tortoise.Zdist,
			NumLayersToKeep:  app.Config.Tortoise.Zdist,
		}),
		blocks.WithCertifierLogger(app.addLogger(BlockCertLogger, lg)))

	fetcher := fetch.NewFetch(cdb, msh, beaconProtocol, app.host,
		fetch.WithContext(ctx),
		fetch.WithConfig(app.Config.FETCH),
		fetch.WithLogger(app.addLogger(Fetcher, lg)),
		fetch.WithATXHandler(atxHandler),
		fetch.WithBallotHandler(proposalListener),
		fetch.WithBlockHandler(blockHandler),
		fetch.WithProposalHandler(proposalListener),
		fetch.WithTXHandler(txHandler),
		fetch.WithPoetHandler(poetDb),
	)
	fetcherWrapped.Fetcher = fetcher

	patrol := layerpatrol.New()
	syncerConf := syncer.Config{
		SyncInterval:     time.Duration(app.Config.SyncInterval) * time.Second,
		HareDelayLayers:  app.Config.Tortoise.Zdist,
		SyncCertDistance: app.Config.Tortoise.Hdist,
		MaxHashesInReq:   100,
		MaxStaleDuration: time.Hour,
	}
	newSyncer := syncer.NewSyncer(cdb, clock, beaconProtocol, msh, fetcher, patrol, app.certifier,
		syncer.WithContext(ctx),
		syncer.WithConfig(syncerConf),
		syncer.WithLogger(app.addLogger(SyncLogger, lg)))
	// TODO(dshulyak) this needs to be improved, but dependency graph is a bit complicated
	beaconProtocol.SetSyncState(newSyncer)

	hareOutputCh := make(chan hare.LayerOutput, app.Config.HARE.LimitConcurrent)
	app.blockGen = blocks.NewGenerator(cdb, app.conState, msh, fetcherWrapped, app.certifier,
		blocks.WithContext(ctx),
		blocks.WithConfig(blocks.Config{
			LayerSize:      layerSize,
			LayersPerEpoch: layersPerEpoch,
		}),
		blocks.WithHareOutputChan(hareOutputCh),
		blocks.WithGeneratorLogger(app.addLogger(BlockGenLogger, lg)))
	app.hare = hare.New(
		sqlDB,
		app.Config.HARE,
		app.host.ID(),
		app.host,
		sgn,
		nodeID,
		hareOutputCh,
		newSyncer,
		beaconProtocol,
		hOracle,
		patrol,
		uint16(app.Config.LayersPerEpoch),
		hOracle,
		clock,
		app.addLogger(HareLogger, lg))

	proposalBuilder := miner.NewProposalBuilder(
		ctx,
		clock.Subscribe(),
		sgn,
		vrfSigner,
		cdb,
		app.host,
		trtl,
		beaconProtocol,
		newSyncer,
		app.conState,
		miner.WithMinerID(nodeID),
		miner.WithTxsPerProposal(app.Config.TxsPerProposal),
		miner.WithLayerSize(layerSize),
		miner.WithLayerPerEpoch(layersPerEpoch),
		miner.WithHdist(app.Config.Tortoise.Hdist),
		miner.WithLogger(app.addLogger(ProposalBuilderLogger, lg)))

	poetListener := activation.NewPoetListener(poetDb, app.addLogger(PoetListenerLogger, lg))

	postSetupMgr, err := activation.NewPostSetupManager(nodeID, app.Config.POST, app.addLogger(PostLogger, lg), cdb, goldenATXID)
	if err != nil {
		app.log.Panic("failed to create post setup manager: %v", err)
	}

	nipostBuilder := activation.NewNIPostBuilder(nodeID, postSetupMgr, poetClients, poetDb, sqlDB, app.addLogger(NipostBuilderLogger, lg), sgn)

	var coinbaseAddr types.Address
	if app.Config.SMESHING.Start {
		coinbaseAddr, err = types.StringToAddress(app.Config.SMESHING.CoinbaseAccount)
		if err != nil {
			app.log.Panic("failed to parse CoinbaseAccount address `%s`: %v", app.Config.SMESHING.CoinbaseAccount, err)
		}
		if coinbaseAddr.IsEmpty() {
			app.log.Panic("invalid coinbase account")
		}
	}

	builderConfig := activation.Config{
		CoinbaseAccount: coinbaseAddr,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}
	atxBuilder := activation.NewBuilder(builderConfig, nodeID, sgn, cdb, atxHandler, app.host, nipostBuilder,
		postSetupMgr, clock, newSyncer, app.addLogger("atxBuilder", lg),
		activation.WithContext(ctx),
		activation.WithPoetConfig(activation.PoetConfig{
			PhaseShift:  app.Config.POET.PhaseShift,
			CycleGap:    app.Config.POET.CycleGap,
			GracePeriod: app.Config.POET.GracePeriod,
		}))

	syncHandler := func(_ context.Context, _ p2p.Peer, _ []byte) pubsub.ValidationResult {
		if newSyncer.ListenToGossip() {
			return pubsub.ValidationAccept
		}
		return pubsub.ValidationIgnore
	}

	app.host.Register(pubsub.BeaconWeakCoinProtocol, pubsub.ChainGossipHandler(syncHandler, beaconProtocol.HandleWeakCoinProposal))
	app.host.Register(pubsub.BeaconProposalProtocol,
		pubsub.ChainGossipHandler(syncHandler, beaconProtocol.HandleProposal))
	app.host.Register(pubsub.BeaconFirstVotesProtocol,
		pubsub.ChainGossipHandler(syncHandler, beaconProtocol.HandleFirstVotes))
	app.host.Register(pubsub.BeaconFollowingVotesProtocol,
		pubsub.ChainGossipHandler(syncHandler, beaconProtocol.HandleFollowingVotes))
	app.host.Register(pubsub.ProposalProtocol, pubsub.ChainGossipHandler(syncHandler, proposalListener.HandleProposal))
	app.host.Register(pubsub.AtxProtocol, pubsub.ChainGossipHandler(
		func(_ context.Context, _ p2p.Peer, _ []byte) pubsub.ValidationResult {
			if newSyncer.ListenToATXGossip() {
				return pubsub.ValidationAccept
			}
			return pubsub.ValidationIgnore
		},
		atxHandler.HandleGossipAtx))
	app.host.Register(pubsub.TxProtocol, pubsub.ChainGossipHandler(syncHandler, txHandler.HandleGossipTransaction))
	app.host.Register(pubsub.PoetProofProtocol, poetListener.HandlePoetProofMessage)
	app.host.Register(pubsub.HareProtocol, pubsub.ChainGossipHandler(syncHandler, app.hare.GetHareMsgHandler()))
	app.host.Register(pubsub.BlockCertify, pubsub.ChainGossipHandler(syncHandler, app.certifier.HandleCertifyMessage))

	app.proposalBuilder = proposalBuilder
	app.proposalListener = proposalListener
	app.mesh = msh
	app.syncer = newSyncer
	app.clock = clock
	app.svm = state
	app.poetListener = poetListener
	app.atxBuilder = atxBuilder
	app.postSetupMgr = postSetupMgr
	app.atxHandler = atxHandler
	app.fetcher = fetcher
	app.beaconProtocol = beaconProtocol
	app.tortoise = trtl
	if !app.Config.TIME.Peersync.Disable {
		app.ptimesync = peersync.New(
			app.host,
			app.host,
			peersync.WithLog(app.addLogger(TimeSyncLogger, lg)),
			peersync.WithConfig(app.Config.TIME.Peersync),
		)
	}

	return nil
}

func (app *App) startServices(ctx context.Context) error {
	app.fetcher.Start()
	go app.startSyncer(ctx)
	app.beaconProtocol.Start(ctx)

	app.blockGen.Start()
	app.certifier.Start()
	if err := app.hare.Start(ctx); err != nil {
		return fmt.Errorf("cannot start hare: %w", err)
	}
	if err := app.proposalBuilder.Start(ctx); err != nil {
		return fmt.Errorf("cannot start block producer: %w", err)
	}

	if app.Config.SMESHING.Start {
		coinbaseAddr, err := types.StringToAddress(app.Config.SMESHING.CoinbaseAccount)
		if err != nil {
			app.log.Panic("failed to parse CoinbaseAccount address on start `%s`: %v", app.Config.SMESHING.CoinbaseAccount, err)
		}
		go func() {
			if err := app.atxBuilder.StartSmeshing(coinbaseAddr, app.Config.SMESHING.Opts); err != nil {
				log.Panic("failed to start smeshing: %v", err)
			}
		}()
	} else {
		log.Info("smeshing not started, waiting to be triggered via smesher api")
	}

	app.clock.StartNotifying()
	if app.ptimesync != nil {
		app.ptimesync.Start()
	}
	return nil
}

func (app *App) startAPIServices(ctx context.Context) {
	apiConf := &app.Config.API
	layerDuration := app.Config.LayerDurationSec

	// API SERVICES
	// Since we have multiple GRPC services, we cannot automatically enable them if
	// the gateway server is enabled (since we don't know which ones to enable), so
	// it's an error if the gateway server is enabled without enabling at least one
	// GRPC service.

	// Make sure we only create the server once.
	var services []grpcserver.ServiceAPI
	registerService := func(svc grpcserver.ServiceAPI) {
		if app.grpcAPIService == nil {
			app.addLogger(GRPCLogger, app.log)
			app.grpcAPIService = grpcserver.NewServerWithInterface(apiConf.GrpcServerPort, apiConf.GrpcServerInterface,
				grpcmw.WithStreamServerChain(
					grpctags.StreamServerInterceptor(),
					grpczap.StreamServerInterceptor(grpcLog)),
				grpcmw.WithUnaryServerChain(
					grpctags.UnaryServerInterceptor(),
					grpczap.UnaryServerInterceptor(grpcLog)),
			)
		}
		services = append(services, svc)
		svc.RegisterService(app.grpcAPIService)
	}

	// Register the requested services one by one
	if apiConf.StartDebugService {
		registerService(grpcserver.NewDebugService(app.conState, app.host))
	}
	if apiConf.StartGatewayService {
		verifier := activation.NewChallengeVerifier(&app.atxDB, signing.DefaultVerifier, app.Config.POST, types.ATXID(app.Config.Genesis.GenesisID().ToHash32()), app.Config.LayersPerEpoch)
		registerService(grpcserver.NewGatewayService(app.host, verifier))
	}
	if apiConf.StartGlobalStateService {
		registerService(grpcserver.NewGlobalStateService(app.mesh, app.conState))
	}
	if apiConf.StartMeshService {
		registerService(grpcserver.NewMeshService(app.mesh, app.conState, app.clock, app.Config.LayersPerEpoch, app.Config.Genesis.GenesisID(), layerDuration, app.Config.LayerAvgSize, app.Config.TxsPerProposal))
	}
	if apiConf.StartNodeService {
		nodeService := grpcserver.NewNodeService(app.host, app.mesh, app.clock, app.syncer, app.atxBuilder)
		registerService(nodeService)
	}
	if apiConf.StartSmesherService {
		registerService(grpcserver.NewSmesherService(app.postSetupMgr, app.atxBuilder, apiConf.SmesherStreamInterval))
	}
	if apiConf.StartTransactionService {
		registerService(grpcserver.NewTransactionService(app.db, app.host, app.mesh, app.conState, app.syncer))
	}
	if apiConf.StartActivationService {
		registerService(grpcserver.NewActivationService(&app.atxDB))
	}

	// Now that the services are registered, start the server.
	if app.grpcAPIService != nil {
		app.grpcAPIService.Start()
	}

	if apiConf.StartJSONServer {
		if app.grpcAPIService == nil {
			// This panics because it should not happen.
			// It should be caught inside apiConf.
			log.Fatal("one or more new grpc services must be enabled with new json gateway server")
		}
		app.jsonAPIService = grpcserver.NewJSONHTTPServer(apiConf.JSONServerPort)
		app.jsonAPIService.StartService(ctx, services...)
	}
}

func (app *App) stopServices(ctx context.Context) {
	if app.jsonAPIService != nil {
		log.Info("stopping json gateway service")
		if err := app.jsonAPIService.Shutdown(ctx); err != nil {
			log.With().Error("error stopping json gateway server", log.Err(err))
		}
	}

	if app.grpcAPIService != nil {
		log.Info("stopping grpc service")
		// does not return any errors
		_ = app.grpcAPIService.Close()
	}

	if app.proposalBuilder != nil {
		app.log.Info("closing proposal builder")
		app.proposalBuilder.Close()
	}

	if app.clock != nil {
		app.log.Info("closing clock")
		app.clock.Close()
	}

	if app.beaconProtocol != nil {
		app.log.Info("stopping beacon")
		app.beaconProtocol.Close()
	}

	if app.atxBuilder != nil {
		app.log.Info("closing atx builder")
		_ = app.atxBuilder.StopSmeshing(false)
	}

	if app.hare != nil {
		app.log.Info("closing hare")
		app.hare.Close()
	}

	if app.blockGen != nil {
		app.log.Info("stopping blockGen")
		app.blockGen.Stop()
	}

	if app.certifier != nil {
		app.log.Info("stopping certifier")
		app.certifier.Stop()
	}

	if app.fetcher != nil {
		app.log.Info("closing layerFetch")
		app.fetcher.Stop()
	}

	if app.syncer != nil {
		app.log.Info("closing sync")
		app.syncer.Close()
	}

	if app.ptimesync != nil {
		app.ptimesync.Stop()
		app.log.Debug("peer timesync stopped")
	}
	if app.tortoise != nil {
		app.log.Info("stopping tortoise. if tortoise is in rerun it may take a while")
		app.tortoise.Stop()
	}

	if app.host != nil {
		if err := app.host.Stop(); err != nil {
			app.log.With().Warning("p2p host exited with error", log.Err(err))
		}
	}
	if app.db != nil {
		if err := app.db.Close(); err != nil {
			app.log.With().Warning("db exited with error", log.Err(err))
		}
	}
	if app.dbMetrics != nil {
		app.dbMetrics.Close()
	}

	events.CloseEventReporter()
}

// LoadOrCreateEdSigner either loads a previously created ed identity for the node or creates a new one if not exists.
func (app *App) LoadOrCreateEdSigner() (*signing.EdSigner, error) {
	filename := filepath.Join(app.Config.SMESHING.Opts.DataDir, edKeyFileName)
	log.Info("Looking for identity file at `%v`", filename)

	data, err := os.ReadFile(filename)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to read identity file: %w", err)
		}

		log.Info("Identity file not found. Creating new identity...")

		edSgn := signing.NewEdSigner(signing.WithSignerPrefix(app.Config.Genesis.GenesisID().Bytes()))
		err := os.MkdirAll(filepath.Dir(filename), 0o700)
		if err != nil {
			return nil, fmt.Errorf("failed to create directory for identity file: %w", err)
		}
		err = os.WriteFile(filename, edSgn.ToBuffer(), 0o600)
		if err != nil {
			return nil, fmt.Errorf("failed to write identity file: %w", err)
		}

		log.With().Info("created new identity", edSgn.PublicKey())
		return edSgn, nil
	}
	edSgn, err := signing.NewEdSignerFromBuffer(data, signing.WithSignerPrefix(app.Config.Genesis.GenesisID().Bytes()))
	if err != nil {
		return nil, fmt.Errorf("failed to construct identity from data file: %w", err)
	}

	log.Info("Loaded existing identity; public key: %v", edSgn.PublicKey())

	return edSgn, nil
}

func (app *App) startSyncer(ctx context.Context) {
	app.log.With().Info("sync: waiting for p2p host to find outbound peers",
		log.Int("outbound", app.Config.P2P.TargetOutbound))
	_, err := app.host.WaitPeers(ctx, app.Config.P2P.TargetOutbound)
	if err != nil {
		return
	}
	app.log.Info("sync: waiting for tortoise to load state")
	if err := app.tortoise.WaitReady(ctx); err != nil {
		app.log.With().Error("sync: tortoise failed to load state", log.Err(err))
		return
	}
	app.syncer.Start(ctx)
}

// Start starts the Spacemesh node and initializes all relevant services according to command line arguments provided.
func (app *App) Start(ctx context.Context) error {
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
		log.String("hostname", hostname))

	if err := os.MkdirAll(app.Config.DataDir(), 0o700); err != nil {
		return fmt.Errorf("data-dir %s not found or could not be created: %w", app.Config.DataDir(), err)
	}

	/* Setup monitoring */
	pprofErr := make(chan error, 1)
	if app.Config.PprofHTTPServer {
		logger.Info("starting pprof server")
		srv := &http.Server{Addr: ":6060"}
		defer srv.Shutdown(ctx)
		go func() {
			if err := srv.ListenAndServe(); err != nil {
				pprofErr <- fmt.Errorf("cannot start pprof http server: %w", err)
			}
		}()
	}

	if app.Config.ProfilerURL != "" {
		p, err := profiler.Start(profiler.Config{
			ApplicationName: app.Config.ProfilerName,
			// app.Config.ProfilerURL should be the pyroscope server address
			// TODO: AuthToken? no need right now since server isn't public
			ServerAddress: app.Config.ProfilerURL,
			// by default all profilers are enabled,
		})
		if err != nil {
			return fmt.Errorf("cannot start profiling client: %w", err)
		}
		defer p.Stop()
	}

	/* Create or load miner identity */

	app.edSgn, err = app.LoadOrCreateEdSigner()
	if err != nil {
		return fmt.Errorf("could not retrieve identity: %w", err)
	}

	poetClients := make([]activation.PoetProvingServiceClient, 0, len(app.Config.PoETServers))
	for _, address := range app.Config.PoETServers {
		poetClients = append(poetClients, activation.NewHTTPPoetClient(address))
	}

	edPubkey := app.edSgn.PublicKey()
	vrfSigner := app.edSgn.VRFSigner()

	nodeID := types.BytesToNodeID(edPubkey.Bytes())

	lg := logger.Named(nodeID.ShortString()).WithFields(nodeID)

	/* Initialize all protocol services */

	dbStorepath := app.Config.DataDir()
	gTime, err := time.Parse(time.RFC3339, app.Config.Genesis.GenesisTime)
	if err != nil {
		return fmt.Errorf("cannot parse genesis time %s: %d", app.Config.Genesis.GenesisTime, err)
	}
	ld := time.Duration(app.Config.LayerDurationSec) * time.Second
	clock := timesync.NewClock(timesync.RealClock{}, ld, gTime, lg.WithName("clock"))

	lg.Info("initializing p2p services")

	cfg := app.Config.P2P
	cfg.DataDir = filepath.Join(app.Config.DataDir(), "p2p")
	p2plog := app.addLogger(P2PLogger, lg)
	// if addLogger won't add a level we will use a default 0 (info).
	cfg.LogLevel = app.getLevel(P2PLogger)
	app.host, err = p2p.New(ctx, p2plog, cfg, app.Config.Genesis.GenesisID(),
		p2p.WithNodeReporter(events.ReportNodeStatusUpdate),
	)
	if err != nil {
		return fmt.Errorf("failed to initialize p2p host: %w", err)
	}

	if err = app.initServices(ctx,
		nodeID,
		dbStorepath,
		app.edSgn,
		uint32(app.Config.LayerAvgSize),
		poetClients,
		vrfSigner,
		app.Config.LayersPerEpoch,
		clock); err != nil {
		return fmt.Errorf("cannot start services: %w", err)
	}

	if app.Config.CollectMetrics {
		metrics.StartMetricsServer(app.Config.MetricsPort)
	}

	if app.Config.MetricsPush != "" {
		metrics.StartPushingMetrics(app.Config.MetricsPush, app.Config.MetricsPushPeriod,
			app.host.ID().String(), app.Config.Genesis.GenesisID().ShortString())
	}

	if err := app.startServices(ctx); err != nil {
		return fmt.Errorf("error starting services: %w", err)
	}

	app.startAPIServices(ctx)

	events.SubscribeToLayers(clock.Subscribe())
	logger.Info("app started")

	// notify anyone who might be listening that the app has finished starting.
	// this can be used by, e.g., app tests.
	close(app.started)

	defer events.ReportError(events.NodeError{
		Msg:   "node is shutting down",
		Level: zapcore.InfoLevel,
	})
	syncErr := make(chan error, 1)
	if app.ptimesync != nil {
		go func() {
			syncErr <- app.ptimesync.Wait()
		}()
	}
	// app blocks until it receives a signal to exit
	// this signal may come from the node or from sig-abort (ctrl-c)
	select {
	case <-ctx.Done():
		return nil
	case err := <-pprofErr:
		return err
	case err := <-syncErr:
		return err
	}
}

type layerFetcher struct {
	system.Fetcher
}

func decodeLoggers(cfg config.LoggerConfig) (map[string]string, error) {
	rst := map[string]string{}
	if err := mapstructure.Decode(cfg, &rst); err != nil {
		return nil, fmt.Errorf("mapstructure decode: %w", err)
	}
	return rst, nil
}
