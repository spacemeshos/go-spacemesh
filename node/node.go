// Package node contains the main executable for go-spacemesh node
package node

import (
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
	"syscall"
	"time"

	"github.com/gofrs/flock"
	grpc_logsettable "github.com/grpc-ecosystem/go-grpc-middleware/logging/settable"
	grpczap "github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	grpctags "github.com/grpc-ecosystem/go-grpc-middleware/tags"
	"github.com/mitchellh/mapstructure"
	"github.com/pyroscope-io/pyroscope/pkg/agent/profiler"
	poetconfig "github.com/spacemeshos/poet/config"
	"github.com/spacemeshos/poet/server"
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
	"github.com/spacemeshos/go-spacemesh/malfeasance"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/metrics"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/node/mapstructureutil"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/proposals"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
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
			app := New(
				WithConfig(conf),
				// NOTE(dshulyak) this needs to be max level so that child logger can can be current level or below.
				// otherwise it will fail later when child logger will try to increase level.
				WithLog(log.RegisterHooks(
					log.NewWithLevel("", zap.NewAtomicLevelAt(zapcore.DebugLevel)),
					events.EventHook())),
			)

			run := func(ctx context.Context) error {
				types.SetLayersPerEpoch(app.Config.LayersPerEpoch)

				// ensure all data folders exist
				if err := os.MkdirAll(app.Config.DataDir(), 0o700); err != nil {
					return fmt.Errorf("ensure folders exist: %w", err)
				}

				/* Create or load miner identity */
				if app.edSgn, err = app.LoadOrCreateEdSigner(); err != nil {
					return fmt.Errorf("could not retrieve identity: %w", err)
				}

				if err = app.LoadCheckpoint(ctx); err != nil {
					return err
				}

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
					_ = app.eg.Wait()
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
	lvl := zap.NewAtomicLevelAt(zap.InfoLevel)
	log.SetupGlobal(app.log.SetLevel(&lvl))
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
	syncer             *syncer.Syncer
	proposalListener   *proposals.Handler
	proposalBuilder    *miner.ProposalBuilder
	mesh               *mesh.Mesh
	cachedDB           *datastore.CachedDB
	clock              *timesync.NodeClock
	hare               *hare.Hare
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
	postVerifier       *activation.OffloadingPostVerifier

	host *p2p.Host

	loggers map[string]*zap.AtomicLevel
	started chan struct{} // this channel is closed once the app has finished starting
	eg      *errgroup.Group
}

// this code path is only used during systest after admin::Recover() RPC causing
// the node to log.fatal and restart without reading any new config.
// the expected action for node operator is to supply new config with
// --checkpoint-file and --restore-layer via config file or cmdline options.
func defaultRecoveryFile(dataDir string) (string, types.LayerID, error) {
	recoverDir := checkpoint.RecoveryDir(dataDir)
	files, err := filepath.Glob(fmt.Sprintf("%s%s*", recoverDir, string([]rune{filepath.Separator})))
	if err != nil {
		return "", 0, nil
	}
	if len(files) == 0 {
		// remove the directory regardless
		_ = os.Remove(recoverDir)
		return "", 0, nil
	}
	if len(files) > 1 {
		return "", 0, fmt.Errorf("multiple checkpoint files found [%v]. delete all and re-download", files)
	}
	restore, err := checkpoint.ParseRestoreLayer(filepath.Base(files[0]))
	if err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("file://%s", files[0]), restore, nil
}

func (app *App) LoadCheckpoint(ctx context.Context) error {
	var (
		checkpointFile = app.Config.Recovery.Uri
		restore        = types.LayerID(app.Config.Recovery.Restore)
		err            error
	)
	if len(checkpointFile) == 0 {
		if !app.Config.Recovery.RecoverFromDefaultDir {
			return nil
		}
		checkpointFile, restore, err = defaultRecoveryFile(app.Config.DataDir())
		if err != nil {
			return err
		}
	}
	if len(checkpointFile) == 0 {
		return nil
	}
	if restore == 0 {
		return fmt.Errorf("restore layer not set")
	}
	cfg := &checkpoint.RecoverConfig{
		GoldenAtx:      types.ATXID(app.Config.Genesis.GoldenATX()),
		DataDir:        app.Config.DataDir(),
		DbFile:         dbFile,
		PreserveOwnAtx: app.Config.Recovery.PreserveOwnAtx,
	}
	app.log.WithContext(ctx).With().Info("recover from checkpoint",
		log.String("url", checkpointFile),
		log.Stringer("restore", restore),
	)
	return checkpoint.Recover(ctx, app.log, afero.NewOsFs(), cfg, app.edSgn.NodeID(), checkpointFile, restore)
}

func (app *App) Started() chan struct{} {
	return app.started
}

func (app *App) introduction() {
	log.Info("Welcome to Spacemesh. Spacemesh full node is starting...")
}

// Initialize sets up an exit signal, logging and checks the clock, returns error if clock is not in sync.
func (app *App) Initialize() (err error) {
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
	maxHareLayerDuration := app.Config.HARE.WakeupDelta + time.Duration(maxHareRoundsPerLayer)*app.Config.HARE.RoundDuration
	if app.Config.LayerDuration*time.Duration(app.Config.Tortoise.Zdist) <= maxHareLayerDuration {
		log.With().Error("incompatible params",
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

func (app *App) initServices(ctx context.Context, poetClients []activation.PoetProvingServiceClient) error {
	vrfSigner, err := app.edSgn.VRFSigner()
	if err != nil {
		return fmt.Errorf("could not create vrf signer: %w", err)
	}
	layerSize := app.Config.LayerAvgSize
	layersPerEpoch := types.GetLayersPerEpoch()
	lg := app.log.Named(app.edSgn.NodeID().ShortString()).WithFields(app.edSgn.NodeID())

	poetDb := activation.NewPoetDb(app.db, app.addLogger(PoetDbLogger, lg))

	nipostValidatorLogger := app.addLogger(NipostValidatorLogger, lg)
	postVerifiers := make([]activation.PostVerifier, 0, app.Config.SMESHING.VerifyingOpts.Workers)
	for i := 0; i < app.Config.SMESHING.VerifyingOpts.Workers; i++ {
		logger := nipostValidatorLogger.Named(fmt.Sprintf("worker-%d", i))
		postVerifiers = append(postVerifiers, activation.NewPostVerifier(app.Config.POST, logger))
	}
	app.postVerifier = activation.NewOffloadingPostVerifier(postVerifiers, nipostValidatorLogger)

	validator := activation.NewValidator(poetDb, app.Config.POST, nipostValidatorLogger, app.postVerifier)
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
	beaconProtocol := beacon.New(app.edSgn.NodeID(), app.host, app.edSgn, app.edVerifier, vrfSigner, vrfVerifier, app.cachedDB, app.clock,
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
	trtl, err := tortoise.Recover(
		app.cachedDB, beaconProtocol, trtlopts...,
	)
	if err != nil {
		return fmt.Errorf("can't recover tortoise state: %w", err)
	}
	app.eg.Go(func() error {
		for rst := range beaconProtocol.Results() {
			trtl.OnBeacon(rst.Epoch, rst.Beacon)
		}
		app.log.Debug("beacon results watcher exited")
		return nil
	})

	executor := mesh.NewExecutor(app.cachedDB, state, app.conState, app.addLogger(ExecutorLogger, lg))
	msh, err := mesh.NewMesh(app.cachedDB, app.clock, trtl, executor, app.conState, app.addLogger(MeshLogger, lg))
	if err != nil {
		return fmt.Errorf("failed to create mesh: %w", err)
	}

	poetCfg := activation.PoetConfig{
		PhaseShift:  app.Config.POET.PhaseShift,
		CycleGap:    app.Config.POET.CycleGap,
		GracePeriod: app.Config.POET.GracePeriod,
	}
	fetcherWrapped := &layerFetcher{}
	atxHandler := activation.NewHandler(
		app.cachedDB,
		app.edVerifier,
		app.clock,
		app.host,
		fetcherWrapped,
		layersPerEpoch,
		app.Config.TickSize,
		goldenATXID,
		validator,
		[]activation.AtxReceiver{&atxReceiver{trtl}, beaconProtocol},
		app.addLogger(ATXHandlerLogger, lg),
		poetCfg,
	)

	// we can't have an epoch offset which is greater/equal than the number of layers in an epoch

	if app.Config.HareEligibility.ConfidenceParam >= app.Config.BaseConfig.LayersPerEpoch {
		return fmt.Errorf("confidence param should be smaller than layers per epoch. eligibility-confidence-param: %d. layers-per-epoch: %d",
			app.Config.HareEligibility.ConfidenceParam, app.Config.BaseConfig.LayersPerEpoch)
	}

	proposalListener := proposals.NewHandler(app.cachedDB, app.edVerifier, app.host, fetcherWrapped, beaconProtocol, msh, trtl, vrfVerifier, app.clock,
		proposals.WithLogger(app.addLogger(ProposalListenerLogger, lg)),
		proposals.WithConfig(proposals.Config{
			LayerSize:      layerSize,
			LayersPerEpoch: layersPerEpoch,
			GoldenATXID:    goldenATXID,
			MaxExceptions:  trtlCfg.MaxExceptions,
			Hdist:          trtlCfg.Hdist,
		}),
	)

	blockHandler := blocks.NewHandler(fetcherWrapped, app.db, msh,
		blocks.WithLogger(app.addLogger(BlockHandlerLogger, lg)))

	app.txHandler = txs.NewTxHandler(
		app.conState,
		app.host.ID(),
		app.addLogger(TxHandlerLogger, lg),
	)

	app.hOracle = eligibility.New(beaconProtocol, app.cachedDB, vrfVerifier, vrfSigner, app.Config.LayersPerEpoch, app.Config.HareEligibility, app.addLogger(HareOracleLogger, lg))
	// TODO: genesisMinerWeight is set to app.Config.SpaceToCommit, because PoET ticks are currently hardcoded to 1

	bscfg := app.Config.Bootstrap
	bscfg.DataDir = app.Config.DataDir()
	bscfg.Interval = app.Config.LayerDuration / 5
	app.updater = bootstrap.New(
		app.clock,
		bootstrap.WithConfig(bscfg),
		bootstrap.WithLogger(app.addLogger(BootstrapLogger, lg)),
	)

	app.certifier = blocks.NewCertifier(app.cachedDB, app.hOracle, app.edSgn.NodeID(), app.edSgn, app.edVerifier, app.host, app.clock, beaconProtocol, trtl,
		blocks.WithCertContext(ctx),
		blocks.WithCertConfig(blocks.CertConfig{
			CommitteeSize:    app.Config.HARE.N,
			CertifyThreshold: app.Config.HARE.N/2 + 1,
			LayerBuffer:      app.Config.Tortoise.Zdist,
			NumLayersToKeep:  app.Config.Tortoise.Zdist * 2,
		}),
		blocks.WithCertifierLogger(app.addLogger(BlockCertLogger, lg)),
	)

	fetcher := fetch.NewFetch(app.cachedDB, msh, beaconProtocol, app.host,
		fetch.WithContext(ctx),
		fetch.WithConfig(app.Config.FETCH),
		fetch.WithLogger(app.addLogger(Fetcher, lg)),
	)
	fetcherWrapped.Fetcher = fetcher

	patrol := layerpatrol.New()
	syncerConf := syncer.Config{
		Interval:         app.Config.Sync.Interval,
		EpochEndFraction: 0.8,
		HareDelayLayers:  app.Config.Tortoise.Zdist,
		SyncCertDistance: app.Config.Tortoise.Hdist,
		MaxHashesInReq:   100,
		MaxStaleDuration: time.Hour,
		Standalone:       app.Config.Standalone,
	}
	newSyncer := syncer.NewSyncer(app.cachedDB, app.clock, beaconProtocol, msh, trtl, fetcher, patrol, app.certifier,
		syncer.WithConfig(syncerConf),
		syncer.WithLogger(app.addLogger(SyncLogger, lg)))
	// TODO(dshulyak) this needs to be improved, but dependency graph is a bit complicated
	beaconProtocol.SetSyncState(newSyncer)

	hareOutputCh := make(chan hare.LayerOutput, app.Config.HARE.LimitConcurrent)
	app.blockGen = blocks.NewGenerator(app.cachedDB, executor, msh, fetcherWrapped, app.certifier, patrol,
		blocks.WithContext(ctx),
		blocks.WithConfig(blocks.Config{
			LayerSize:          layerSize,
			LayersPerEpoch:     layersPerEpoch,
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

	proposalBuilder := miner.NewProposalBuilder(
		ctx,
		app.clock,
		app.edSgn,
		vrfSigner,
		app.cachedDB,
		app.host,
		trtl,
		beaconProtocol,
		newSyncer,
		app.conState,
		miner.WithNodeID(app.edSgn.NodeID()),
		miner.WithLayerSize(layerSize),
		miner.WithLayerPerEpoch(layersPerEpoch),
		miner.WithHdist(app.Config.Tortoise.Hdist),
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

	nipostBuilder := activation.NewNIPostBuilder(
		app.edSgn.NodeID(),
		postSetupMgr,
		poetClients,
		poetDb,
		app.Config.SMESHING.Opts.DataDir,
		app.addLogger(NipostBuilderLogger, lg),
		app.edSgn,
		poetCfg,
		app.clock,
	)

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
	atxBuilder := activation.NewBuilder(
		builderConfig,
		app.edSgn.NodeID(),
		app.edSgn,
		app.cachedDB,
		atxHandler,
		app.host,
		nipostBuilder,
		postSetupMgr,
		app.clock,
		newSyncer,
		app.addLogger("atxBuilder", lg),
		activation.WithContext(ctx),
		activation.WithPoetConfig(poetCfg),
		activation.WithPoetRetryInterval(app.Config.HARE.WakeupDelta),
	)

	malfeasanceHandler := malfeasance.NewHandler(
		app.cachedDB,
		app.addLogger(MalfeasanceLogger, lg),
		app.host.ID(),
		app.hare,
		app.edVerifier,
	)
	fetcher.SetValidators(
		fetch.ValidatorFunc(pubsub.DropPeerOnValidationReject(atxHandler.HandleAtxData, app.host, lg)),
		fetch.ValidatorFunc(pubsub.DropPeerOnValidationReject(poetDb.ValidateAndStoreMsg, app.host, lg)),
		fetch.ValidatorFunc(pubsub.DropPeerOnValidationReject(proposalListener.HandleSyncedBallot, app.host, lg)),
		fetch.ValidatorFunc(pubsub.DropPeerOnValidationReject(blockHandler.HandleSyncedBlock, app.host, lg)),
		fetch.ValidatorFunc(pubsub.DropPeerOnValidationReject(proposalListener.HandleSyncedProposal, app.host, lg)),
		fetch.ValidatorFunc(pubsub.DropPeerOnValidationReject(app.txHandler.HandleBlockTransaction, app.host, lg)),
		fetch.ValidatorFunc(pubsub.DropPeerOnValidationReject(app.txHandler.HandleProposalTransaction, app.host, lg)),
		fetch.ValidatorFunc(pubsub.DropPeerOnValidationReject(malfeasanceHandler.HandleMalfeasanceProof, app.host, lg)),
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

	app.host.Register(pubsub.BeaconWeakCoinProtocol, pubsub.ChainGossipHandler(syncHandler, beaconProtocol.HandleWeakCoinProposal))
	app.host.Register(pubsub.BeaconProposalProtocol, pubsub.ChainGossipHandler(syncHandler, beaconProtocol.HandleProposal))
	app.host.Register(pubsub.BeaconFirstVotesProtocol, pubsub.ChainGossipHandler(syncHandler, beaconProtocol.HandleFirstVotes))
	app.host.Register(pubsub.BeaconFollowingVotesProtocol, pubsub.ChainGossipHandler(syncHandler, beaconProtocol.HandleFollowingVotes))
	app.host.Register(pubsub.ProposalProtocol, pubsub.ChainGossipHandler(syncHandler, proposalListener.HandleProposal))
	app.host.Register(pubsub.AtxProtocol, pubsub.ChainGossipHandler(atxSyncHandler, atxHandler.HandleGossipAtx))
	app.host.Register(pubsub.TxProtocol, pubsub.ChainGossipHandler(syncHandler, app.txHandler.HandleGossipTransaction))
	app.host.Register(pubsub.HareProtocol, pubsub.ChainGossipHandler(syncHandler, app.hare.GetHareMsgHandler()))
	app.host.Register(pubsub.BlockCertify, pubsub.ChainGossipHandler(syncHandler, app.certifier.HandleCertifyMessage))
	app.host.Register(pubsub.MalfeasanceProof, pubsub.ChainGossipHandler(atxSyncHandler, malfeasanceHandler.HandleMalfeasanceProof))

	app.proposalBuilder = proposalBuilder
	app.proposalListener = proposalListener
	app.mesh = msh
	app.syncer = newSyncer
	app.svm = state
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

func (app *App) launchStandalone(ctx context.Context) error {
	if !app.Config.Standalone {
		return nil
	}
	if len(app.Config.PoETServers) != 1 {
		return fmt.Errorf("to launch in a standalone mode provide single local address for poet: %v", app.Config.PoETServers)
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
	cfg := poetconfig.DefaultConfig()
	cfg.PoetDir = filepath.Join(app.Config.DataDir(), "poet")
	cfg.DataDir = cfg.PoetDir
	cfg.LogDir = cfg.PoetDir
	parsed, err := url.Parse(app.Config.PoETServers[0])
	if err != nil {
		return err
	}
	cfg.RawRESTListener = parsed.Host
	cfg.Service.Genesis = app.Config.Genesis.GenesisTime
	cfg.Service.EpochDuration = app.Config.LayerDuration * time.Duration(app.Config.LayersPerEpoch)
	cfg.Service.CycleGap = app.Config.POET.CycleGap
	cfg.Service.PhaseShift = app.Config.POET.PhaseShift
	srv, err := server.New(ctx, *cfg)
	if err != nil {
		return fmt.Errorf("init poet server: %w", err)
	}
	app.log.With().Warning("lauching poet in standalone mode", log.Any("config", cfg))
	app.eg.Go(func() error {
		if err := srv.Start(ctx); err != nil {
			app.log.With().Error("poet server failed", log.Err(err))
			return err
		}
		return nil
	})
	return nil
}

func (app *App) listenToUpdates(ctx context.Context, appErr chan error) {
	app.eg.Go(func() error {
		ch := app.updater.Subscribe()
		if err := app.updater.Start(ctx); err != nil {
			appErr <- err
			return nil
		}
		for update := range ch {
			select {
			case <-ctx.Done():
				return nil
			default:
				if update.Data.Beacon != types.EmptyBeacon {
					if err := app.beaconProtocol.UpdateBeacon(update.Data.Epoch, update.Data.Beacon); err != nil {
						appErr <- err
						return nil
					}
				}
				if len(update.Data.ActiveSet) > 0 {
					app.hOracle.UpdateActiveSet(update.Data.Epoch, update.Data.ActiveSet)
				}
			}
		}
		return nil
	})
}

func (app *App) startServices(ctx context.Context, appErr chan error) error {
	if err := app.fetcher.Start(); err != nil {
		return fmt.Errorf("failed to start fetcher: %w", err)
	}
	app.eg.Go(func() error {
		app.postVerifier.Start(ctx)
		return nil
	})
	app.syncer.Start(ctx)
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
		if err := app.atxBuilder.StartSmeshing(coinbaseAddr, app.Config.SMESHING.Opts); err != nil {
			log.Panic("failed to start smeshing: %v", err)
		}
	} else {
		log.Info("smeshing not started, waiting to be triggered via smesher api")
	}

	if app.ptimesync != nil {
		app.ptimesync.Start()
	}

	if app.updater != nil {
		app.listenToUpdates(ctx, appErr)
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
		return grpcserver.NewMeshService(app.mesh, app.conState, app.clock, app.Config.LayersPerEpoch, app.Config.Genesis.GenesisID(), app.Config.LayerDuration, app.Config.LayerAvgSize, uint32(app.Config.TxsPerProposal)), nil
	case grpcserver.Node:
		return grpcserver.NewNodeService(ctx, app.host, app.mesh, app.clock, app.syncer, cmd.Version, cmd.Commit), nil
	case grpcserver.Admin:
		return grpcserver.NewAdminService(app.db, app.Config.DataDir(), app.log.WithName("admin")), nil
	case grpcserver.Smesher:
		return grpcserver.NewSmesherService(app.postSetupMgr, app.atxBuilder, app.Config.API.SmesherStreamInterval, app.Config.SMESHING.Opts), nil
	case grpcserver.Transaction:
		return grpcserver.NewTransactionService(app.db, app.host, app.mesh, app.conState, app.syncer, app.txHandler), nil
	case grpcserver.Activation:
		return grpcserver.NewActivationService(app.cachedDB), nil
	}
	return nil, fmt.Errorf("unknown service %s", svc)
}

func (app *App) newGrpc(logger *zap.Logger, endpoint string) *grpcserver.Server {
	return grpcserver.New(endpoint,
		grpc.ChainStreamInterceptor(grpctags.StreamServerInterceptor(), grpczap.StreamServerInterceptor(logger)),
		grpc.ChainUnaryInterceptor(grpctags.UnaryServerInterceptor(), grpczap.UnaryServerInterceptor(logger)),
		grpc.MaxSendMsgSize(app.Config.API.GrpcSendMsgSize),
		grpc.MaxRecvMsgSize(app.Config.API.GrpcRecvMsgSize),
	)
}

func (app *App) startAPIServices(ctx context.Context) error {
	logger := app.addLogger(GRPCLogger, app.log).Zap()
	grpczap.SetGrpcLoggerV2(grpclog, logger)
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
		gsvc.RegisterService(app.grpcPrivateService)
		unique[svc] = struct{}{}
	}
	if len(app.Config.API.JSONListener) > 0 {
		if len(public) == 0 {
			return fmt.Errorf("can't start json server without public services")
		}
		app.jsonAPIService = grpcserver.NewJSONHTTPServer(app.Config.API.JSONListener)
		app.jsonAPIService.StartService(ctx, public...)
	}
	if app.grpcPublicService != nil {
		app.grpcPublicService.Start()
	}
	if app.grpcPrivateService != nil {
		app.grpcPrivateService.Start()
	}
	return nil
}

func (app *App) stopServices(ctx context.Context) {
	if app.jsonAPIService != nil {
		if err := app.jsonAPIService.Shutdown(ctx); err != nil {
			log.With().Error("error stopping json gateway server", log.Err(err))
		}
	}

	if app.grpcPublicService != nil {
		log.Info("stopping public grpc service")
		// does not return any errors
		_ = app.grpcPublicService.Close()
	}
	if app.grpcPrivateService != nil {
		log.Info("stopping private grpc service")
		// does not return any errors
		_ = app.grpcPrivateService.Close()
	}

	if app.updater != nil {
		app.updater.Close()
	}

	if app.proposalBuilder != nil {
		app.proposalBuilder.Close()
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

	if app.hare != nil {
		app.hare.Close()
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

		log.With().Info("created new identity", edSgn.PublicKey())
		return edSgn, nil
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

	log.Info("Loaded existing identity; public key: %v", edSgn.PublicKey())
	return edSgn, nil
}

func (app *App) setupDBs(ctx context.Context, lg log.Log, dbPath string) error {
	if err := os.MkdirAll(dbPath, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create %s: %w", dbPath, err)
	}
	sqlDB, err := sql.Open("file:"+filepath.Join(dbPath, dbFile),
		sql.WithConnections(app.Config.DatabaseConnections),
		sql.WithLatencyMetering(app.Config.DatabaseLatencyMetering),
	)
	if err != nil {
		return fmt.Errorf("open sqlite db %w", err)
	}
	app.db = sqlDB
	if app.Config.CollectMetrics {
		app.dbMetrics = dbmetrics.NewDBMetricsCollector(ctx, sqlDB, app.addLogger(StateDbLogger, lg), 5*time.Minute)
	}
	app.cachedDB = datastore.NewCachedDB(sqlDB, app.addLogger(CachedDBLogger, lg))
	return nil
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
		log.String("hostname", hostname),
	)

	if err := os.MkdirAll(app.Config.DataDir(), 0o700); err != nil {
		return fmt.Errorf("data-dir %s not found or could not be created: %w", app.Config.DataDir(), err)
	}

	/* Setup monitoring */
	appErr := make(chan error, 100)
	if app.Config.PprofHTTPServer {
		logger.Info("starting pprof server")
		srv := &http.Server{Addr: ":6060"}
		defer srv.Shutdown(ctx)
		app.eg.Go(func() error {
			if err := srv.ListenAndServe(); err != nil {
				appErr <- fmt.Errorf("cannot start pprof http server: %w", err)
			}
			return nil
		})
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

	lg := logger.Named(app.edSgn.NodeID().ShortString()).WithFields(app.edSgn.NodeID())

	poetClients := make([]activation.PoetProvingServiceClient, 0, len(app.Config.PoETServers))
	for _, address := range app.Config.PoETServers {
		client, err := activation.NewHTTPPoetClient(address, app.Config.POET)
		if err != nil {
			return fmt.Errorf("cannot create poet client: %w", err)
		}
		poetClients = append(poetClients, client)
	}

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
	app.host, err = p2p.New(ctx, p2plog, cfg, []byte(prologue),
		p2p.WithNodeReporter(events.ReportNodeStatusUpdate),
	)
	if err != nil {
		return fmt.Errorf("failed to initialize p2p host: %w", err)
	}

	if err = app.setupDBs(ctx, lg, app.Config.DataDir()); err != nil {
		return err
	}
	if err = app.initServices(ctx, poetClients); err != nil {
		return fmt.Errorf("cannot start services: %w", err)
	}

	if app.Config.CollectMetrics {
		metrics.StartMetricsServer(app.Config.MetricsPort)
	}

	if app.Config.MetricsPush != "" {
		metrics.StartPushingMetrics(app.Config.MetricsPush,
			app.Config.MetricsPushUser, app.Config.MetricsPushPass, app.Config.MetricsPushPeriod,
			app.host.ID().String(), app.Config.Genesis.GenesisID().ShortString())
	}

	if err := app.startServices(ctx, appErr); err != nil {
		return err
	}

	if err := app.startAPIServices(ctx); err != nil {
		return err
	}

	if err := app.launchStandalone(ctx); err != nil {
		return err
	}

	events.SubscribeToLayers(app.clock)
	logger.Info("app started")

	// notify anyone who might be listening that the app has finished starting.
	// this can be used by, e.g., app tests.
	close(app.started)

	defer events.ReportError(events.NodeError{
		Msg:   "node is shutting down",
		Level: zapcore.InfoLevel,
	})
	// TODO: pass app.eg to components and wait for them collectively
	if app.ptimesync != nil {
		app.eg.Go(func() error {
			appErr <- app.ptimesync.Wait()
			return nil
		})
	}
	// app blocks until it receives a signal to exit
	// this signal may come from the node or from sig-abort (ctrl-c)
	select {
	case <-ctx.Done():
		return nil
	case err = <-appErr:
		return err
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

type atxReceiver struct {
	tortoise *tortoise.Tortoise
}

func (a *atxReceiver) OnAtx(header *types.ActivationTxHeader) {
	a.tortoise.OnAtx(header.ToData())
}
