// Package node contains the main executable for go-spacemesh node
package node

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"syscall"
	"time"

	"github.com/gofrs/flock"
	"github.com/grafana/pyroscope-go"
	grpc_logsettable "github.com/grpc-ecosystem/go-grpc-middleware/logging/settable"
	grpczap "github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	grpctags "github.com/grpc-ecosystem/go-grpc-middleware/tags"
	"github.com/mitchellh/mapstructure"
	"github.com/spacemeshos/poet/server"
	"github.com/spacemeshos/post/verifying"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/beacon"
	"github.com/spacemeshos/go-spacemesh/blocks"
	"github.com/spacemeshos/go-spacemesh/bootstrap"
	"github.com/spacemeshos/go-spacemesh/checkpoint"
	"github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/config/presets"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/fetch"
	vm "github.com/spacemeshos/go-spacemesh/genvm"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/hare/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare3"
	"github.com/spacemeshos/go-spacemesh/hare3/compat"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/layerpatrol"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/malfeasance"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/metrics"
	"github.com/spacemeshos/go-spacemesh/metrics/public"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/node/mapstructureutil"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/handshake"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/proposals"
	"github.com/spacemeshos/go-spacemesh/prune"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/activesets"
	"github.com/spacemeshos/go-spacemesh/sql/ballots/util"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	dbmetrics "github.com/spacemeshos/go-spacemesh/sql/metrics"
	"github.com/spacemeshos/go-spacemesh/syncer"
	"github.com/spacemeshos/go-spacemesh/syncer/atxsync"
	"github.com/spacemeshos/go-spacemesh/syncer/blockssync"
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
	dbFile          = "state.sql"
)

// Logger names.
const (
	ClockLogger            = "clock"
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
	NipostBuilderLogger    = "nipostBuilder"
	NipostValidatorLogger  = "nipostValidator"
	Fetcher                = "fetcher"
	TimeSyncLogger         = "timesync"
	VMLogger               = "vm"
	GRPCLogger             = "grpc"
	ConStateLogger         = "conState"
	ExecutorLogger         = "executor"
	MalfeasanceLogger      = "malfeasance"
	BootstrapLogger        = "bootstrap"
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

			if cmd.NoMainNet && onMainNet(conf) {
				log.With().Fatal("this is a testnet-only build not intended for mainnet")
			}

			app := New(
				WithConfig(conf),
				// NOTE(dshulyak) this needs to be max level so that child logger can can be current level or below.
				// otherwise it will fail later when child logger will try to increase level.
				WithLog(log.RegisterHooks(
					log.NewWithLevel("node", zap.NewAtomicLevelAt(zap.DebugLevel)),
					events.EventHook()),
				),
			)

			run := func(ctx context.Context) error {
				types.SetLayersPerEpoch(app.Config.LayersPerEpoch)
				// ensure all data folders exist
				if err := os.MkdirAll(app.Config.DataDir(), 0o700); err != nil {
					return fmt.Errorf("ensure folders exist: %w", err)
				}

				if err := app.Lock(); err != nil {
					return fmt.Errorf("failed to get exclusive file lock: %w", err)
				}
				defer app.Unlock()

				if err := app.Initialize(); err != nil {
					return err
				}

				/* Create or load miner identity */
				if app.edSgn, err = app.LoadOrCreateEdSigner(); err != nil {
					return fmt.Errorf("could not retrieve identity: %w", err)
				}

				app.preserve, err = app.LoadCheckpoint(ctx)
				if err != nil {
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
					_ = app.eg.Wait()
					close(done)
				}()
				select {
				case <-done:
				case <-cleanupCtx.Done():
					app.log.With().Error("app failed to clean up in time")
				}
				return err
			}
			// os.Interrupt for all systems, especially windows, syscall.SIGTERM is mainly for docker.
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			if err = run(ctx); err != nil {
				app.log.With().Fatal(err.Error())
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
	grpclog grpc_logsettable.SettableLoggerV2
)

func init() {
	appLog = log.NewNop()
	grpclog = grpc_logsettable.ReplaceGrpcLoggerV2()
}

func loadConfig(c *cobra.Command) (*config.Config, error) {
	conf, err := LoadConfigFromFile()
	if err != nil {
		return nil, err
	}
	if err := cmd.EnsureCLIFlags(c, conf); err != nil {
		return nil, fmt.Errorf("mapping cli flags to config: %w", err)
	}
	return conf, nil
}

// LoadConfigFromFile tries to load configuration file if the config parameter was specified.
func LoadConfigFromFile() (*config.Config, error) {
	// read in default config if passed as param using viper
	if err := config.LoadConfig(viper.GetString("config"), viper.GetViper()); err != nil {
		return nil, err
	}
	conf := config.MainnetConfig()
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
		mapstructureutil.AddressListDecodeFunc(),
		mapstructureutil.BigRatDecodeFunc(),
		mapstructureutil.PostProviderIDDecodeFunc(),
		mapstructure.TextUnmarshallerHookFunc(),
	)

	// load config if it was loaded to the viper
	if err := viper.Unmarshal(&conf, viper.DecodeHook(hook), withZeroFields()); err != nil {
		return nil, fmt.Errorf("unmarshal viper: %w", err)
	}
	return &conf, nil
}

func withZeroFields() viper.DecoderConfigOption {
	return func(cfg *mapstructure.DecoderConfig) {
		cfg.ZeroFields = true
	}
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
		eg:      &errgroup.Group{},
	}
	for _, opt := range opts {
		opt(app)
	}
	// TODO(mafa): this is a hack to suppress debugging logs on 0000.defaultLogger
	// to fix this we should get rid of the global logger and pass app.log to all
	// components that need it
	lvl := zap.NewAtomicLevelAt(zap.InfoLevel)
	log.SetupGlobal(app.log.SetLevel(&lvl))

	types.SetNetworkHRP(app.Config.NetworkHRP)
	return app
}

// App is the cli app singleton.
type App struct {
	*cobra.Command
	fileLock           *flock.Flock
	edSgn              *signing.EdSigner
	Config             *config.Config
	db                 *sql.Database
	dbMetrics          *dbmetrics.DBMetricsCollector
	grpcPublicService  *grpcserver.Server
	grpcPrivateService *grpcserver.Server
	jsonAPIService     *grpcserver.JSONHTTPServer
	pprofService       *http.Server
	profilerService    *pyroscope.Profiler
	syncer             *syncer.Syncer
	proposalListener   *proposals.Handler
	proposalBuilder    *miner.ProposalBuilder
	mesh               *mesh.Mesh
	cachedDB           *datastore.CachedDB
	clock              *timesync.NodeClock
	hare               *hare.Hare
	hare3              *hare3.Hare
	hOracle            *eligibility.Oracle
	blockGen           *blocks.Generator
	certifier          *blocks.Certifier
	postSetupMgr       *activation.PostSetupManager
	atxBuilder         *activation.Builder
	atxHandler         *activation.Handler
	txHandler          *txs.TxHandler
	validator          *activation.Validator
	edVerifier         *signing.EdVerifier
	beaconProtocol     *beacon.ProtocolDriver
	log                log.Log
	svm                *vm.VM
	conState           *txs.ConservativeState
	fetcher            *fetch.Fetch
	ptimesync          *peersync.Sync
	tortoise           *tortoise.Tortoise
	updater            *bootstrap.Updater
	poetDb             *activation.PoetDb
	postVerifier       *activation.OffloadingPostVerifier
	preserve           *checkpoint.PreservedData
	errCh              chan error

	host *p2p.Host

	loggers map[string]*zap.AtomicLevel
	started chan struct{} // this channel is closed once the app has finished starting
	eg      *errgroup.Group
}

func (app *App) LoadCheckpoint(ctx context.Context) (*checkpoint.PreservedData, error) {
	checkpointFile := app.Config.Recovery.Uri
	restore := types.LayerID(app.Config.Recovery.Restore)
	if len(checkpointFile) == 0 {
		return nil, nil
	}
	if restore == 0 {
		return nil, fmt.Errorf("restore layer not set")
	}
	cfg := &checkpoint.RecoverConfig{
		GoldenAtx:      types.ATXID(app.Config.Genesis.GoldenATX()),
		PostDataDir:    app.Config.SMESHING.Opts.DataDir,
		DataDir:        app.Config.DataDir(),
		DbFile:         dbFile,
		PreserveOwnAtx: app.Config.Recovery.PreserveOwnAtx,
		NodeID:         app.edSgn.NodeID(),
		Uri:            checkpointFile,
		Restore:        restore,
	}
	app.log.WithContext(ctx).With().Info("recover from checkpoint",
		log.String("url", checkpointFile),
		log.Stringer("restore", restore),
	)
	return checkpoint.Recover(ctx, app.log, afero.NewOsFs(), cfg)
}

func (app *App) Started() <-chan struct{} {
	return app.started
}

// Lock locks the app for exclusive use. It returns an error if the app is already locked.
func (app *App) Lock() error {
	lockdir := filepath.Dir(app.Config.FileLock)
	if _, err := os.Stat(lockdir); errors.Is(err, os.ErrNotExist) {
		err := os.Mkdir(lockdir, os.ModePerm)
		if err != nil {
			return fmt.Errorf("creating dir %s for lock %s: %w", lockdir, app.Config.FileLock, err)
		}
	}
	fl := flock.New(app.Config.FileLock)
	locked, err := fl.TryLock()
	if err != nil {
		return fmt.Errorf("flock %s: %w", app.Config.FileLock, err)
	} else if !locked {
		return fmt.Errorf("only one spacemesh instance should be running (locking file %s)", fl.Path())
	}
	app.fileLock = fl
	return nil
}

// Unlock unlocks the app. It is a no-op if the app is not locked.
func (app *App) Unlock() {
	if app.fileLock == nil {
		return
	}
	if err := app.fileLock.Unlock(); err != nil {
		app.log.With().Error("failed to unlock file",
			log.String("path", app.fileLock.Path()),
			log.Err(err),
		)
	}
}

// Initialize parses and validates the node configuration and sets up logging.
func (app *App) Initialize() error {
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

	// tortoise wait zdist layers for hare to timeout for a layer. once hare timeout, tortoise will
	// vote against all blocks in that layer. so it's important to make sure zdist takes longer than
	// hare's max time duration to run consensus for a layer
	maxHareRoundsPerLayer := 1 + app.Config.HARE.LimitIterations*hare.RoundsPerIteration // pre-round + 4 rounds per iteration
	maxHareLayerDuration := app.Config.HARE.WakeupDelta + time.Duration(
		maxHareRoundsPerLayer,
	)*app.Config.HARE.RoundDuration
	if app.Config.LayerDuration*time.Duration(app.Config.Tortoise.Zdist) <= maxHareLayerDuration {
		app.log.With().Error("incompatible params",
			log.Uint32("tortoise_zdist", app.Config.Tortoise.Zdist),
			log.Duration("layer_duration", app.Config.LayerDuration),
			log.Duration("hare_wakeup_delta", app.Config.HARE.WakeupDelta),
			log.Int("hare_limit_iterations", app.Config.HARE.LimitIterations),
			log.Duration("hare_round_duration", app.Config.HARE.RoundDuration))

		return errors.New("incompatible tortoise hare params")
	}

	// override default config in timesync since timesync is using TimeConfigValues
	timeCfg.TimeConfigValues = app.Config.TIME

	app.setupLogging()
	app.log.Info("Welcome to Spacemesh. Spacemesh full node is starting...")

	public.Version.WithLabelValues(cmd.Version).Set(1)
	public.SmeshingOptsProvingNonces.Set(float64(app.Config.SMESHING.ProvingOpts.Nonces))
	public.SmeshingOptsProvingThreads.Set(float64(app.Config.SMESHING.ProvingOpts.Threads))
	return nil
}

// setupLogging configured the app logging system.
func (app *App) setupLogging() {
	app.log.Info("%s", app.getAppInfo())
	events.InitializeReporter()
}

func (app *App) getAppInfo() string {
	return fmt.Sprintf("App version: %s. Git: %s - %s . Go Version: %s. OS: %s-%s ",
		cmd.Version, cmd.Branch, cmd.Commit, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

// Cleanup stops all app services.
func (app *App) Cleanup(ctx context.Context) {
	app.log.Info("app cleanup starting...")
	app.stopServices(ctx)
	// add any other Cleanup tasks here....
	app.log.Info("app cleanup completed")
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

func (app *App) initServices(ctx context.Context) error {
	vrfSigner := app.edSgn.VRFSigner()
	layerSize := app.Config.LayerAvgSize
	layersPerEpoch := types.GetLayersPerEpoch()
	lg := app.log.Named(app.edSgn.NodeID().ShortString()).WithFields(app.edSgn.NodeID())

	poetDb := activation.NewPoetDb(app.db, app.addLogger(PoetDbLogger, lg))

	nipostValidatorLogger := app.addLogger(NipostValidatorLogger, lg)

	lg.Debug("creating post verifier")
	verifier, err := activation.NewPostVerifier(
		app.Config.POST,
		nipostValidatorLogger.Zap(),
		verifying.WithPowFlags(app.Config.SMESHING.VerifyingOpts.Flags),
	)
	lg.With().Debug("created post verifier", log.Err(err))
	if err != nil {
		return err
	}
	minWorkers := app.Config.SMESHING.VerifyingOpts.MinWorkers
	workers := app.Config.SMESHING.VerifyingOpts.Workers
	app.postVerifier = activation.NewOffloadingPostVerifier(verifier, workers, nipostValidatorLogger.Zap())
	app.postVerifier.Autoscale(minWorkers, workers)

	validator := activation.NewValidator(
		poetDb,
		app.Config.POST,
		app.Config.SMESHING.Opts.Scrypt,
		nipostValidatorLogger,
		app.postVerifier,
	)
	app.validator = validator

	cfg := vm.DefaultConfig()
	cfg.GasLimit = app.Config.BlockGasLimit
	cfg.GenesisID = app.Config.Genesis.GenesisID()
	state := vm.New(app.db,
		vm.WithConfig(cfg),
		vm.WithLogger(app.addLogger(VMLogger, lg)))
	app.conState = txs.NewConservativeState(state, app.db,
		txs.WithCSConfig(txs.CSConfig{
			BlockGasLimit:     app.Config.BlockGasLimit,
			NumTXsPerProposal: app.Config.TxsPerProposal,
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

	goldenATXID := types.ATXID(app.Config.Genesis.GoldenATX())
	if goldenATXID == types.EmptyATXID {
		return errors.New("invalid golden atx id")
	}

	app.edVerifier, err = signing.NewEdVerifier(signing.WithVerifierPrefix(app.Config.Genesis.GenesisID().Bytes()))
	if err != nil {
		return fmt.Errorf("failed to create signature verifier: %w", err)
	}

	vrfVerifier := signing.NewVRFVerifier()
	beaconProtocol := beacon.New(app.host, app.edSgn, app.edVerifier, vrfVerifier, app.cachedDB, app.clock,
		beacon.WithContext(ctx),
		beacon.WithConfig(app.Config.Beacon),
		beacon.WithLogger(app.addLogger(BeaconLogger, lg)),
	)

	trtlCfg := app.Config.Tortoise
	trtlCfg.LayerSize = layerSize
	if trtlCfg.BadBeaconVoteDelayLayers == 0 {
		trtlCfg.BadBeaconVoteDelayLayers = app.Config.LayersPerEpoch
	}
	trtlopts := []tortoise.Opt{
		tortoise.WithLogger(app.addLogger(TrtlLogger, lg)),
		tortoise.WithConfig(trtlCfg),
	}
	if trtlCfg.EnableTracer {
		app.log.With().Info("tortoise will trace execution")
		trtlopts = append(trtlopts, tortoise.WithTracer())
	}
	start := time.Now()
	trtl, err := tortoise.Recover(
		ctx,
		app.cachedDB,
		app.clock.CurrentLayer(), trtlopts...,
	)
	if err != nil {
		return fmt.Errorf("can't recover tortoise state: %w", err)
	}
	app.log.With().Info("tortoise initialized", log.Duration("duration", time.Since(start)))
	app.eg.Go(func() error {
		for rst := range beaconProtocol.Results() {
			events.EmitBeacon(rst.Epoch, rst.Beacon)
			trtl.OnBeacon(rst.Epoch, rst.Beacon)
		}
		app.log.Debug("beacon results watcher exited")
		return nil
	})

	executor := mesh.NewExecutor(app.cachedDB, state, app.conState, app.addLogger(ExecutorLogger, lg))
	mlog := app.addLogger(MeshLogger, lg)
	msh, err := mesh.NewMesh(app.cachedDB, app.clock, trtl, executor, app.conState, mlog)
	if err != nil {
		return fmt.Errorf("failed to create mesh: %w", err)
	}

	app.eg.Go(func() error {
		prune.Prune(ctx, mlog.Zap(), app.db, app.clock, app.Config.Tortoise.Hdist, app.Config.DatabasePruneInterval)
		return nil
	})

	fetcherWrapped := &layerFetcher{}
	atxHandler := activation.NewHandler(
		app.host.ID(),
		app.cachedDB,
		app.edVerifier,
		app.clock,
		app.host,
		fetcherWrapped,
		app.Config.TickSize,
		goldenATXID,
		validator,
		beaconProtocol,
		trtl,
		app.addLogger(ATXHandlerLogger, lg),
		app.Config.POET,
	)

	// we can't have an epoch offset which is greater/equal than the number of layers in an epoch

	if app.Config.HareEligibility.ConfidenceParam >= app.Config.BaseConfig.LayersPerEpoch {
		return fmt.Errorf(
			"confidence param should be smaller than layers per epoch. eligibility-confidence-param: %d. layers-per-epoch: %d",
			app.Config.HareEligibility.ConfidenceParam,
			app.Config.BaseConfig.LayersPerEpoch,
		)
	}

	proposalListener := proposals.NewHandler(
		app.cachedDB,
		app.edVerifier,
		app.host,
		fetcherWrapped,
		beaconProtocol,
		msh,
		trtl,
		vrfVerifier,
		app.clock,
		proposals.WithLogger(app.addLogger(ProposalListenerLogger, lg)),
		proposals.WithConfig(proposals.Config{
			LayerSize:              layerSize,
			LayersPerEpoch:         layersPerEpoch,
			GoldenATXID:            goldenATXID,
			MaxExceptions:          trtlCfg.MaxExceptions,
			Hdist:                  trtlCfg.Hdist,
			MinimalActiveSetWeight: trtlCfg.MinimalActiveSetWeight,
		}),
	)

	blockHandler := blocks.NewHandler(fetcherWrapped, app.db, trtl, msh,
		blocks.WithLogger(app.addLogger(BlockHandlerLogger, lg)))

	app.txHandler = txs.NewTxHandler(
		app.conState,
		app.host.ID(),
		app.addLogger(TxHandlerLogger, lg),
	)

	app.hOracle = eligibility.New(
		beaconProtocol,
		app.cachedDB,
		vrfVerifier,
		vrfSigner,
		app.Config.LayersPerEpoch,
		app.Config.HareEligibility,
		app.addLogger(HareOracleLogger, lg),
	)
	// TODO: genesisMinerWeight is set to app.Config.SpaceToCommit, because PoET ticks are currently hardcoded to 1

	bscfg := app.Config.Bootstrap
	bscfg.DataDir = app.Config.DataDir()
	bscfg.Interval = app.Config.LayerDuration / 5
	app.updater = bootstrap.New(
		app.clock,
		bootstrap.WithConfig(bscfg),
		bootstrap.WithLogger(app.addLogger(BootstrapLogger, lg)),
	)

	app.certifier = blocks.NewCertifier(
		app.cachedDB,
		app.hOracle,
		app.edSgn.NodeID(),
		app.edSgn,
		app.edVerifier,
		app.host,
		app.clock,
		beaconProtocol,
		trtl,
		blocks.WithCertContext(ctx),
		blocks.WithCertConfig(blocks.CertConfig{
			CommitteeSize:    app.Config.HARE.N,
			CertifyThreshold: app.Config.HARE.N/2 + 1,
			LayerBuffer:      app.Config.Tortoise.Zdist,
			NumLayersToKeep:  app.Config.Tortoise.Zdist * 2,
		}),
		blocks.WithCertifierLogger(app.addLogger(BlockCertLogger, lg)),
	)

	flog := app.addLogger(Fetcher, lg)
	fetcher := fetch.NewFetch(app.cachedDB, msh, beaconProtocol, app.host,
		fetch.WithContext(ctx),
		fetch.WithConfig(app.Config.FETCH),
		fetch.WithLogger(flog),
	)
	fetcherWrapped.Fetcher = fetcher
	app.eg.Go(func() error {
		return blockssync.Sync(ctx, flog.Zap(), msh.MissingBlocks(), fetcher)
	})

	patrol := layerpatrol.New()
	syncerConf := app.Config.Sync
	syncerConf.HareDelayLayers = app.Config.Tortoise.Zdist
	syncerConf.SyncCertDistance = app.Config.Tortoise.Hdist
	syncerConf.Standalone = app.Config.Standalone
	newSyncer := syncer.NewSyncer(app.cachedDB, app.clock, beaconProtocol, msh, trtl, fetcher, patrol, app.certifier,
		syncer.WithConfig(syncerConf),
		syncer.WithLogger(app.addLogger(SyncLogger, lg)),
	)
	// TODO(dshulyak) this needs to be improved, but dependency graph is a bit complicated
	beaconProtocol.SetSyncState(newSyncer)
	app.hOracle.SetSync(newSyncer)

	hareOutputCh := make(chan hare.LayerOutput, app.Config.HARE.LimitConcurrent)
	app.blockGen = blocks.NewGenerator(app.cachedDB, executor, msh, fetcherWrapped, app.certifier, patrol,
		blocks.WithContext(ctx),
		blocks.WithConfig(blocks.Config{
			BlockGasLimit:      app.Config.BlockGasLimit,
			OptFilterThreshold: app.Config.OptFilterThreshold,
			GenBlockInterval:   500 * time.Millisecond,
		}),
		blocks.WithHareOutputChan(hareOutputCh),
		blocks.WithGeneratorLogger(app.addLogger(BlockGenLogger, lg)))

	hareCfg := app.Config.HARE
	hareCfg.Hdist = app.Config.Tortoise.Hdist
	app.hare = hare.New(
		app.cachedDB,
		hareCfg,
		app.host,
		app.edSgn,
		app.edVerifier,
		app.edSgn.NodeID(),
		hareOutputCh,
		newSyncer,
		beaconProtocol,
		app.hOracle,
		patrol,
		app.hOracle,
		app.clock,
		tortoiseWeakCoin{db: app.cachedDB, tortoise: trtl},
		app.addLogger(HareLogger, lg),
	)
	if app.Config.HARE3.Enable {
		if err := app.Config.HARE3.Validate(time.Duration(app.Config.Tortoise.Zdist) * app.Config.LayerDuration); err != nil {
			return err
		}
		logger := app.addLogger(HareLogger, lg).Zap()
		app.hare3 = hare3.New(
			app.clock, app.host, app.cachedDB, app.edVerifier, app.hOracle, newSyncer, patrol,
			hare3.WithLogger(logger),
			hare3.WithConfig(app.Config.HARE3),
		)
		app.hare3.Register(app.edSgn)
		app.hare3.Start()
		app.eg.Go(func() error {
			compat.ReportWeakcoin(ctx, logger, app.hare3.Coins(), tortoiseWeakCoin{db: app.cachedDB, tortoise: trtl})
			return nil
		})
		app.eg.Go(func() error {
			compat.ReportResult(ctx, logger, app.hare3.Results(), hareOutputCh)
			return nil
		})
	}

	minerGoodAtxPct := 90
	if app.Config.MinerGoodAtxsPercent > 0 {
		minerGoodAtxPct = app.Config.MinerGoodAtxsPercent
	}
	proposalBuilder := miner.New(
		app.clock,
		app.edSgn,
		app.cachedDB,
		app.host,
		trtl,
		newSyncer,
		app.conState,
		miner.WithLayerSize(layerSize),
		miner.WithLayerPerEpoch(layersPerEpoch),
		miner.WithMinimalActiveSetWeight(app.Config.Tortoise.MinimalActiveSetWeight),
		miner.WithHdist(app.Config.Tortoise.Hdist),
		miner.WithNetworkDelay(app.Config.ATXGradeDelay),
		miner.WithMinGoodAtxPercent(minerGoodAtxPct),
		miner.WithLogger(app.addLogger(ProposalBuilderLogger, lg)),
	)

	postSetupMgr, err := activation.NewPostSetupManager(
		app.edSgn.NodeID(),
		app.Config.POST,
		app.addLogger(PostLogger, lg),
		app.cachedDB, goldenATXID,
		app.Config.SMESHING.ProvingOpts,
	)
	if err != nil {
		app.log.Panic("failed to create post setup manager: %v", err)
	}

	nipostBuilder, err := activation.NewNIPostBuilder(
		app.edSgn.NodeID(),
		postSetupMgr,
		poetDb,
		app.Config.PoETServers,
		app.Config.SMESHING.Opts.DataDir,
		app.addLogger(NipostBuilderLogger, lg),
		app.edSgn,
		app.Config.POET,
		app.clock,
		activation.WithNipostValidator(app.validator),
	)
	if err != nil {
		app.log.Panic("failed to create nipost builder: %v", err)
	}

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
		CoinbaseAccount:  coinbaseAddr,
		GoldenATXID:      goldenATXID,
		LayersPerEpoch:   layersPerEpoch,
		RegossipInterval: app.Config.RegossipAtxInterval,
	}
	atxBuilder := activation.NewBuilder(
		builderConfig,
		app.edSgn.NodeID(),
		app.edSgn,
		app.cachedDB,
		app.host,
		nipostBuilder,
		postSetupMgr,
		app.clock,
		newSyncer,
		app.addLogger("atxBuilder", lg),
		activation.WithContext(ctx),
		activation.WithPoetConfig(app.Config.POET),
		activation.WithPoetRetryInterval(app.Config.HARE.WakeupDelta),
		activation.WithValidator(app.validator),
	)

	malfeasanceHandler := malfeasance.NewHandler(
		app.cachedDB,
		app.addLogger(MalfeasanceLogger, lg),
		app.host.ID(),
		app.edSgn.NodeID(),
		app.hare,
		app.edVerifier,
		trtl,
	)
	fetcher.SetValidators(
		fetch.ValidatorFunc(pubsub.DropPeerOnSyncValidationReject(atxHandler.HandleSyncedAtx, app.host, lg)),
		fetch.ValidatorFunc(pubsub.DropPeerOnSyncValidationReject(poetDb.ValidateAndStoreMsg, app.host, lg)),
		fetch.ValidatorFunc(pubsub.DropPeerOnSyncValidationReject(proposalListener.HandleSyncedBallot, app.host, lg)),
		fetch.ValidatorFunc(pubsub.DropPeerOnSyncValidationReject(proposalListener.HandleActiveSet, app.host, lg)),
		fetch.ValidatorFunc(pubsub.DropPeerOnSyncValidationReject(blockHandler.HandleSyncedBlock, app.host, lg)),
		fetch.ValidatorFunc(pubsub.DropPeerOnSyncValidationReject(proposalListener.HandleSyncedProposal, app.host, lg)),
		fetch.ValidatorFunc(pubsub.DropPeerOnSyncValidationReject(app.txHandler.HandleBlockTransaction, app.host, lg)),
		fetch.ValidatorFunc(
			pubsub.DropPeerOnSyncValidationReject(app.txHandler.HandleProposalTransaction, app.host, lg),
		),
		fetch.ValidatorFunc(
			pubsub.DropPeerOnSyncValidationReject(malfeasanceHandler.HandleSyncedMalfeasanceProof, app.host, lg),
		),
	)

	syncHandler := func(_ context.Context, _ p2p.Peer, _ []byte) error {
		if newSyncer.ListenToGossip() {
			return nil
		}
		return errors.New("not synced for gossip")
	}
	atxSyncHandler := func(_ context.Context, _ p2p.Peer, _ []byte) error {
		if newSyncer.ListenToATXGossip() {
			return nil
		}
		return errors.New("not synced for gossip")
	}

	if app.Config.Beacon.RoundsNumber > 0 {
		app.host.Register(
			pubsub.BeaconWeakCoinProtocol,
			pubsub.ChainGossipHandler(syncHandler, beaconProtocol.HandleWeakCoinProposal),
			pubsub.WithValidatorInline(true),
		)
		app.host.Register(
			pubsub.BeaconProposalProtocol,
			pubsub.ChainGossipHandler(syncHandler, beaconProtocol.HandleProposal),
			pubsub.WithValidatorInline(true),
		)
		app.host.Register(
			pubsub.BeaconFirstVotesProtocol,
			pubsub.ChainGossipHandler(syncHandler, beaconProtocol.HandleFirstVotes),
			pubsub.WithValidatorInline(true),
		)
		app.host.Register(
			pubsub.BeaconFollowingVotesProtocol,
			pubsub.ChainGossipHandler(syncHandler, beaconProtocol.HandleFollowingVotes),
			pubsub.WithValidatorInline(true),
		)
	}
	app.host.Register(
		pubsub.ProposalProtocol,
		pubsub.ChainGossipHandler(syncHandler, proposalListener.HandleProposal),
	)
	app.host.Register(
		pubsub.AtxProtocol,
		pubsub.ChainGossipHandler(atxSyncHandler, atxHandler.HandleGossipAtx),
		pubsub.WithValidatorConcurrency(app.Config.P2P.GossipAtxValidationThrottle),
	)
	app.host.Register(
		pubsub.TxProtocol,
		pubsub.ChainGossipHandler(syncHandler, app.txHandler.HandleGossipTransaction),
	)
	app.host.Register(
		pubsub.HareProtocol,
		pubsub.ChainGossipHandler(syncHandler, app.hare.GetHareMsgHandler()),
	)
	app.host.Register(
		pubsub.BlockCertify,
		pubsub.ChainGossipHandler(syncHandler, app.certifier.HandleCertifyMessage),
	)
	app.host.Register(
		pubsub.MalfeasanceProof,
		pubsub.ChainGossipHandler(atxSyncHandler, malfeasanceHandler.HandleMalfeasanceProof),
	)

	app.proposalBuilder = proposalBuilder
	app.proposalListener = proposalListener
	app.mesh = msh
	app.syncer = newSyncer
	app.svm = state
	app.atxBuilder = atxBuilder
	app.postSetupMgr = postSetupMgr
	app.atxHandler = atxHandler
	app.poetDb = poetDb
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
	if err := app.host.Start(); err != nil {
		return err
	}
	return nil
}

func (app *App) launchStandalone(ctx context.Context) error {
	if !app.Config.Standalone {
		return nil
	}
	if len(app.Config.PoETServers) != 1 {
		return fmt.Errorf(
			"to launch in a standalone mode provide single local address for poet: %v",
			app.Config.PoETServers,
		)
	}
	value := types.Beacon{}
	genesis := app.Config.Genesis.GenesisID()
	copy(value[:], genesis[:])
	epoch := types.GetEffectiveGenesis().GetEpoch() + 1
	app.log.With().Warning("using standalone mode for bootstrapping beacon",
		log.Uint32("epoch", epoch.Uint32()),
		log.Stringer("beacon", value),
	)
	if err := app.beaconProtocol.UpdateBeacon(epoch, value); err != nil {
		return fmt.Errorf("update standalone beacon: %w", err)
	}
	cfg := server.DefaultConfig()
	cfg.PoetDir = filepath.Join(app.Config.DataDir(), "poet")

	parsed, err := url.Parse(app.Config.PoETServers[0])
	if err != nil {
		return err
	}
	cfg.RawRESTListener = parsed.Host
	cfg.Genesis.UnmarshalFlag(app.Config.Genesis.GenesisTime)
	cfg.Round.EpochDuration = app.Config.LayerDuration * time.Duration(app.Config.LayersPerEpoch)
	cfg.Round.CycleGap = app.Config.POET.CycleGap
	cfg.Round.PhaseShift = app.Config.POET.PhaseShift
	server.SetupConfig(cfg)

	srv, err := server.New(ctx, *cfg)
	if err != nil {
		return fmt.Errorf("init poet server: %w", err)
	}
	app.log.With().Warning("launching poet in standalone mode", log.Any("config", cfg))
	app.eg.Go(func() error {
		if err := srv.Start(ctx); err != nil {
			app.log.With().Error("poet server failed", log.Err(err))
			return err
		}
		return srv.Close()
	})
	return nil
}

func (app *App) listenToUpdates(ctx context.Context) {
	app.eg.Go(func() error {
		ch, err := app.updater.Subscribe()
		if err != nil {
			app.errCh <- err
			return nil
		}
		if err := app.updater.Start(); err != nil {
			app.errCh <- err
			return nil
		}
		for {
			select {
			case <-ctx.Done():
				return nil
			case update, ok := <-ch:
				if !ok {
					return nil
				}
				if update.Data.Beacon != types.EmptyBeacon {
					if err := app.beaconProtocol.UpdateBeacon(update.Data.Epoch, update.Data.Beacon); err != nil {
						app.errCh <- err
						return nil
					}
				}
				if len(update.Data.ActiveSet) > 0 {
					epoch := update.Data.Epoch
					set := update.Data.ActiveSet
					sort.Slice(set, func(i, j int) bool {
						return bytes.Compare(set[i].Bytes(), set[j].Bytes()) < 0
					})
					id := types.ATXIDList(set).Hash()
					activeSet := &types.EpochActiveSet{
						Epoch: epoch,
						Set:   set,
					}
					activesets.Add(app.db, id, activeSet)

					app.hOracle.UpdateActiveSet(epoch, set)
					app.proposalBuilder.UpdateActiveSet(epoch, set)

					app.eg.Go(func() error {
						if err := atxsync.Download(
							ctx,
							10*time.Second,
							app.addLogger(SyncLogger, app.log).Zap(),
							app.db,
							app.fetcher,
							set,
						); err != nil {
							app.errCh <- err
						}
						return nil
					})
				}
			}
		}
	})
}

func (app *App) startServices(ctx context.Context) error {
	if err := app.fetcher.Start(); err != nil {
		return fmt.Errorf("failed to start fetcher: %w", err)
	}
	app.syncer.Start()
	app.beaconProtocol.Start(ctx)

	app.blockGen.Start()
	app.certifier.Start()
	if err := app.hare.Start(ctx); err != nil {
		return fmt.Errorf("cannot start hare: %w", err)
	}
	app.eg.Go(func() error {
		return app.proposalBuilder.Run(ctx)
	})

	if app.Config.SMESHING.Start {
		coinbaseAddr, err := types.StringToAddress(app.Config.SMESHING.CoinbaseAccount)
		if err != nil {
			app.log.Panic(
				"failed to parse CoinbaseAccount address on start `%s`: %v",
				app.Config.SMESHING.CoinbaseAccount,
				err,
			)
		}
		if err := app.atxBuilder.StartSmeshing(coinbaseAddr, app.Config.SMESHING.Opts); err != nil {
			app.log.Panic("failed to start smeshing: %v", err)
		}
	} else {
		app.log.Info("smeshing not started, waiting to be triggered via smesher api")
	}

	if app.ptimesync != nil {
		app.ptimesync.Start()
	}

	if app.updater != nil {
		app.listenToUpdates(ctx)
	}
	return nil
}

func (app *App) initService(ctx context.Context, svc grpcserver.Service) (grpcserver.ServiceAPI, error) {
	switch svc {
	case grpcserver.Debug:
		return grpcserver.NewDebugService(app.db, app.conState, app.host, app.hOracle), nil
	case grpcserver.GlobalState:
		return grpcserver.NewGlobalStateService(app.mesh, app.conState), nil
	case grpcserver.Mesh:
		return grpcserver.NewMeshService(
			app.cachedDB,
			app.mesh,
			app.conState,
			app.clock,
			app.Config.LayersPerEpoch,
			app.Config.Genesis.GenesisID(),
			app.Config.LayerDuration,
			app.Config.LayerAvgSize,
			uint32(app.Config.TxsPerProposal),
		), nil
	case grpcserver.Node:
		return grpcserver.NewNodeService(app.host, app.mesh, app.clock, app.syncer, cmd.Version, cmd.Commit), nil
	case grpcserver.Admin:
		return grpcserver.NewAdminService(app.db, app.Config.DataDir(), app.host), nil
	case grpcserver.Smesher:
		return grpcserver.NewSmesherService(
			app.postSetupMgr,
			app.atxBuilder,
			app.Config.API.SmesherStreamInterval,
			app.Config.SMESHING.Opts,
		), nil
	case grpcserver.Transaction:
		return grpcserver.NewTransactionService(
			app.db,
			app.host,
			app.mesh,
			app.conState,
			app.syncer,
			app.txHandler,
		), nil
	case grpcserver.Activation:
		return grpcserver.NewActivationService(app.cachedDB, types.ATXID(app.Config.Genesis.GoldenATX())), nil
	}
	return nil, fmt.Errorf("unknown service %s", svc)
}

func unaryGrpcLogStart(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	ctxzap.Info(ctx, "started unary call")
	return handler(ctx, req)
}

func streamingGrpcLogStart(
	srv any,
	stream grpc.ServerStream,
	_ *grpc.StreamServerInfo,
	handler grpc.StreamHandler,
) error {
	ctxzap.Info(stream.Context(), "started streaming call")
	return handler(srv, stream)
}

func (app *App) newGrpc(logger log.Log, endpoint string) *grpcserver.Server {
	return grpcserver.New(
		endpoint,
		logger,
		grpc.ChainStreamInterceptor(
			grpctags.StreamServerInterceptor(),
			grpczap.StreamServerInterceptor(logger.Zap()),
			streamingGrpcLogStart,
		),
		grpc.ChainUnaryInterceptor(
			grpctags.UnaryServerInterceptor(),
			grpczap.UnaryServerInterceptor(logger.Zap()),
			unaryGrpcLogStart,
		),
		grpc.MaxSendMsgSize(app.Config.API.GrpcSendMsgSize),
		grpc.MaxRecvMsgSize(app.Config.API.GrpcRecvMsgSize),
	)
}

func (app *App) startAPIServices(ctx context.Context) error {
	logger := app.addLogger(GRPCLogger, app.log)
	grpczap.SetGrpcLoggerV2(grpclog, logger.Zap())
	var (
		unique = map[grpcserver.Service]struct{}{}
		public []grpcserver.ServiceAPI
	)
	if len(app.Config.API.PublicServices) > 0 {
		app.grpcPublicService = app.newGrpc(logger, app.Config.API.PublicListener)
	}
	if len(app.Config.API.PrivateServices) > 0 {
		app.grpcPrivateService = app.newGrpc(logger, app.Config.API.PrivateListener)
	}
	for _, svc := range app.Config.API.PublicServices {
		if _, exists := unique[svc]; exists {
			return fmt.Errorf("can't start more than one %s", svc)
		}
		gsvc, err := app.initService(ctx, svc)
		if err != nil {
			return err
		}
		logger.Info("registering public service %s", svc)
		gsvc.RegisterService(app.grpcPublicService)
		public = append(public, gsvc)
		unique[svc] = struct{}{}
	}
	for _, svc := range app.Config.API.PrivateServices {
		if _, exists := unique[svc]; exists {
			return fmt.Errorf("can't start more than one %s", svc)
		}
		gsvc, err := app.initService(ctx, svc)
		if err != nil {
			return err
		}
		logger.Info("registering private service %s", svc)
		gsvc.RegisterService(app.grpcPrivateService)
		unique[svc] = struct{}{}
	}
	if len(app.Config.API.JSONListener) > 0 {
		if len(public) == 0 {
			return fmt.Errorf("can't start json server without public services")
		}
		app.jsonAPIService = grpcserver.NewJSONHTTPServer(app.Config.API.JSONListener, logger.WithName("JSON"))
		app.jsonAPIService.StartService(ctx, public...)
	}
	if app.grpcPublicService != nil {
		if err := app.grpcPublicService.Start(); err != nil {
			return err
		}
	}
	if app.grpcPrivateService != nil {
		if err := app.grpcPrivateService.Start(); err != nil {
			return err
		}
	}
	return nil
}

func (app *App) stopServices(ctx context.Context) {
	if app.jsonAPIService != nil {
		if err := app.jsonAPIService.Shutdown(ctx); err != nil {
			app.log.With().Error("error stopping json gateway server", log.Err(err))
		}
	}

	if app.grpcPublicService != nil {
		app.log.Info("stopping public grpc service")
		// does not return any errors
		_ = app.grpcPublicService.Close()
	}
	if app.grpcPrivateService != nil {
		app.log.Info("stopping private grpc service")
		// does not return any errors
		_ = app.grpcPrivateService.Close()
	}

	if app.updater != nil {
		app.log.Info("stopping updater")
		app.updater.Close()
	}

	if app.clock != nil {
		app.clock.Close()
	}

	if app.beaconProtocol != nil {
		app.beaconProtocol.Close()
	}

	if app.atxBuilder != nil {
		_ = app.atxBuilder.StopSmeshing(false)
	}

	if app.postVerifier != nil {
		app.postVerifier.Close()
	}

	if app.hare != nil {
		app.hare.Close()
	}
	if app.hare3 != nil {
		app.hare3.Stop()
	}

	if app.blockGen != nil {
		app.blockGen.Stop()
	}

	if app.certifier != nil {
		app.certifier.Stop()
	}

	if app.fetcher != nil {
		app.fetcher.Stop()
	}

	if app.syncer != nil {
		app.syncer.Close()
	}

	if app.ptimesync != nil {
		app.ptimesync.Stop()
		app.log.Debug("peer timesync stopped")
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

	if app.pprofService != nil {
		if err := app.pprofService.Close(); err != nil {
			app.log.With().Warning("pprof service exited with error", log.Err(err))
		}
	}
	if app.profilerService != nil {
		if err := app.profilerService.Stop(); err != nil {
			app.log.With().Warning("profiler service exited with error", log.Err(err))
		}
	}

	events.CloseEventReporter()
	// SetGrpcLogger unfortunately is global
	// this ensures that a test-logger isn't used after the app shuts down
	// by e.g. a grpc connection to the node that is still open - like in TestSpacemeshApp_NodeService
	grpczap.SetGrpcLoggerV2(grpclog, log.NewNop().Zap())
}

// LoadOrCreateEdSigner either loads a previously created ed identity for the node or creates a new one if not exists.
func (app *App) LoadOrCreateEdSigner() (*signing.EdSigner, error) {
	filename := filepath.Join(app.Config.SMESHING.Opts.DataDir, edKeyFileName)
	app.log.Info("Looking for identity file at `%v`", filename)

	var data []byte
	if len(app.Config.TestConfig.SmesherKey) > 0 {
		app.log.With().Error("!!!TESTING!!! using pre-configured smesher key")
		data = []byte(app.Config.TestConfig.SmesherKey)
	} else {
		var err error
		data, err = os.ReadFile(filename)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("failed to read identity file: %w", err)
			}

			app.log.Info("Identity file not found. Creating new identity...")

			edSgn, err := signing.NewEdSigner(
				signing.WithPrefix(app.Config.Genesis.GenesisID().Bytes()),
			)
			if err != nil {
				return nil, fmt.Errorf("failed to create identity: %w", err)
			}
			if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
				return nil, fmt.Errorf("failed to create directory for identity file: %w", err)
			}

			err = os.WriteFile(filename, []byte(hex.EncodeToString(edSgn.PrivateKey())), 0o600)
			if err != nil {
				return nil, fmt.Errorf("failed to write identity file: %w", err)
			}

			app.log.With().Info("created new identity", edSgn.PublicKey())
			return edSgn, nil
		}
	}
	dst := make([]byte, signing.PrivateKeySize)
	n, err := hex.Decode(dst, data)
	if err != nil {
		return nil, fmt.Errorf("decoding private key: %w", err)
	}
	if n != signing.PrivateKeySize {
		return nil, fmt.Errorf("invalid key size %d/%d", n, signing.PrivateKeySize)
	}
	edSgn, err := signing.NewEdSigner(
		signing.WithPrivateKey(dst),
		signing.WithPrefix(app.Config.Genesis.GenesisID().Bytes()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to construct identity from data file: %w", err)
	}

	app.log.Info("Loaded existing identity; public key: %v", edSgn.PublicKey())
	return edSgn, nil
}

func (app *App) setupDBs(ctx context.Context, lg log.Log) error {
	dbPath := app.Config.DataDir()
	if err := os.MkdirAll(dbPath, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create %s: %w", dbPath, err)
	}
	sqlDB, err := sql.Open("file:"+filepath.Join(dbPath, dbFile),
		sql.WithConnections(app.Config.DatabaseConnections),
		sql.WithLatencyMetering(app.Config.DatabaseLatencyMetering),
		sql.WithV5Migration(util.ExtractActiveSet),
	)
	if err != nil {
		return fmt.Errorf("open sqlite db %w", err)
	}
	app.db = sqlDB
	if app.Config.CollectMetrics && app.Config.DatabaseSizeMeteringInterval != 0 {
		app.dbMetrics = dbmetrics.NewDBMetricsCollector(
			ctx,
			sqlDB,
			app.addLogger(StateDbLogger, lg),
			app.Config.DatabaseSizeMeteringInterval,
		)
	}
	app.cachedDB = datastore.NewCachedDB(
		sqlDB,
		app.addLogger(CachedDBLogger, lg),
		datastore.WithConfig(app.Config.Cache),
	)
	return nil
}

// Start starts the Spacemesh node and initializes all relevant services according to command line arguments provided.
func (app *App) Start(ctx context.Context) error {
	err := app.startSynchronous(ctx)
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

func (app *App) startSynchronous(ctx context.Context) (err error) {
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
		return fmt.Errorf("data-dir %s not found or could not be created: %w", app.Config.DataDir(), err)
	}

	/* Setup monitoring */
	app.errCh = make(chan error, 100)
	if app.Config.PprofHTTPServer {
		logger.Info("starting pprof server")
		app.pprofService = &http.Server{Addr: ":6060"}
		app.eg.Go(func() error {
			if err := app.pprofService.ListenAndServe(); err != nil {
				app.errCh <- fmt.Errorf("cannot start pprof http server: %w", err)
			}
			return nil
		})
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

	lg := logger.Named(app.edSgn.NodeID().ShortString()).WithFields(app.edSgn.NodeID())

	/* Initialize all protocol services */

	gTime, err := time.Parse(time.RFC3339, app.Config.Genesis.GenesisTime)
	if err != nil {
		return fmt.Errorf("cannot parse genesis time %s: %w", app.Config.Genesis.GenesisTime, err)
	}
	app.clock, err = timesync.NewClock(
		timesync.WithLayerDuration(app.Config.LayerDuration),
		timesync.WithTickInterval(1*time.Second),
		timesync.WithGenesisTime(gTime),
		timesync.WithLogger(app.addLogger(ClockLogger, lg)),
	)
	if err != nil {
		return fmt.Errorf("cannot create clock: %w", err)
	}

	lg.Info("initializing p2p services")

	cfg := app.Config.P2P
	cfg.DataDir = filepath.Join(app.Config.DataDir(), "p2p")
	p2plog := app.addLogger(P2PLogger, lg)
	// if addLogger won't add a level we will use a default 0 (info).
	cfg.LogLevel = app.getLevel(P2PLogger)
	prologue := fmt.Sprintf("%x-%v",
		app.Config.Genesis.GenesisID(),
		types.GetEffectiveGenesis(),
	)
	// Prevent testnet nodes from working on the mainnet, but
	// don't use the network cookie on mainnet as this technique
	// may be replaced later
	nc := handshake.NoNetworkCookie
	if !onMainNet(app.Config) {
		nc = handshake.NetworkCookie(prologue)
	}
	app.host, err = p2p.New(ctx, p2plog, cfg, []byte(prologue), nc,
		p2p.WithNodeReporter(events.ReportNodeStatusUpdate),
	)
	if err != nil {
		return fmt.Errorf("failed to initialize p2p host: %w", err)
	}

	if err := app.setupDBs(ctx, lg); err != nil {
		return err
	}
	if err := app.initServices(ctx); err != nil {
		return fmt.Errorf("cannot start services: %w", err)
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

	if err := app.startServices(ctx); err != nil {
		return err
	}

	// need post verifying service to start first
	app.preserveAfterRecovery(ctx)

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

func (app *App) preserveAfterRecovery(ctx context.Context) {
	if app.preserve == nil {
		return
	}
	for i, poetProof := range app.preserve.Proofs {
		encoded, err := codec.Encode(poetProof)
		if err != nil {
			app.log.With().Error("failed to encode poet proof after checkpoint",
				log.Stringer("atx id", app.preserve.Deps[i].ID()),
				log.Object("poet proof", poetProof),
				log.Err(err),
			)
			continue
		}
		hash := app.preserve.Deps[i].GetPoetProofRef()
		if err := app.poetDb.ValidateAndStoreMsg(ctx, hash, p2p.NoPeer, encoded); err != nil {
			app.log.With().Error("failed to preserve poet proof after checkpoint",
				log.Stringer("atx id", app.preserve.Deps[i].ID()),
				log.String("poet proof ref", hash.ShortString()),
				log.Err(err),
			)
			continue
		}
		app.log.With().Info("preserved poet proof after checkpoint",
			log.Stringer("atx id", app.preserve.Deps[i].ID()),
			log.String("poet proof ref", hash.ShortString()),
		)
	}
	for _, vatx := range app.preserve.Deps {
		encoded, err := codec.Encode(vatx)
		if err != nil {
			app.log.With().Error("failed to encode atx after checkpoint",
				log.Inline(vatx),
				log.Err(err),
			)
			continue
		}
		if err := app.atxHandler.HandleSyncedAtx(ctx, vatx.ID().Hash32(), p2p.NoPeer, encoded); err != nil {
			app.log.With().Error("failed to preserve atx after checkpoint",
				log.Inline(vatx),
				log.Err(err),
			)
			continue
		}
		app.log.With().Info("preserved atx after checkpoint",
			log.Inline(vatx),
		)
	}
}

func (app *App) Host() *p2p.Host {
	return app.host
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

type tortoiseWeakCoin struct {
	db       sql.Executor
	tortoise system.Tortoise
}

func (w tortoiseWeakCoin) Set(lid types.LayerID, value bool) error {
	if err := layers.SetWeakCoin(w.db, lid, value); err != nil {
		return err
	}
	w.tortoise.OnWeakCoin(lid, value)
	return nil
}

func onMainNet(conf *config.Config) bool {
	return conf.Genesis.GenesisTime == config.MainnetConfig().Genesis.GenesisTime
}
