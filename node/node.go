// Package node contains the main executable for go-spacemesh node
package node

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"syscall"
	"time"

	"github.com/gofrs/flock"
	pyroscope "github.com/grafana/pyroscope-go"
	grpc_logsettable "github.com/grpc-ecosystem/go-grpc-middleware/logging/settable"
	grpczap "github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	"github.com/mitchellh/mapstructure"
	"github.com/spacemeshos/poet/server"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/exp/maps"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver/v2alpha1"
	"github.com/spacemeshos/go-spacemesh/atxsdata"
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
	"github.com/spacemeshos/go-spacemesh/hare3"
	"github.com/spacemeshos/go-spacemesh/hare3/compat"
	"github.com/spacemeshos/go-spacemesh/hare3/eligibility"
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
	"github.com/spacemeshos/go-spacemesh/proposals/store"
	"github.com/spacemeshos/go-spacemesh/prune"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/activesets"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	dbmetrics "github.com/spacemeshos/go-spacemesh/sql/metrics"
	sqlmigrations "github.com/spacemeshos/go-spacemesh/sql/migrations"
	"github.com/spacemeshos/go-spacemesh/syncer"
	"github.com/spacemeshos/go-spacemesh/syncer/atxsync"
	"github.com/spacemeshos/go-spacemesh/syncer/blockssync"
	"github.com/spacemeshos/go-spacemesh/syncer/malsync"
	"github.com/spacemeshos/go-spacemesh/system"
	"github.com/spacemeshos/go-spacemesh/timesync"
	timeCfg "github.com/spacemeshos/go-spacemesh/timesync/config"
	"github.com/spacemeshos/go-spacemesh/timesync/peersync"
	"github.com/spacemeshos/go-spacemesh/tortoise"
	"github.com/spacemeshos/go-spacemesh/txs"
)

const (
	genesisFileName = "genesis.json"
	dbFile          = "state.sql"

	oldLocalDbFile = "node_state.sql"
	localDbFile    = "local.sql"
)

// Logger names.
const (
	ClockLogger            = "clock"
	P2PLogger              = "p2p"
	PostLogger             = "post"
	PostServiceLogger      = "postService"
	PostInfoServiceLogger  = "postInfoService"
	StateDbLogger          = "stateDb"
	BeaconLogger           = "beacon"
	CachedDBLogger         = "cachedDB"
	PoetDbLogger           = "poetDb"
	TrtlLogger             = "trtl"
	ATXHandlerLogger       = "atxHandler"
	ATXBuilderLogger       = "atxBuilder"
	MeshLogger             = "mesh"
	SyncLogger             = "sync"
	HareOracleLogger       = "hareOracle"
	HareLogger             = "hare"
	BlockCertLogger        = "blockCert"
	BlockGenLogger         = "blockGenerator"
	BlockHandlerLogger     = "blockHandler"
	TxHandlerLogger        = "txHandler"
	ProposalStoreLogger    = "proposalStore"
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
	conf := config.MainnetConfig()
	var configPath *string
	c := &cobra.Command{
		Use:   "node",
		Short: "start node",
		RunE: func(c *cobra.Command, args []string) error {
			if err := configure(c, *configPath, &conf); err != nil {
				return err
			}

			app := New(
				WithConfig(&conf),
				// NOTE(dshulyak) this needs to be max level so that child logger can can be current level or below.
				// otherwise it will fail later when child logger will try to increase level.
				WithLog(log.NewWithLevel("node", zap.NewAtomicLevelAt(zap.DebugLevel), events.EventHook())),
			)

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

			// Migrate legacy identity to new location
			if err := app.MigrateExistingIdentity(); err != nil {
				return fmt.Errorf("migrating existing identity: %w", err)
			}

			var err error
			if app.signers, err = app.TestIdentity(); err != nil {
				return fmt.Errorf("testing identity: %w", err)
			}

			if app.signers == nil {
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
			}

			// Don't print usage on error from this point forward
			c.SilenceUsage = true

			app.preserve, err = app.LoadCheckpoint(ctx)
			if err != nil {
				return fmt.Errorf("loading checkpoint: %w", err)
			}

			// This blocks until the context is finished or until an error is produced
			err = app.Start(ctx)
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

func configure(c *cobra.Command, configPath string, conf *config.Config) error {
	preset := conf.Preset // might be set via CLI flag
	if err := loadConfig(conf, preset, configPath); err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	// apply CLI args to config
	if err := c.ParseFlags(os.Args[1:]); err != nil {
		return fmt.Errorf("parsing flags: %w", err)
	}

	if conf.LOGGING.Encoder == config.JSONLogEncoder {
		log.JSONLog(true)
	}

	if cmd.NoMainNet && onMainNet(conf) && !conf.NoMainOverride {
		return errors.New("this is a testnet-only build not intended for mainnet")
	}

	return nil
}

var (
	appLog  log.Log
	grpclog grpc_logsettable.SettableLoggerV2
)

func init() {
	appLog = log.NewNop()
	grpclog = grpc_logsettable.ReplaceGrpcLoggerV2()
}

// loadConfig loads config and preset (if provided) into the provided config.
// It first loads the preset and then overrides it with values from the config file.
func loadConfig(cfg *config.Config, preset, path string) error {
	v := viper.New()
	// read in config from file
	if err := config.LoadConfig(path, v); err != nil {
		return err
	}

	// override default config with preset if provided
	if len(preset) == 0 && v.IsSet("preset") {
		preset = v.GetString("preset")
	}
	if len(preset) > 0 {
		p, err := presets.Get(preset)
		if err != nil {
			return err
		}
		*cfg = p
	}

	// Unmarshall config file into config struct
	hook := mapstructure.ComposeDecodeHookFunc(
		mapstructure.StringToTimeDurationHookFunc(),
		mapstructure.StringToSliceHookFunc(","),
		mapstructureutil.AddressListDecodeFunc(),
		mapstructureutil.BigRatDecodeFunc(),
		mapstructureutil.PostProviderIDDecodeFunc(),
		mapstructureutil.DeprecatedHook(),
		mapstructure.TextUnmarshallerHookFunc(),
	)

	opts := []viper.DecoderConfigOption{
		viper.DecodeHook(hook),
		WithZeroFields(),
		WithIgnoreUntagged(),
		WithErrorUnused(),
	}

	// load config if it was loaded to the viper
	if err := v.Unmarshal(cfg, opts...); err != nil {
		return fmt.Errorf("unmarshal config: %w", err)
	}
	return nil
}

func WithZeroFields() viper.DecoderConfigOption {
	return func(cfg *mapstructure.DecoderConfig) {
		cfg.ZeroFields = true
	}
}

func WithIgnoreUntagged() viper.DecoderConfigOption {
	return func(cfg *mapstructure.DecoderConfig) {
		cfg.IgnoreUntaggedFields = true
	}
}

func WithErrorUnused() viper.DecoderConfigOption {
	return func(cfg *mapstructure.DecoderConfig) {
		cfg.ErrorUnused = true
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
		Config:       &defaultConfig,
		log:          appLog,
		loggers:      make(map[string]*zap.AtomicLevel),
		grpcServices: make(map[grpcserver.Service]grpcserver.ServiceAPI),
		started:      make(chan struct{}),
		eg:           &errgroup.Group{},
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
	fileLock          *flock.Flock
	signers           []*signing.EdSigner
	Config            *config.Config
	db                *sql.Database
	cachedDB          *datastore.CachedDB
	dbMetrics         *dbmetrics.DBMetricsCollector
	localDB           *localsql.Database
	grpcPublicServer  *grpcserver.Server
	grpcPrivateServer *grpcserver.Server
	grpcPostServer    *grpcserver.Server
	grpcTLSServer     *grpcserver.Server
	jsonAPIServer     *grpcserver.JSONHTTPServer
	grpcServices      map[grpcserver.Service]grpcserver.ServiceAPI
	pprofService      *http.Server
	profilerService   *pyroscope.Profiler
	syncer            *syncer.Syncer
	proposalListener  *proposals.Handler
	proposalBuilder   *miner.ProposalBuilder
	mesh              *mesh.Mesh
	atxsdata          *atxsdata.Data
	clock             *timesync.NodeClock
	hare3             *hare3.Hare
	hOracle           *eligibility.Oracle
	blockGen          *blocks.Generator
	certifier         *blocks.Certifier
	atxBuilder        *activation.Builder
	nipostBuilder     *activation.NIPostBuilder
	atxHandler        *activation.Handler
	txHandler         *txs.TxHandler
	validator         *activation.Validator
	edVerifier        *signing.EdVerifier
	beaconProtocol    *beacon.ProtocolDriver
	log               log.Log
	syncLogger        log.Log
	svm               *vm.VM
	conState          *txs.ConservativeState
	fetcher           *fetch.Fetch
	ptimesync         *peersync.Sync
	tortoise          *tortoise.Tortoise
	updater           *bootstrap.Updater
	poetDb            *activation.PoetDb
	postVerifier      activation.PostVerifier
	postSupervisor    *activation.PostSupervisor
	preserve          *checkpoint.PreservedData
	errCh             chan error

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
		return nil, errors.New("restore layer not set")
	}
	nodeIDs := make([]types.NodeID, len(app.signers))
	for i, sig := range app.signers {
		nodeIDs[i] = sig.NodeID()
	}
	cfg := &checkpoint.RecoverConfig{
		GoldenAtx:      types.ATXID(app.Config.Genesis.GoldenATX()),
		DataDir:        app.Config.DataDir(),
		DbFile:         dbFile,
		LocalDbFile:    localDbFile,
		PreserveOwnAtx: app.Config.Recovery.PreserveOwnAtx,
		NodeIDs:        nodeIDs,
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
	lockDir := filepath.Dir(app.Config.FileLock)
	if _, err := os.Stat(lockDir); errors.Is(err, fs.ErrNotExist) {
		err := os.Mkdir(lockDir, os.ModePerm)
		if err != nil {
			return fmt.Errorf("creating dir %s for lock %s: %w", lockDir, app.Config.FileLock, err)
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
	return fmt.Sprintf(
		"App version: %s. Git: %s - %s . Go Version: %s. OS: %s-%s . Genesis %s",
		cmd.Version,
		cmd.Branch,
		cmd.Commit,
		runtime.Version(),
		runtime.GOOS,
		runtime.GOARCH,
		app.Config.Genesis.GenesisID().String(),
	)
}

// Cleanup stops all app services.
func (app *App) Cleanup(ctx context.Context) {
	app.log.Info("app cleanup starting...")
	app.stopServices(ctx)
	app.eg.Wait()
	app.log.Info("app cleanup completed")
}

// Wrap the top-level logger to add context info and set the level for a
// specific module. Calling this method and will create a new logger every time
// and not re-use an existing logger with the same name.
//
// This method is not safe to be called concurrently.
func (app *App) addLogger(name string, logger log.Log) log.Log {
	lvl, err := decodeLoggerLevel(app.Config, name)
	if err != nil {
		app.log.With().Panic("unable to decode loggers into map[string]string", log.Err(err))
	}
	if logger.Check(lvl.Level()) {
		app.loggers[name] = &lvl
		logger = logger.SetLevel(&lvl)
	}
	return logger.WithName(name)
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
	layerSize := app.Config.LayerAvgSize
	layersPerEpoch := types.GetLayersPerEpoch()
	lg := app.log

	poetDb := activation.NewPoetDb(app.db, app.addLogger(PoetDbLogger, lg))
	postStates := activation.NewPostStates(app.addLogger(PostLogger, lg).Zap())
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
			return fmt.Errorf(
				"failed to check genesis account %v: %w",
				genesisAccts[0].Address,
				err,
			)
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

	app.edVerifier = signing.NewEdVerifier(
		signing.WithVerifierPrefix(app.Config.Genesis.GenesisID().Bytes()),
	)

	vrfVerifier := signing.NewVRFVerifier()
	beaconProtocol := beacon.New(
		app.host,
		app.edVerifier,
		vrfVerifier,
		app.cachedDB,
		app.clock,
		beacon.WithConfig(app.Config.Beacon),
		beacon.WithLogger(app.addLogger(BeaconLogger, lg)),
	)
	for _, sig := range app.signers {
		beaconProtocol.Register(sig)
	}

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
	app.eg.Go(func() error {
		for rst := range beaconProtocol.Results() {
			events.EmitBeacon(rst.Epoch, rst.Beacon)
			trtl.OnBeacon(rst.Epoch, rst.Beacon)
		}
		app.log.Debug("beacon results watcher exited")
		return nil
	})

	executor := mesh.NewExecutor(
		app.db,
		app.atxsdata,
		state,
		app.conState,
		app.addLogger(ExecutorLogger, lg),
	)
	mlog := app.addLogger(MeshLogger, lg)
	msh, err := mesh.NewMesh(app.db, app.atxsdata, app.clock, trtl, executor, app.conState, mlog)
	if err != nil {
		return fmt.Errorf("create mesh: %w", err)
	}

	pruner := prune.New(app.db, app.Config.Tortoise.Hdist, app.Config.PruneActivesetsFrom, prune.WithLogger(mlog.Zap()))
	if err := pruner.Prune(app.clock.CurrentLayer()); err != nil {
		return fmt.Errorf("pruner %w", err)
	}
	app.eg.Go(func() error {
		prune.Run(ctx, pruner, app.clock, app.Config.DatabasePruneInterval)
		return nil
	})

	fetcherWrapped := &layerFetcher{}
	atxHandler := activation.NewHandler(
		app.host.ID(),
		app.cachedDB,
		app.atxsdata,
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
	)
	for _, sig := range app.signers {
		atxHandler.Register(sig)
	}

	// we can't have an epoch offset which is greater/equal than the number of layers in an epoch

	if app.Config.HareEligibility.ConfidenceParam >= app.Config.BaseConfig.LayersPerEpoch {
		return fmt.Errorf(
			"confidence param should be smaller than layers per epoch. eligibility-confidence-param: %d. "+
				"layers-per-epoch: %d",
			app.Config.HareEligibility.ConfidenceParam,
			app.Config.BaseConfig.LayersPerEpoch,
		)
	}

	blockHandler := blocks.NewHandler(fetcherWrapped, app.db, trtl, msh,
		blocks.WithLogger(app.addLogger(BlockHandlerLogger, lg)))

	app.txHandler = txs.NewTxHandler(
		app.conState,
		app.host.ID(),
		app.addLogger(TxHandlerLogger, lg),
	)

	app.hOracle = eligibility.New(
		beaconProtocol,
		app.db,
		app.atxsdata,
		vrfVerifier,
		app.Config.LayersPerEpoch,
		eligibility.WithConfig(app.Config.HareEligibility),
		eligibility.WithLogger(app.addLogger(HareOracleLogger, lg)),
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
	if app.Config.Certificate.CommitteeSize == 0 {
		app.log.With().Warning("certificate committee size is not set, defaulting to hare committee size",
			log.Uint16("size", app.Config.HARE3.Committee))
		app.Config.Certificate.CommitteeSize = int(app.Config.HARE3.Committee)
	}
	app.Config.Certificate.CertifyThreshold = app.Config.Certificate.CommitteeSize/2 + 1
	app.Config.Certificate.LayerBuffer = app.Config.Tortoise.Zdist
	app.Config.Certificate.NumLayersToKeep = app.Config.Tortoise.Zdist * 2
	app.certifier = blocks.NewCertifier(
		app.db,
		app.hOracle,
		app.edVerifier,
		app.host,
		app.clock,
		beaconProtocol,
		trtl,
		blocks.WithCertConfig(app.Config.Certificate),
		blocks.WithCertifierLogger(app.addLogger(BlockCertLogger, lg)),
	)
	for _, sig := range app.signers {
		app.certifier.Register(sig)
	}

	proposalsStore := store.New(
		store.WithEvictedLayer(app.clock.CurrentLayer()),
		store.WithLogger(app.addLogger(ProposalStoreLogger, lg).Zap()),
		store.WithCapacity(app.Config.Tortoise.Zdist+1),
	)

	flog := app.addLogger(Fetcher, lg)
	fetcher := fetch.NewFetch(app.cachedDB, proposalsStore, app.host,
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

	if app.Config.P2P.MinPeers < app.Config.Sync.MalSync.MinSyncPeers {
		app.Config.Sync.MalSync.MinSyncPeers = max(1, app.Config.P2P.MinPeers)
	}
	app.syncLogger = app.addLogger(SyncLogger, lg)
	newSyncer := syncer.NewSyncer(
		app.cachedDB,
		app.clock,
		beaconProtocol,
		msh,
		trtl,
		fetcher,
		patrol,
		app.certifier,
		atxsync.New(fetcher, app.db, app.localDB,
			atxsync.WithConfig(app.Config.Sync.AtxSync),
			atxsync.WithLogger(app.syncLogger.Zap()),
		),
		malsync.New(fetcher, app.db, app.localDB,
			malsync.WithConfig(app.Config.Sync.MalSync),
			malsync.WithLogger(app.syncLogger.Zap()),
			malsync.WithPeerErrMetric(syncer.MalPeerError),
		),
		syncer.WithConfig(syncerConf),
		syncer.WithLogger(app.syncLogger),
	)
	// TODO(dshulyak) this needs to be improved, but dependency graph is a bit complicated
	beaconProtocol.SetSyncState(newSyncer)
	app.hOracle.SetSync(newSyncer)

	err = app.Config.HARE3.Validate(time.Duration(app.Config.Tortoise.Zdist) * app.Config.LayerDuration)
	if err != nil {
		return err
	}
	logger := app.addLogger(HareLogger, lg).Zap()

	app.hare3 = hare3.New(
		app.clock,
		app.host,
		app.db,
		app.atxsdata,
		proposalsStore,
		app.edVerifier,
		app.hOracle,
		newSyncer,
		patrol,
		hare3.WithLogger(logger),
		hare3.WithConfig(app.Config.HARE3),
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
			tortoiseWeakCoin{db: app.cachedDB, tortoise: trtl},
		)
		return nil
	})

	proposalListener := proposals.NewHandler(
		app.db,
		app.atxsdata,
		app.hare3,
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

	app.blockGen = blocks.NewGenerator(
		app.db,
		app.atxsdata,
		proposalsStore,
		executor,
		msh,
		fetcherWrapped,
		app.certifier,
		patrol,
		blocks.WithConfig(blocks.Config{
			BlockGasLimit:      app.Config.BlockGasLimit,
			OptFilterThreshold: app.Config.OptFilterThreshold,
			GenBlockInterval:   500 * time.Millisecond,
		}),
		blocks.WithHareOutputChan(app.hare3.Results()),
		blocks.WithGeneratorLogger(app.addLogger(BlockGenLogger, lg)),
	)

	minerGoodAtxPct := 90
	if app.Config.MinerGoodAtxsPercent > 0 {
		minerGoodAtxPct = app.Config.MinerGoodAtxsPercent
	}
	proposalBuilder := miner.New(
		app.clock,
		app.db,
		app.localDB,
		app.atxsdata,
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
		miner.WithActivesetPreparation(app.Config.ActiveSet),
	)
	for _, sig := range app.signers {
		proposalBuilder.Register(sig)
	}

	postSetupMgr, err := activation.NewPostSetupManager(
		app.Config.POST,
		app.addLogger(PostLogger, lg).Zap(),
		app.cachedDB,
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
	nipostBuilder, err := activation.NewNIPostBuilder(
		app.localDB,
		poetDb,
		grpcPostService.(*grpcserver.PostService),
		app.Config.PoetServers,
		app.addLogger(NipostBuilderLogger, lg).Zap(),
		app.Config.POET,
		app.clock,
		activation.NipostbuilderWithPostStates(postStates),
	)
	if err != nil {
		return fmt.Errorf("create nipost builder: %w", err)
	}

	builderConfig := activation.Config{
		GoldenATXID:      goldenATXID,
		LabelsPerUnit:    app.Config.POST.LabelsPerUnit,
		RegossipInterval: app.Config.RegossipAtxInterval,
	}
	atxBuilder := activation.NewBuilder(
		builderConfig,
		app.cachedDB,
		app.localDB,
		app.host,
		nipostBuilder,
		app.clock,
		newSyncer,
		app.addLogger(ATXBuilderLogger, lg).Zap(),
		activation.WithContext(ctx),
		activation.WithPoetConfig(app.Config.POET),
		// TODO(dshulyak) makes no sense. how we ended using it?
		activation.WithPoetRetryInterval(app.Config.HARE3.PreroundDelay),
		activation.WithValidator(app.validator),
		activation.WithPostValidityDelay(app.Config.PostValidDelay),
		activation.WithPostStates(postStates),
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

	nodeIDs := make([]types.NodeID, 0, len(app.signers))
	for _, s := range app.signers {
		nodeIDs = append(nodeIDs, s.NodeID())
	}
	malfeasanceHandler := malfeasance.NewHandler(
		app.cachedDB,
		app.addLogger(MalfeasanceLogger, lg),
		app.host.ID(),
		nodeIDs,
		app.edVerifier,
		trtl,
		app.postVerifier,
	)
	fetcher.SetValidators(
		fetch.ValidatorFunc(
			pubsub.DropPeerOnSyncValidationReject(atxHandler.HandleSyncedAtx, app.host, lg),
		),
		fetch.ValidatorFunc(
			pubsub.DropPeerOnSyncValidationReject(poetDb.ValidateAndStoreMsg, app.host, lg),
		),
		fetch.ValidatorFunc(
			pubsub.DropPeerOnSyncValidationReject(
				proposalListener.HandleSyncedBallot,
				app.host,
				lg,
			),
		),
		fetch.ValidatorFunc(
			pubsub.DropPeerOnSyncValidationReject(proposalListener.HandleActiveSet, app.host, lg),
		),
		fetch.ValidatorFunc(
			pubsub.DropPeerOnSyncValidationReject(blockHandler.HandleSyncedBlock, app.host, lg),
		),
		fetch.ValidatorFunc(
			pubsub.DropPeerOnSyncValidationReject(
				proposalListener.HandleSyncedProposal,
				app.host,
				lg,
			),
		),
		fetch.ValidatorFunc(
			pubsub.DropPeerOnSyncValidationReject(
				app.txHandler.HandleBlockTransaction,
				app.host,
				lg,
			),
		),
		fetch.ValidatorFunc(
			pubsub.DropPeerOnSyncValidationReject(
				app.txHandler.HandleProposalTransaction,
				app.host,
				lg,
			),
		),
		fetch.ValidatorFunc(
			pubsub.DropPeerOnSyncValidationReject(
				malfeasanceHandler.HandleSyncedMalfeasanceProof,
				app.host,
				lg,
			),
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
	app.nipostBuilder = nipostBuilder
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
	if len(app.Config.PoetServers) != 1 {
		return fmt.Errorf(
			"to launch in a standalone mode provide single local address for poet: %v",
			app.Config.PoetServers,
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

	parsed, err := url.Parse(app.Config.PoetServers[0].Address)
	if err != nil {
		return err
	}

	cfg.RawRESTListener = parsed.Host
	cfg.RawRPCListener = parsed.Hostname() + ":0"
	if err := cfg.Genesis.UnmarshalFlag(app.Config.Genesis.GenesisTime); err != nil {
		return err
	}
	cfg.Round.EpochDuration = app.Config.LayerDuration * time.Duration(app.Config.LayersPerEpoch)
	cfg.Round.CycleGap = app.Config.POET.CycleGap
	cfg.Round.PhaseShift = app.Config.POET.PhaseShift
	server.SetupConfig(cfg)

	srv, err := server.New(ctx, *cfg)
	if err != nil {
		return fmt.Errorf("init poet server: %w", err)
	}

	app.Config.PoetServers[0].Pubkey = types.NewBase64Enc(srv.PublicKey())
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
						select {
						case <-app.syncer.RegisterForATXSynced():
						case <-ctx.Done():
							return nil
						}
						if err := atxsync.Download(
							ctx,
							10*time.Second,
							app.syncLogger.Zap(),
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
		return fmt.Errorf("start fetcher: %w", err)
	}
	app.syncer.Start()
	app.beaconProtocol.Start(ctx)

	app.blockGen.Start(ctx)
	app.certifier.Start(ctx)
	app.eg.Go(func() error {
		return app.proposalBuilder.Run(ctx)
	})

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

func (app *App) grpcService(svc grpcserver.Service, lg log.Log) (grpcserver.ServiceAPI, error) {
	if service, ok := app.grpcServices[svc]; ok {
		return service, nil
	}

	switch svc {
	case grpcserver.Debug:
		service := grpcserver.NewDebugService(app.db, app.conState, app.host, app.hOracle, app.loggers)
		app.grpcServices[svc] = service
		return service, nil
	case grpcserver.GlobalState:
		service := grpcserver.NewGlobalStateService(app.mesh, app.conState)
		app.grpcServices[svc] = service
		return service, nil
	case grpcserver.Mesh:
		service := grpcserver.NewMeshService(
			app.cachedDB,
			app.mesh,
			app.conState,
			app.clock,
			app.Config.LayersPerEpoch,
			app.Config.Genesis.GenesisID(),
			app.Config.LayerDuration,
			app.Config.LayerAvgSize,
			uint32(app.Config.TxsPerProposal),
		)
		app.grpcServices[svc] = service
		return service, nil
	case grpcserver.Node:
		service := grpcserver.NewNodeService(
			app.host,
			app.mesh,
			app.clock,
			app.syncer,
			cmd.Version,
			cmd.Commit,
		)
		app.grpcServices[svc] = service
		return service, nil
	case grpcserver.Admin:
		service := grpcserver.NewAdminService(app.db, app.Config.DataDir(), app.host)
		app.grpcServices[svc] = service
		return service, nil
	case grpcserver.Smesher:
		var sig *signing.EdSigner
		if len(app.signers) == 1 && app.signers[0].Name() == supervisedIDKeyFileName {
			// StartSmeshing is only supported in a supervised setup (single signer)
			sig = app.signers[0]
		}
		postService, err := app.grpcService(grpcserver.Post, lg)
		if err != nil {
			return nil, err
		}
		service := grpcserver.NewSmesherService(
			app.atxBuilder,
			app.postSupervisor,
			postService.(*grpcserver.PostService),
			app.Config.API.SmesherStreamInterval,
			app.Config.SMESHING.Opts,
			sig,
		)
		app.grpcServices[svc] = service
		return service, nil
	case grpcserver.Post:
		service := grpcserver.NewPostService(app.addLogger(PostServiceLogger, lg).Zap())
		isCoinbaseSet := app.Config.SMESHING.CoinbaseAccount != ""
		if !isCoinbaseSet {
			lg.Warning("coinbase account is not set, connections from remote post services will be rejected")
		}
		service.AllowConnections(isCoinbaseSet)
		app.grpcServices[svc] = service
		return service, nil
	case grpcserver.PostInfo:
		service := grpcserver.NewPostInfoService(app.addLogger(PostInfoServiceLogger, lg).Zap(), app.atxBuilder)
		app.grpcServices[svc] = service
		return service, nil
	case grpcserver.Transaction:
		service := grpcserver.NewTransactionService(
			app.db,
			app.host,
			app.mesh,
			app.conState,
			app.syncer,
			app.txHandler,
		)
		app.grpcServices[svc] = service
		return service, nil
	case grpcserver.Activation:
		service := grpcserver.NewActivationService(app.cachedDB, types.ATXID(app.Config.Genesis.GoldenATX()))
		app.grpcServices[svc] = service
		return service, nil
	case v2alpha1.Activation:
		service := v2alpha1.NewActivationService(app.db)
		app.grpcServices[svc] = service
		return service, nil
	case v2alpha1.ActivationStream:
		service := v2alpha1.NewActivationStreamService(app.db)
		app.grpcServices[svc] = service
		return service, nil
	case v2alpha1.Reward:
		service := v2alpha1.NewRewardService(app.db)
		app.grpcServices[svc] = service
		return service, nil
	case v2alpha1.RewardStream:
		service := v2alpha1.NewRewardStreamService(app.db)
		app.grpcServices[svc] = service
		return service, nil
	case v2alpha1.Network:
		service := v2alpha1.NewNetworkService(
			app.clock.GenesisTime(),
			app.Config.Genesis.GenesisID(),
			app.Config.LayerDuration)
		app.grpcServices[svc] = service
		return service, nil
	case v2alpha1.Node:
		service := v2alpha1.NewNodeService(app.host, app.mesh, app.clock, app.syncer)
		app.grpcServices[svc] = service
		return service, nil
	}
	return nil, fmt.Errorf("unknown service %s", svc)
}

func (app *App) startAPIServices(ctx context.Context) error {
	logger := app.addLogger(GRPCLogger, app.log)
	grpczap.SetGrpcLoggerV2(grpclog, logger.Zap())

	var (
		publicSvcs        = make(map[grpcserver.Service]grpcserver.ServiceAPI, len(app.Config.API.PublicServices))
		privateSvcs       = make(map[grpcserver.Service]grpcserver.ServiceAPI, len(app.Config.API.PrivateServices))
		postSvcs          = make(map[grpcserver.Service]grpcserver.ServiceAPI, len(app.Config.API.PostServices))
		authenticatedSvcs = make(map[grpcserver.Service]grpcserver.ServiceAPI, len(app.Config.API.TLSServices))
	)

	// check services for uniques across all endpoints
	for _, svc := range app.Config.API.PublicServices {
		if _, exists := publicSvcs[svc]; exists {
			return fmt.Errorf("can't start more than one %s on public grpc endpoint", svc)
		}
		gsvc, err := app.grpcService(svc, app.log)
		if err != nil {
			return err
		}
		logger.Info("registering public service %s", svc)
		publicSvcs[svc] = gsvc
	}
	for _, svc := range app.Config.API.PrivateServices {
		if _, exists := privateSvcs[svc]; exists {
			return fmt.Errorf("can't start more than one %s on private grpc endpoint", svc)
		}
		gsvc, err := app.grpcService(svc, app.log)
		if err != nil {
			return err
		}
		logger.Info("registering private service %s", svc)
		privateSvcs[svc] = gsvc
	}
	for _, svc := range app.Config.API.PostServices {
		if _, exists := postSvcs[svc]; exists {
			return fmt.Errorf("can't start more than one %s on post grpc endpoint", svc)
		}
		gsvc, err := app.grpcService(svc, app.log)
		if err != nil {
			return err
		}
		logger.Info("registering post service %s", svc)
		postSvcs[svc] = gsvc
	}
	for _, svc := range app.Config.API.TLSServices {
		if _, exists := authenticatedSvcs[svc]; exists {
			return fmt.Errorf("can't start more than one %s on authenticated grpc endpoint", svc)
		}
		gsvc, err := app.grpcService(svc, app.log)
		if err != nil {
			return err
		}
		logger.Info("registering authenticated service %s", svc)
		authenticatedSvcs[svc] = gsvc
	}

	// start servers if at least one endpoint is defined for them
	if len(publicSvcs) > 0 {
		var err error
		app.grpcPublicServer, err = grpcserver.NewWithServices(
			app.Config.API.PublicListener,
			logger.Zap(),
			app.Config.API,
			maps.Values(publicSvcs),
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
		logger.With().Info("public grpc service started",
			log.String("address", app.Config.API.PublicListener),
			log.Array("services", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
				services := maps.Keys(publicSvcs)
				slices.Sort(services)
				for _, svc := range services {
					encoder.AppendString(svc)
				}
				return nil
			})),
		)
	}
	if len(privateSvcs) > 0 {
		var err error
		app.grpcPrivateServer, err = grpcserver.NewWithServices(
			app.Config.API.PrivateListener,
			logger.Zap(),
			app.Config.API,
			maps.Values(privateSvcs),
		)
		if err != nil {
			return err
		}
		if err := app.grpcPrivateServer.Start(); err != nil {
			return err
		}
		logger.With().Info("private grpc service started",
			log.String("address", app.Config.API.PrivateListener),
			log.Array("services", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
				services := maps.Keys(privateSvcs)
				slices.Sort(services)
				for _, svc := range services {
					encoder.AppendString(svc)
				}
				return nil
			})),
		)
	}
	if len(postSvcs) > 0 && app.Config.API.PostListener != "" {
		var err error
		app.grpcPostServer, err = grpcserver.NewWithServices(
			app.Config.API.PostListener,
			logger.Zap(),
			app.Config.API,
			maps.Values(postSvcs),
		)
		if err != nil {
			return err
		}
		if err := app.grpcPostServer.Start(); err != nil {
			return err
		}
		logger.With().Info("post grpc service started",
			log.String("address", app.Config.API.PostListener),
			log.Array("services", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
				services := maps.Keys(postSvcs)
				slices.Sort(services)
				for _, svc := range services {
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
		app.Config.POSTService.NodeAddress = fmt.Sprintf("http://%s:%s", host, port)
		svc, err := app.grpcService(grpcserver.Smesher, app.log)
		if err != nil {
			return err
		}
		svc.(*grpcserver.SmesherService).SetPostServiceConfig(app.Config.POSTService)
		if app.Config.SMESHING.Start {
			if app.Config.SMESHING.CoinbaseAccount == "" {
				return errors.New("smeshing enabled but no coinbase account provided")
			}
			if len(app.signers) > 1 {
				return errors.New("supervised smeshing cannot be started in a multi-smeshing setup")
			}
			if err := app.postSupervisor.Start(
				app.Config.POSTService,
				app.Config.SMESHING.Opts,
				app.signers[0],
			); err != nil {
				return fmt.Errorf("start post service: %w", err)
			}
		} else if len(app.signers) == 1 && app.signers[0].Name() == supervisedIDKeyFileName {
			// supervised setup but not started
			app.log.Info("smeshing not started, waiting to be triggered via smesher api")
		}
	}

	if len(authenticatedSvcs) > 0 && app.Config.API.TLSListener != "" {
		var err error
		app.grpcTLSServer, err = grpcserver.NewTLS(logger.Zap(), app.Config.API, maps.Values(authenticatedSvcs))
		if err != nil {
			return err
		}
		if err := app.grpcTLSServer.Start(); err != nil {
			return err
		}
		logger.With().Info("authenticated grpc service started",
			log.String("address", app.Config.API.TLSListener),
			log.Array("services", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
				services := maps.Keys(authenticatedSvcs)
				slices.Sort(services)
				for _, svc := range services {
					encoder.AppendString(svc)
				}
				return nil
			})),
		)
	}

	if len(app.Config.API.JSONListener) > 0 {
		if len(publicSvcs) == 0 {
			return errors.New("start json server without public services")
		}
		app.jsonAPIServer = grpcserver.NewJSONHTTPServer(
			app.Config.API.JSONListener,
			logger.Zap().Named("JSON"),
		)
		if err := app.jsonAPIServer.StartService(ctx, maps.Values(publicSvcs)...); err != nil {
			return fmt.Errorf("start listen server: %w", err)
		}
		logger.With().Info("json listener started",
			log.String("address", app.Config.API.JSONListener),
			log.Array("services", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
				services := maps.Keys(publicSvcs)
				slices.Sort(services)
				for _, svc := range services {
					encoder.AppendString(svc)
				}
				return nil
			})),
		)
	}
	return nil
}

func (app *App) stopServices(ctx context.Context) {
	if app.jsonAPIServer != nil {
		if err := app.jsonAPIServer.Shutdown(ctx); err != nil {
			app.log.With().Error("error stopping json gateway server", log.Err(err))
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
		app.atxBuilder.StopSmeshing(false)
	}

	if app.postVerifier != nil {
		app.postVerifier.Close()
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

	if app.postSupervisor != nil {
		if err := app.postSupervisor.Stop(false); err != nil {
			app.log.With().Error("error stopping local post service", log.Err(err))
		}
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
	if app.localDB != nil {
		if err := app.localDB.Close(); err != nil {
			app.log.With().Warning("local db exited with error", log.Err(err))
		}
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

func (app *App) setupDBs(ctx context.Context, lg log.Log) error {
	dbPath := app.Config.DataDir()
	if err := os.MkdirAll(dbPath, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create %s: %w", dbPath, err)
	}
	migrations, err := sql.StateMigrations()
	if err != nil {
		return fmt.Errorf("failed to load migrations: %w", err)
	}
	dbLog := app.addLogger(StateDbLogger, lg)
	dbopts := []sql.Opt{
		sql.WithLogger(dbLog.Zap()),
		sql.WithMigrations(migrations),
		sql.WithMigration(sqlmigrations.New0017Migration(dbLog.Zap())),
		sql.WithConnections(app.Config.DatabaseConnections),
		sql.WithLatencyMetering(app.Config.DatabaseLatencyMetering),
		sql.WithVacuumState(app.Config.DatabaseVacuumState),
		sql.WithQueryCache(app.Config.DatabaseQueryCache),
		sql.WithQueryCacheSizes(map[sql.QueryCacheKind]int{
			atxs.CacheKindEpochATXs:           app.Config.DatabaseQueryCacheSizes.EpochATXs,
			atxs.CacheKindATXBlob:             app.Config.DatabaseQueryCacheSizes.ATXBlob,
			activesets.CacheKindActiveSetBlob: app.Config.DatabaseQueryCacheSizes.ActiveSetBlob,
		}),
	}
	if len(app.Config.DatabaseSkipMigrations) > 0 {
		dbopts = append(dbopts, sql.WithSkipMigrations(app.Config.DatabaseSkipMigrations...))
	}
	sqlDB, err := sql.Open("file:"+filepath.Join(dbPath, dbFile), dbopts...)
	if err != nil {
		return fmt.Errorf("open sqlite db %w", err)
	}
	app.db = sqlDB
	if app.Config.CollectMetrics && app.Config.DatabaseSizeMeteringInterval != 0 {
		app.dbMetrics = dbmetrics.NewDBMetricsCollector(
			ctx,
			app.db,
			dbLog,
			app.Config.DatabaseSizeMeteringInterval,
		)
	}
	app.log.Info("starting cache warmup")
	applied, err := layers.GetLastApplied(app.db)
	if err != nil {
		return err
	}
	start := time.Now()
	data, err := atxsdata.Warm(
		app.db,
		app.Config.Tortoise.WindowSizeEpochs(applied),
	)
	if err != nil {
		return err
	}
	app.atxsdata = data
	app.log.With().Info("cache warmup", log.Duration("duration", time.Since(start)))
	app.cachedDB = datastore.NewCachedDB(sqlDB, app.addLogger(CachedDBLogger, lg),
		datastore.WithConfig(app.Config.Cache),
	)

	if app.Config.ScanMalfeasantATXs {
		app.log.With().Info("checking DB for malicious ATXs")
		start = time.Now()
		if err := activation.CheckPrevATXs(ctx, app.log.Zap(), app.db); err != nil {
			return fmt.Errorf("malicious ATX check: %w", err)
		}
		app.log.With().Info("malicious ATX check completed", log.Duration("duration", time.Since(start)))
	}

	migrations, err = sql.LocalMigrations()
	if err != nil {
		return fmt.Errorf("load local migrations: %w", err)
	}
	localDB, err := localsql.Open("file:"+filepath.Join(dbPath, localDbFile),
		sql.WithLogger(dbLog.Zap()),
		sql.WithMigrations(migrations),
		sql.WithConnections(app.Config.DatabaseConnections),
	)
	if err != nil {
		return fmt.Errorf("open sqlite db %w", err)
	}
	app.localDB = localDB
	return nil
}

// Start starts the Spacemesh node and initializes all relevant services according to command line arguments provided.
func (app *App) Start(ctx context.Context) error {
	if err := app.verifyVersionUpgrades(); err != nil {
		return fmt.Errorf("version upgrade verification failed: %w", err)
	}

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

	/* Initialize all protocol services */

	gTime, err := time.Parse(time.RFC3339, app.Config.Genesis.GenesisTime)
	if err != nil {
		return fmt.Errorf("cannot parse genesis time %s: %w", app.Config.Genesis.GenesisTime, err)
	}
	app.clock, err = timesync.NewClock(
		timesync.WithLayerDuration(app.Config.LayerDuration),
		timesync.WithTickInterval(1*time.Second),
		timesync.WithGenesisTime(gTime),
		timesync.WithLogger(app.addLogger(ClockLogger, logger).Zap()),
	)
	if err != nil {
		return fmt.Errorf("cannot create clock: %w", err)
	}

	logger.Info("initializing p2p services")

	cfg := app.Config.P2P
	cfg.DataDir = filepath.Join(app.Config.DataDir(), "p2p")
	p2plog := app.addLogger(P2PLogger, logger)
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
		return fmt.Errorf("initialize p2p host: %w", err)
	}

	if err := app.setupDBs(ctx, logger); err != nil {
		return err
	}
	if err := app.initServices(ctx); err != nil {
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

	if err := app.startServices(ctx); err != nil {
		return fmt.Errorf("start services: %w", err)
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
				log.Object("poet proof", poetProof),
				log.Err(err),
			)
			continue
		}
		ref, err := poetProof.Ref()
		if err != nil {
			app.log.With().Error("failed to get poet proof ref after checkpoint", log.Inline(poetProof), log.Err(err))
			continue
		}
		hash := types.Hash32(ref)
		if err := app.poetDb.ValidateAndStoreMsg(ctx, hash, p2p.NoPeer, encoded); err != nil {
			app.log.With().Error("failed to preserve poet proof after checkpoint",
				log.Stringer("atx id", app.preserve.Deps[i].ID),
				log.String("poet proof ref", hash.ShortString()),
				log.Err(err),
			)
			continue
		}
		app.log.With().Info("preserved poet proof after checkpoint",
			log.Stringer("atx id", app.preserve.Deps[i].ID),
			log.String("poet proof ref", hash.ShortString()),
		)
	}
	for _, atx := range app.preserve.Deps {
		if err := app.atxHandler.HandleSyncedAtx(ctx, atx.ID.Hash32(), p2p.NoPeer, atx.Blob); err != nil {
			app.log.With().Error(
				"failed to preserve atx after checkpoint",
				log.ShortStringer("id", atx.ID),
				log.Err(err),
			)
			continue
		}
		app.log.With().Info("preserved atx after checkpoint", log.ShortStringer("id", atx.ID))
	}
}

func (app *App) Host() *p2p.Host {
	return app.host
}

type layerFetcher struct {
	system.Fetcher
}

func decodeLoggerLevel(cfg *config.Config, name string) (zap.AtomicLevel, error) {
	lvl := zap.NewAtomicLevel()
	loggers := map[string]string{}
	if err := mapstructure.Decode(cfg.LOGGING, &loggers); err != nil {
		return zap.AtomicLevel{}, fmt.Errorf("error decoding mapstructure: %w", err)
	}

	level, ok := loggers[name]
	if ok {
		if err := lvl.UnmarshalText([]byte(level)); err != nil {
			return zap.AtomicLevel{}, fmt.Errorf("cannot parse logging for %v: %w", name, err)
		}
	} else {
		lvl.SetLevel(log.DefaultLevel())
	}

	return lvl, nil
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
