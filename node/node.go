// Package node contains the main executable for go-spacemesh node
package node

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/config/presets"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/hare3"
	"github.com/spacemeshos/go-spacemesh/hare3/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare4"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/malfeasance"
	"github.com/spacemeshos/go-spacemesh/malfeasance2"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/metrics"
	"github.com/spacemeshos/go-spacemesh/metrics/public"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/node/mapstructureutil"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/handshake"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/activesets"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	localmigrations "github.com/spacemeshos/go-spacemesh/sql/localsql/migrations"
	dbmetrics "github.com/spacemeshos/go-spacemesh/sql/metrics"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	statemigrations "github.com/spacemeshos/go-spacemesh/sql/statesql/migrations"
	"github.com/spacemeshos/go-spacemesh/syncer"
	"github.com/spacemeshos/go-spacemesh/syncer/atxsync"
	"github.com/spacemeshos/go-spacemesh/system"
	"github.com/spacemeshos/go-spacemesh/timesync"
	timeCfg "github.com/spacemeshos/go-spacemesh/timesync/config"
	"github.com/spacemeshos/go-spacemesh/timesync/peersync"
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
	ApiStateDBLogger       = "apiStateDB"
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
	Malfeasance2Logger     = "malfeasance2"
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
	f, err := os.Open(configPath)
	if err != nil {
		return fmt.Errorf("opening config file: %w", err)
	}
	defer f.Close()
	if err := LoadConfig(conf, conf.Preset, f); err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing config file: %w", err)
	}
	// apply CLI args to config
	if err := c.ParseFlags(os.Args[1:]); err != nil {
		return fmt.Errorf("parsing flags: %w", err)
	}
	if cmd.NoMainNet && onMainNet(conf) && !conf.NoMainOverride {
		return errors.New("this is a testnet-only build not intended for mainnet")
	}
	return nil
}

var grpcLog = grpc_logsettable.ReplaceGrpcLoggerV2()

// LoadConfig loads config and preset (if provided) into the provided config.
// It first loads the preset and then overrides it with values from the config file.
func LoadConfig(cfg *config.Config, preset string, src io.Reader) error {
	v := viper.New()
	// read in config from src
	if src != nil {
		v.SetConfigType("json")
		if err := v.ReadConfig(src); err != nil {
			return fmt.Errorf("can't load config: %w", err)
		}
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

	// Unmarshal config file into config struct
	hook := mapstructure.ComposeDecodeHookFunc(
		mapstructure.StringToTimeDurationHookFunc(),
		mapstructure.StringToSliceHookFunc(","),
		mapstructureutil.AddressListDecodeFunc(),
		mapstructureutil.BigRatDecodeFunc(),
		mapstructureutil.PostProviderIDDecodeFunc(),
		mapstructureutil.DeprecatedHook(),
		mapstructureutil.AtxVersionsDecodeFunc(),
		mapstructure.TextUnmarshallerHookFunc(),
	)
	opts := []viper.DecoderConfigOption{
		viper.DecodeHook(hook),
		WithZeroFields(),
		WithIgnoreUntagged(),
		WithErrorUnused(),
	}
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
		log:          log.NewNop(),
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
	fileLock            *flock.Flock
	signers             []*signing.EdSigner
	Config              *config.Config
	db                  sql.StateDatabase
	apiDB               sql.StateDatabase
	cachedDB            *datastore.CachedDB
	dbMetrics           *dbmetrics.DBMetricsCollector
	localDB             sql.LocalDatabase
	grpcPublicServer    *grpcserver.Server
	grpcPrivateServer   *grpcserver.Server
	grpcPostServer      *grpcserver.Server
	grpcTLSServer       *grpcserver.Server
	jsonAPIServer       *grpcserver.JSONHTTPServer
	grpcServices        map[grpcserver.Service]grpcserver.ServiceAPI
	pprofService        *http.Server
	profilerService     *pyroscope.Profiler
	syncer              *syncer.Syncer
	proposalBuilder     *miner.ProposalBuilder
	mesh                *mesh.Mesh
	atxsdata            *atxsdata.Data
	clock               *timesync.NodeClock
	hare3               *hare3.Hare
	hare4               *hare4.Hare
	hareResultsChan     chan hare4.ConsensusOutput
	hOracle             *eligibility.Oracle
	blockGen            *blocks.Generator
	certifier           *blocks.Certifier
	atxBuilder          *activation.Builder
	atxHandler          *activation.Handler
	txHandler           *txs.TxHandler
	validator           *activation.Validator
	edVerifier          *signing.EdVerifier
	beaconProtocol      *beacon.ProtocolDriver
	log                 log.Log
	syncLogger          log.Log
	conState            *txs.ConservativeState
	fetcher             *fetch.Fetch
	ptimesync           *peersync.Sync
	updater             *bootstrap.Updater
	poetDb              *activation.PoetDb
	postVerifier        activation.PostVerifier
	postSupervisor      *activation.PostSupervisor
	malfeasanceHandler  *malfeasance.Handler
	malfeasance2Handler *malfeasance2.Handler
	errCh               chan error

	host *p2p.Host

	loggers map[string]*zap.AtomicLevel
	started chan struct{} // this channel is closed once the app has finished starting
	eg      *errgroup.Group
}

func (app *App) loadCheckpoint(ctx context.Context) (*checkpoint.PreservedData, error) {
	nodeIDs := make([]types.NodeID, 0, len(app.signers))
	if app.Config.Recovery.PreserveOwnAtx {
		for _, sig := range app.signers {
			nodeIDs = append(nodeIDs, sig.NodeID())
		}
	}
	cfg := &checkpoint.RecoverConfig{
		GoldenAtx:   types.ATXID(app.Config.Genesis.GoldenATX()),
		DataDir:     app.Config.DataDir(),
		DbFile:      dbFile,
		LocalDbFile: localDbFile,
		NodeIDs:     nodeIDs,
		Uri:         app.Config.Recovery.Uri,
		Restore:     types.LayerID(app.Config.Recovery.Restore),
	}

	return checkpoint.Recover(ctx, app.log.Zap(), afero.NewOsFs(), cfg)
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
	cfg.Genesis = server.Genesis(app.Config.Genesis.GenesisTime)
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
					err := activesets.Add(app.db, id, activeSet)
					if err != nil && !errors.Is(err, sql.ErrObjectExists) {
						app.errCh <- fmt.Errorf("error storing ActiveSet: %w", err)
						return nil
					}

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
		service := grpcserver.NewPostInfoService(app.atxBuilder)
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
		service := v2alpha1.NewActivationService(app.apiDB, types.ATXID(app.Config.Genesis.GoldenATX()))
		app.grpcServices[svc] = service
		return service, nil
	case v2alpha1.ActivationStream:
		service := v2alpha1.NewActivationStreamService(app.apiDB)
		app.grpcServices[svc] = service
		return service, nil
	case v2alpha1.Reward:
		service := v2alpha1.NewRewardService(app.apiDB)
		app.grpcServices[svc] = service
		return service, nil
	case v2alpha1.RewardStream:
		service := v2alpha1.NewRewardStreamService(app.apiDB)
		app.grpcServices[svc] = service
		return service, nil
	case v2alpha1.Malfeasance:
		service := v2alpha1.NewMalfeasanceService(app.apiDB, app.malfeasance2Handler, app.malfeasanceHandler)
		app.grpcServices[svc] = service
		return service, nil
	case v2alpha1.MalfeasanceStream:
		service := v2alpha1.NewMalfeasanceStreamService(app.apiDB, app.malfeasance2Handler, app.malfeasanceHandler)
		app.grpcServices[svc] = service
		return service, nil
	case v2alpha1.Network:
		service := v2alpha1.NewNetworkService(
			app.clock.GenesisTime(),
			app.Config,
		)
		app.grpcServices[svc] = service
		return service, nil
	case v2alpha1.Node:
		service := v2alpha1.NewNodeService(app.host, app.mesh, app.clock, app.syncer)
		app.grpcServices[svc] = service
		return service, nil
	case v2alpha1.Layer:
		service := v2alpha1.NewLayerService(app.apiDB)
		app.grpcServices[svc] = service
		return service, nil
	case v2alpha1.LayerStream:
		service := v2alpha1.NewLayerStreamService(app.apiDB)
		app.grpcServices[svc] = service
		return service, nil
	case v2alpha1.Transaction:
		service := v2alpha1.NewTransactionService(app.apiDB, app.conState, app.syncer, app.txHandler, app.host)
		app.grpcServices[svc] = service
		return service, nil
	case v2alpha1.TransactionStream:
		service := v2alpha1.NewTransactionStreamService()
		app.grpcServices[svc] = service
		return service, nil
	case v2alpha1.Account:
		service := v2alpha1.NewAccountService(app.apiDB, app.conState)
		app.grpcServices[svc] = service
		return service, nil
	}
	return nil, fmt.Errorf("unknown service %s", svc)
}

func (app *App) startAPIServices(ctx context.Context) error {
	logger := app.addLogger(GRPCLogger, app.log)
	grpczap.SetGrpcLoggerV2(grpcLog, logger.Zap())

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
			log.Array("services", zapcore.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
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
			log.Array("services", zapcore.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
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
			log.Array("services", zapcore.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
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
			if len(app.signers) > 1 || app.signers[0].Name() != supervisedIDKeyFileName {
				app.log.Error("supervised smeshing cannot be started in a remote or multi-smeshing setup")
				app.log.Error(
					"if you run a supervised node ensure your key file is named %s and try again",
					supervisedIDKeyFileName,
				)
				return errors.New("smeshing enabled in remote setup")
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
			log.Array("services", zapcore.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
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
			logger.Zap().Named("JSON"),
			app.Config.API.JSONListener,
			app.Config.API.JSONCorsAllowedOrigins,
			app.Config.CollectMetrics,
		)

		if err := app.jsonAPIServer.StartService(maps.Values(publicSvcs)...); err != nil {
			return fmt.Errorf("start listen server: %w", err)
		}
		logger.With().Info("json listener started",
			log.String("address", app.Config.API.JSONListener),
			log.Array("services", zapcore.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
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

	if app.hare4 != nil {
		app.hare4.Stop()
	}

	if app.hareResultsChan != nil {
		close(app.hareResultsChan)
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
	if app.apiDB != nil {
		if err := app.apiDB.Close(); err != nil {
			app.log.With().Warning("api db exited with error", log.Err(err))
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
	grpczap.SetGrpcLoggerV2(grpcLog, log.NewNop().Zap())
}

func (app *App) setupDBs(ctx context.Context, lg log.Log) error {
	dbPath := app.Config.DataDir()
	if err := os.MkdirAll(dbPath, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create %s: %w", dbPath, err)
	}
	dbLog := app.addLogger(StateDbLogger, lg).Zap()
	schema, err := statemigrations.SchemaWithInCodeMigrations(*app.Config)
	if err != nil {
		return fmt.Errorf("error loading db schema: %w", err)
	}
	if len(app.Config.DatabaseSkipMigrations) > 0 {
		schema.SkipMigrations(app.Config.DatabaseSkipMigrations...)
	}
	dbopts := []sql.Opt{
		sql.WithLogger(dbLog),
		sql.WithDatabaseSchema(schema),
		sql.WithConnections(app.Config.DatabaseConnections),
		sql.WithLatencyMetering(app.Config.DatabaseLatencyMetering),
		sql.WithVacuumState(app.Config.DatabaseVacuumState),
		sql.WithAllowSchemaDrift(app.Config.DatabaseSchemaAllowDrift),
		sql.WithQueryCache(app.Config.DatabaseQueryCache),
		sql.WithQueryCacheSizes(map[sql.QueryCacheKind]int{
			atxs.CacheKindEpochATXs:           app.Config.DatabaseQueryCacheSizes.EpochATXs,
			atxs.CacheKindATXBlob:             app.Config.DatabaseQueryCacheSizes.ATXBlob,
			activesets.CacheKindActiveSetBlob: app.Config.DatabaseQueryCacheSizes.ActiveSetBlob,
		}),
		sql.WithConnIdleTimeout(app.Config.DatabaseConnIdleTimeout),
		sql.WithDBName("state"),
	}
	sqlDB, err := statesql.Open("file:"+filepath.Join(dbPath, dbFile), dbopts...)
	if err != nil {
		return fmt.Errorf("open sqlite db: %w", err)
	}
	app.db = sqlDB

	apiDBLog := app.addLogger(ApiStateDBLogger, lg).Zap()
	apiSqlDB, err := statesql.Open("file:"+filepath.Join(dbPath, dbFile),
		sql.WithReadOnly(),
		sql.WithLogger(apiDBLog),
		sql.WithConnections(app.Config.API.DatabaseConnections),
		sql.WithNoCheckSchemaDrift(), // already checked above
		sql.WithMigrationsDisabled(),
		sql.WithConnIdleTimeout(app.Config.DatabaseConnIdleTimeout),
		sql.WithDBName("state-api"),
	)
	if err != nil {
		return fmt.Errorf("open sqlite db: %w", err)
	}
	app.apiDB = apiSqlDB

	if app.Config.CollectMetrics && app.Config.DatabaseSizeMeteringInterval != 0 {
		app.dbMetrics = dbmetrics.NewDBMetricsCollector(
			ctx,
			app.db,
			dbLog,
			app.Config.DatabaseSizeMeteringInterval,
		)
	}
	{
		warmupLog := app.log.Zap().Named("warmup")
		app.log.Info("starting cache warmup")
		applied, err := layers.GetLastApplied(app.db)
		if err != nil {
			return err
		}
		start := time.Now()
		data, err := atxsdata.Warm(
			app.db,
			app.Config.Tortoise.WindowSizeEpochs(applied),
			warmupLog,
			app.signers...,
		)
		if err != nil {
			return err
		}
		app.atxsdata = data
		app.log.With().Info("cache warmup", log.Duration("duration", time.Since(start)))
	}
	app.cachedDB = datastore.NewCachedDB(sqlDB, app.addLogger(CachedDBLogger, lg).Zap(),
		datastore.WithConfig(app.Config.Cache),
		datastore.WithConsensusCache(app.atxsdata),
	)

	lSchema, err := localmigrations.SchemaWithInCodeMigrations()
	if err != nil {
		return fmt.Errorf("error loading db schema: %w", err)
	}
	localDB, err := localsql.Open("file:"+filepath.Join(dbPath, localDbFile),
		sql.WithLogger(dbLog),
		sql.WithDatabaseSchema(lSchema),
		sql.WithConnections(app.Config.DatabaseConnections),
		sql.WithAllowSchemaDrift(app.Config.DatabaseSchemaAllowDrift),
		sql.WithConnIdleTimeout(app.Config.DatabaseConnIdleTimeout),
		sql.WithDBName("local"),
	)
	if err != nil {
		return fmt.Errorf("open sqlite db: %w", err)
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

	logger.Info("initializing p2p services")

	cfg := app.Config.P2P
	cfg.DataDir = filepath.Join(app.Config.DataDir(), "p2p")
	p2plog := app.addLogger(P2PLogger, logger)
	if lvl, exist := app.loggers[P2PLogger]; exist {
		cfg.LogLevel = lvl.Level()
	} else {
		cfg.LogLevel = zapcore.InfoLevel
	}
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
	app.host, err = p2p.New(p2plog.Zap(), cfg, []byte(prologue), nc,
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

func (app *App) preserveAfterRecovery(ctx context.Context, preserved checkpoint.PreservedData) {
	for i, poetProof := range preserved.Proofs {
		ref, err := poetProof.Ref()
		if err != nil {
			app.log.With().Error("failed to calculated poet proof ref after checkpoint", log.Inline(poetProof))
			continue
		}

		if err := app.poetDb.ValidateAndStore(ctx, poetProof); err != nil {
			app.log.With().Error("failed to preserve poet proof after checkpoint",
				log.Stringer("atx id", preserved.Deps[i].ID),
				log.Stringer("poet proof ref", &ref),
				log.Err(err),
			)
			continue
		}
		app.log.With().Info("preserved poet proof after checkpoint",
			log.Stringer("atx id", preserved.Deps[i].ID),
			log.Stringer("poet proof ref", &ref),
		)
	}
	for _, atx := range preserved.Deps {
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
		lvl.SetLevel(zapcore.InfoLevel)
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

// proposalConsumerHare is used for the hare3->hare4 migration
// to satisfy the proposals handler dependency on hare.
type proposalConsumerHare struct {
	hare3          *hare3.Hare
	h3DisableLayer types.LayerID
	hare4          *hare4.Hare
}

func (p *proposalConsumerHare) IsKnown(layer types.LayerID, proposal types.ProposalID) bool {
	if layer < p.h3DisableLayer {
		return p.hare3.IsKnown(layer, proposal)
	}
	return p.hare4.IsKnown(layer, proposal)
}

func (p *proposalConsumerHare) OnProposal(proposal *types.Proposal) error {
	if proposal.Layer < p.h3DisableLayer {
		return p.hare3.OnProposal(proposal)
	}
	return p.hare4.OnProposal(proposal)
}
