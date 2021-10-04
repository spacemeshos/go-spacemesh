// Package node contains the main executable for go-spacemesh node
package node

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"math/big"
	"net/http"
	_ "net/http/pprof" // import for memory and network profiling
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"time"

	"github.com/spacemeshos/go-spacemesh/svm"
	"github.com/spacemeshos/go-spacemesh/svm/state"

	"github.com/mitchellh/mapstructure"
	"github.com/pyroscope-io/pyroscope/pkg/agent/profiler"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/blocks"
	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	cfg "github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/hare/eligibility"
	"github.com/spacemeshos/go-spacemesh/layerfetcher"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mempool"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/metrics"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/peers"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/pendingtxs"
	"github.com/spacemeshos/go-spacemesh/priorityq"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/syncer"
	"github.com/spacemeshos/go-spacemesh/timesync"
	timeCfg "github.com/spacemeshos/go-spacemesh/timesync/config"
	"github.com/spacemeshos/go-spacemesh/timesync/peersync"
	"github.com/spacemeshos/go-spacemesh/tortoise"
	"github.com/spacemeshos/go-spacemesh/tortoisebeacon"
	"github.com/spacemeshos/go-spacemesh/tortoisebeacon/weakcoin"
	"github.com/spacemeshos/go-spacemesh/turbohare"
)

const edKeyFileName = "key.bin"

// Logger names
const (
	AppLogger            = "app"
	P2PLogger            = "p2p"
	PostLogger           = "post"
	StateDbLogger        = "stateDbStore"
	StateLogger          = "state"
	AtxDbStoreLogger     = "atxDbStore"
	TBeaconDbStoreLogger = "tbDbStore"
	TBeaconLogger        = "tBeacon"
	WeakCoinLogger       = "weakCoin"
	PoetDbStoreLogger    = "poetDbStore"
	StoreLogger          = "store"
	PoetDbLogger         = "poetDb"
	MeshDBLogger         = "meshDb"
	TrtlLogger           = "trtl"
	AtxDbLogger          = "atxDb"
	BlkEligibilityLogger = "blkElgValidator"
	MeshLogger           = "mesh"
	SyncLogger           = "sync"
	BlockOracle          = "blockOracle"
	HareBeaconLogger     = "hareBeacon"
	HareOracleLogger     = "hareOracle"
	HareLogger           = "hare"
	BlockBuilderLogger   = "blockBuilder"
	BlockListenerLogger  = "blockListener"
	PoetListenerLogger   = "poetListener"
	NipostBuilderLogger  = "nipostBuilder"
	AtxBuilderLogger     = "atxBuilder"
	GossipListener       = "gossipListener"
	Fetcher              = "fetcher"
	LayerFetcher         = "layerFetcher"
	TimeSyncLogger       = "timesync"
	SVMLogger            = "SVM"
)

// Cmd is the cobra wrapper for the node, that allows adding parameters to it
var Cmd = &cobra.Command{
	Use:   "node",
	Short: "start node",
	Run: func(cmd *cobra.Command, args []string) {
		conf, err := LoadConfigFromFile()
		if err != nil {
			log.With().Error("can't load config file", log.Err(err))
			return
		}
		if err := cmdp.EnsureCLIFlags(cmd, conf); err != nil {
			log.With().Error("can't ensure that cli flags match config value types", log.Err(err))
			return
		}

		if conf.TestMode {
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
		starter := func() error {
			if err := app.Initialize(); err != nil {
				log.With().Error("Failed to initialize node.", log.Err(err))
				return err
			}
			// This blocks until the context is finished or until an error is produced
			err := app.Start()
			if err != nil {
				log.With().Error("Failed to start the node. See logs for details.", log.Err(err))
			}
			return err
		}
		err = starter()
		app.Cleanup()
		if err != nil {
			os.Exit(1)
		}
	},
}

// VersionCmd returns the current version of spacemesh
var VersionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version info",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Print(cmdp.Version)
		if cmdp.Commit != "" {
			fmt.Printf("+%s", cmdp.Commit)
		}
		fmt.Println()
	},
}

func init() {
	// TODO add commands actually adds flags
	cmdp.AddCommands(Cmd)
	Cmd.AddCommand(VersionCmd)
}

// Service is a general service interface that specifies the basic start/stop functionality
type Service interface {
	Start(ctx context.Context) error
	Close()
}

// HareService is basic definition of hare algorithm service, providing consensus results for a layer
type HareService interface {
	Service
	GetResult(types.LayerID) ([]types.BlockID, error)
}

// TortoiseBeaconService is an interface that defines tortoise beacon functionality.
type TortoiseBeaconService interface {
	Service
	GetBeacon(id types.EpochID) ([]byte, error)
	HandleSerializedProposalMessage(ctx context.Context, data service.GossipMessage, sync service.Fetcher)
	HandleSerializedFirstVotingMessage(ctx context.Context, data service.GossipMessage, sync service.Fetcher)
	HandleSerializedFollowingVotingMessage(ctx context.Context, data service.GossipMessage, sync service.Fetcher)
}

// TickProvider is an interface to a glopbal system clock that releases ticks on each layer
type TickProvider interface {
	Subscribe() timesync.LayerTimer
	Unsubscribe(timesync.LayerTimer)
	GetCurrentLayer() types.LayerID
	StartNotifying()
	GetGenesisTime() time.Time
	LayerToTime(types.LayerID) time.Time
	Close()
	AwaitLayer(types.LayerID) chan struct{}
}

// LoadConfigFromFile tries to load configuration file if the config parameter was specified
func LoadConfigFromFile() (*cfg.Config, error) {
	fileLocation := viper.GetString("config")
	vip := viper.New()
	// read in default config if passed as param using viper
	if err := cfg.LoadConfig(fileLocation, vip); err != nil {
		log.Error(fmt.Sprintf("couldn't load config file at location: %s switching to defaults \n error: %v.",
			fileLocation, err))
		// return err
	}

	conf := cfg.DefaultConfig()

	hook := mapstructure.ComposeDecodeHookFunc(
		mapstructure.StringToTimeDurationHookFunc(),
		mapstructure.StringToSliceHookFunc(","),
		bigRatDecodeFunc(),
	)

	// load config if it was loaded to our viper
	err := vip.Unmarshal(&conf, viper.DecodeHook(hook))
	if err != nil {
		log.With().Error("Failed to parse config", log.Err(err))
		return nil, err
	}
	return &conf, nil
}

func bigRatDecodeFunc() mapstructure.DecodeHookFunc {
	return func(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
		if t != reflect.TypeOf(&big.Rat{}) {
			return data, nil
		}

		switch f.Kind() {
		case reflect.String:
			v, ok := new(big.Rat).SetString(data.(string))
			if !ok {
				return nil, errors.New("malformed string representing big.Rat was provided")
			}

			return v, nil
		case reflect.Float64:
			return new(big.Rat).SetFloat64(data.(float64)), nil
		case reflect.Int64:
			return new(big.Rat).SetInt64(data.(int64)), nil
		case reflect.Uint64:
			return new(big.Rat).SetUint64(data.(uint64)), nil
		default:
			return data, nil
		}
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

// WithConfig overvwrites default App config.
func WithConfig(conf *cfg.Config) Option {
	return func(app *App) {
		app.Config = conf
	}
}

// New creates an instance of the spacemesh app
func New(opts ...Option) *App {
	defaultConfig := cfg.DefaultConfig()
	app := &App{
		Config:  &defaultConfig,
		log:     log.NewNop(),
		loggers: make(map[string]*zap.AtomicLevel),
		term:    make(chan struct{}),
		started: make(chan struct{}),
	}
	for _, opt := range opts {
		opt(app)
	}
	lvl := zap.NewAtomicLevelAt(zap.InfoLevel)
	log.SetupGlobal(app.log.SetLevel(&lvl))
	return app
}

// App is the cli app singleton
type App struct {
	*cobra.Command
	nodeID         types.NodeID
	P2P            p2p.Service
	Config         *cfg.Config
	grpcAPIService *grpcserver.Server
	jsonAPIService *grpcserver.JSONHTTPServer
	gatewaySvc     *grpcserver.GatewayService
	globalstateSvc *grpcserver.GlobalStateService
	txService      *grpcserver.TransactionService
	syncer         *syncer.Syncer
	blockListener  *blocks.BlockHandler
	state          *state.TransactionProcessor
	blockProducer  *miner.BlockBuilder
	oracle         *blocks.Oracle
	txProcessor    *state.TransactionProcessor
	mesh           *mesh.Mesh
	gossipListener *service.Listener
	clock          TickProvider
	hare           HareService
	postSetupMgr   *activation.PostSetupManager
	atxBuilder     *activation.Builder
	atxDb          *activation.DB
	poetListener   *activation.PoetListener
	edSgn          *signing.EdSigner
	watchPeers     *peers.Peers
	tortoiseBeacon TortoiseBeaconService
	closers        []interface{ Close() }
	log            log.Log
	txPool         *mempool.TxMempool
	svm            *svm.SVM
	layerFetch     *layerfetcher.Logic
	ptimesync      *peersync.Sync
	loggers        map[string]*zap.AtomicLevel
	term           chan struct{} // this channel is closed when closing services, goroutines should wait on this channel in order to terminate
	started        chan struct{} // this channel is closed once the app has finished starting
}

func (app *App) introduction() {
	log.Info("Welcome to Spacemesh. Spacemesh full node is starting...")
}

// Initialize sets up an exit signal, logging and checks the clock, returns error if clock is not in sync
func (app *App) Initialize() (err error) {
	// override default config in timesync since timesync is using TimeCongigValues
	timeCfg.TimeConfigValues = app.Config.TIME

	// ensure all data folders exist
	err = filesystem.ExistOrCreate(app.Config.DataDir())
	if err != nil {
		return err
	}

	// exit gracefully - e.g. with app Cleanup on sig abort (ctrl-c)
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)

	// Goroutine that listens for Ctrl ^ C command
	// and triggers the quit app
	go func() {
		for range signalChan {
			app.log.Info("Received an interrupt, stopping services...\n")
			cmdp.Cancel()
		}
	}()

	app.setupLogging()

	app.introduction()

	drift, err := timesync.CheckSystemClockDrift()
	if err != nil {
		return err
	}

	log.Info("System clock synchronized with ntp. drift: %s", drift)
	return nil
}

// setupLogging configured the app logging system.
func (app *App) setupLogging() {
	log.Info("%s", app.getAppInfo())

	msg := "initializing event reporter"
	if app.Config.PublishEventsURL != "" {
		msg += fmt.Sprintf(" with pubsub URL: %s", app.Config.PublishEventsURL)
	}
	log.Info(msg)
	if err := events.InitializeEventReporter(app.Config.PublishEventsURL); err != nil {
		log.With().Error("unable to initialize event reporter", log.Err(err))
	}
}

func (app *App) getAppInfo() string {
	return fmt.Sprintf("App version: %s. Git: %s - %s . Go Version: %s. OS: %s-%s ",
		cmdp.Version, cmdp.Branch, cmdp.Commit, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

// Cleanup stops all app services
func (app *App) Cleanup() {
	log.Info("app cleanup starting...")
	app.stopServices()
	// add any other Cleanup tasks here....
	log.Info("app cleanup completed")
}

// Wrap the top-level logger to add context info and set the level for a
// specific module.
func (app *App) addLogger(name string, logger log.Log) log.Log {
	lvl := zap.NewAtomicLevel()
	var err error

	switch name {
	case AppLogger:
		err = lvl.UnmarshalText([]byte(app.Config.LOGGING.AppLoggerLevel))
	case P2PLogger:
		err = lvl.UnmarshalText([]byte(app.Config.LOGGING.P2PLoggerLevel))
	case PostLogger:
		err = lvl.UnmarshalText([]byte(app.Config.LOGGING.PostLoggerLevel))
	case StateDbLogger:
		err = lvl.UnmarshalText([]byte(app.Config.LOGGING.StateDbLoggerLevel))
	case StateLogger:
		err = lvl.UnmarshalText([]byte(app.Config.LOGGING.StateLoggerLevel))
	case AtxDbStoreLogger:
		err = lvl.UnmarshalText([]byte(app.Config.LOGGING.AtxDbStoreLoggerLevel))
	case TBeaconLogger:
		err = lvl.UnmarshalText([]byte(app.Config.LOGGING.TBeaconLoggerLevel))
	case WeakCoinLogger:
		err = lvl.UnmarshalText([]byte(app.Config.LOGGING.WeakCoinLoggerLevel))
	case PoetDbStoreLogger:
		err = lvl.UnmarshalText([]byte(app.Config.LOGGING.PoetDbStoreLoggerLevel))
	case StoreLogger:
		err = lvl.UnmarshalText([]byte(app.Config.LOGGING.StoreLoggerLevel))
	case PoetDbLogger:
		err = lvl.UnmarshalText([]byte(app.Config.LOGGING.PoetDbLoggerLevel))
	case MeshDBLogger:
		err = lvl.UnmarshalText([]byte(app.Config.LOGGING.MeshDBLoggerLevel))
	case TrtlLogger:
		err = lvl.UnmarshalText([]byte(app.Config.LOGGING.TrtlLoggerLevel))
	case AtxDbLogger:
		err = lvl.UnmarshalText([]byte(app.Config.LOGGING.AtxDbLoggerLevel))
	case BlkEligibilityLogger:
		err = lvl.UnmarshalText([]byte(app.Config.LOGGING.BlkEligibilityLoggerLevel))
	case MeshLogger:
		err = lvl.UnmarshalText([]byte(app.Config.LOGGING.MeshLoggerLevel))
	case SyncLogger:
		err = lvl.UnmarshalText([]byte(app.Config.LOGGING.SyncLoggerLevel))
	case BlockOracle:
		err = lvl.UnmarshalText([]byte(app.Config.LOGGING.BlockOracleLevel))
	case HareOracleLogger:
		err = lvl.UnmarshalText([]byte(app.Config.LOGGING.HareOracleLoggerLevel))
	case HareBeaconLogger:
		err = lvl.UnmarshalText([]byte(app.Config.LOGGING.HareBeaconLoggerLevel))
	case HareLogger:
		err = lvl.UnmarshalText([]byte(app.Config.LOGGING.HareLoggerLevel))
	case BlockBuilderLogger:
		err = lvl.UnmarshalText([]byte(app.Config.LOGGING.BlockBuilderLoggerLevel))
	case BlockListenerLogger:
		err = lvl.UnmarshalText([]byte(app.Config.LOGGING.BlockListenerLoggerLevel))
	case PoetListenerLogger:
		err = lvl.UnmarshalText([]byte(app.Config.LOGGING.PoetListenerLoggerLevel))
	case NipostBuilderLogger:
		err = lvl.UnmarshalText([]byte(app.Config.LOGGING.NipostBuilderLoggerLevel))
	case AtxBuilderLogger:
		err = lvl.UnmarshalText([]byte(app.Config.LOGGING.AtxBuilderLoggerLevel))
	case TimeSyncLogger:
		err = lvl.UnmarshalText([]byte(app.Config.LOGGING.TimeSyncLoggerLevel))
	default:
		lvl.SetLevel(log.Level())
	}

	if err != nil {
		app.log.Error("cannot parse logging for %v error %v", name, err)
		lvl.SetLevel(log.Level())
	}

	if logger.Check(lvl.Level()) {
		app.loggers[name] = &lvl
		logger = logger.SetLevel(&lvl)
	}
	return logger.WithName(name).WithFields(log.String("module", name))
}

// SetLogLevel updates the log level of an existing logger
func (app *App) SetLogLevel(name, loglevel string) error {
	if lvl, ok := app.loggers[name]; ok {
		err := lvl.UnmarshalText([]byte(loglevel))
		if err != nil {
			return err
		}
	} else {
		return fmt.Errorf("cannot find logger %v", name)
	}
	return nil
}

func (app *App) initServices(ctx context.Context,
	nodeID types.NodeID,
	swarm service.Service,
	dbStorepath string,
	sgn *signing.EdSigner,
	isFixedOracle bool,
	rolacle hare.Rolacle,
	layerSize uint32,
	poetClient activation.PoetProvingServiceClient,
	vrfSigner signing.Signer,
	layersPerEpoch uint32, clock TickProvider) error {

	app.nodeID = nodeID

	lg := app.log.Named(nodeID.ShortString()).WithFields(nodeID)
	types.SetLayersPerEpoch(app.Config.LayersPerEpoch)

	app.log = app.addLogger(AppLogger, lg)

	db, err := database.NewLDBDatabase(filepath.Join(dbStorepath, "state"), 0, 0, app.addLogger(StateDbLogger, lg))
	if err != nil {
		return err
	}
	app.closers = append(app.closers, db)

	atxdbstore, err := database.NewLDBDatabase(filepath.Join(dbStorepath, "atx"), 0, 0, app.addLogger(AtxDbStoreLogger, lg))
	if err != nil {
		return err
	}
	app.closers = append(app.closers, atxdbstore)

	tBeaconDBStore, err := database.NewLDBDatabase(filepath.Join(dbStorepath, "tbeacon"), 0, 0, app.addLogger(TBeaconDbStoreLogger, lg))
	if err != nil {
		return err
	}
	app.closers = append(app.closers, tBeaconDBStore)

	poetDbStore, err := database.NewLDBDatabase(filepath.Join(dbStorepath, "poet"), 0, 0, app.addLogger(PoetDbStoreLogger, lg))
	if err != nil {
		return err
	}
	app.closers = append(app.closers, poetDbStore)

	iddbstore, err := database.NewLDBDatabase(filepath.Join(dbStorepath, "ids"), 0, 0, app.addLogger(StateDbLogger, lg))
	if err != nil {
		return err
	}
	app.closers = append(app.closers, iddbstore)

	store, err := database.NewLDBDatabase(filepath.Join(dbStorepath, "store"), 0, 0, app.addLogger(StoreLogger, lg))
	if err != nil {
		return err
	}
	app.closers = append(app.closers, store)

	idStore := activation.NewIdentityStore(iddbstore)
	poetDb := activation.NewPoetDb(poetDbStore, app.addLogger(PoetDbLogger, lg))
	validator := activation.NewValidator(poetDb, app.Config.POST)
	mdb, err := mesh.NewPersistentMeshDB(filepath.Join(dbStorepath, "mesh"), app.Config.BlockCacheSize, app.addLogger(MeshDBLogger, lg))
	if err != nil {
		return err
	}

	app.txPool = mempool.NewTxMemPool()
	meshAndPoolProjector := pendingtxs.NewMeshAndPoolProjector(mdb, app.txPool)

	appliedTxs, err := database.NewLDBDatabase(filepath.Join(dbStorepath, "appliedTxs"), 0, 0, lg.WithName("appliedTxs"))
	if err != nil {
		return err
	}
	app.closers = append(app.closers, appliedTxs)
	processor := state.NewTransactionProcessor(db, appliedTxs, meshAndPoolProjector, app.txPool, lg.WithName("state"))

	goldenATXID := types.ATXID(types.HexToHash32(app.Config.GoldenATXID))
	if goldenATXID == *types.EmptyATXID {
		return errors.New("invalid golden atx id")
	}

	atxDB := activation.NewDB(atxdbstore, idStore, mdb, layersPerEpoch, goldenATXID, validator, app.addLogger(AtxDbLogger, lg))

	edVerifier := signing.NewEDVerifier()
	vrfVerifier := signing.VRFVerifier{}

	wc := weakcoin.New(swarm,
		vrfSigner, vrfVerifier,
		weakcoin.WithLog(app.addLogger(WeakCoinLogger, lg)),
		weakcoin.WithMaxRound(app.Config.TortoiseBeacon.RoundsNumber),
	)

	tBeacon := tortoisebeacon.New(
		app.Config.TortoiseBeacon,
		nodeID,
		swarm,
		atxDB,
		sgn,
		edVerifier,
		vrfSigner,
		vrfVerifier,
		wc,
		tBeaconDBStore,
		clock,
		app.addLogger(TBeaconLogger, lg))

	var msh *mesh.Mesh
	var trtl *tortoise.ThreadSafeVerifyingTortoise
	trtlStateDB, err := database.NewLDBDatabase(filepath.Join(dbStorepath, "turtle"), 0, 0, app.addLogger(StateDbLogger, lg))
	if err != nil {
		return err
	}
	app.closers = append(app.closers, trtlStateDB)
	trtlCfg := tortoise.Config{
		LayerSize:       layerSize,
		Database:        trtlStateDB,
		MeshDatabase:    mdb,
		ATXDB:           atxDB,
		Clock:           clock,
		Hdist:           app.Config.Hdist,
		Zdist:           app.Config.Zdist,
		ConfidenceParam: app.Config.ConfidenceParam,
		WindowSize:      app.Config.WindowSize,
		GlobalThreshold: app.Config.GlobalThreshold,
		LocalThreshold:  app.Config.LocalThreshold,
		Log:             app.addLogger(TrtlLogger, lg),
		RerunInterval:   time.Minute * time.Duration(app.Config.TortoiseRerunInterval),
	}

	trtl = tortoise.NewVerifyingTortoise(ctx, trtlCfg)
	svm := svm.New(processor, app.addLogger(SVMLogger, lg))

	if mdb.PersistentData() {
		msh = mesh.NewRecoveredMesh(ctx, mdb, atxDB, app.Config.REWARD, trtl, app.txPool, processor, app.addLogger(MeshLogger, lg))
		go msh.CacheWarmUp(app.Config.LayerAvgSize)
	} else {
		msh = mesh.NewMesh(mdb, atxDB, app.Config.REWARD, trtl, app.txPool, processor, app.addLogger(MeshLogger, lg))
		if err := svm.SetupGenesis(app.Config.Genesis); err != nil {
			return err
		}
	}

	eValidator := blocks.NewBlockEligibilityValidator(layerSize, layersPerEpoch, atxDB, tBeacon,
		signing.VRFVerify, msh, app.addLogger(BlkEligibilityLogger, lg))

	if app.Config.AtxsPerBlock > miner.AtxsPerBlockLimit { // validate limit
		return fmt.Errorf("number of atxs per block required is bigger than the limit. atxs_per_block: %d. limit: %d",
			app.Config.AtxsPerBlock, miner.AtxsPerBlockLimit,
		)
	}

	// we can't have an epoch offset which is greater/equal than the number of layers in an epoch

	if app.Config.HareEligibility.EpochOffset >= app.Config.BaseConfig.LayersPerEpoch {
		return fmt.Errorf("epoch offset cannot be greater than or equal to the number of layers per epoch. epoch_offset: %d. layers_per_epoch: %d",
			app.Config.HareEligibility.EpochOffset, app.Config.BaseConfig.LayersPerEpoch)
	}

	bCfg := blocks.Config{
		Depth:       app.Config.Hdist,
		GoldenATXID: goldenATXID,
	}
	blockListener := blocks.NewBlockHandler(bCfg, msh, eValidator, app.addLogger(BlockListenerLogger, lg))

	remoteFetchService := fetch.NewFetch(ctx, app.Config.FETCH, swarm, app.addLogger(Fetcher, lg))

	layerFetch := layerfetcher.NewLogic(ctx, app.Config.LAYERS, blockListener, atxDB, poetDb, atxDB, processor, swarm, remoteFetchService, msh, app.addLogger(LayerFetcher, lg))
	layerFetch.AddDBs(mdb.Blocks(), atxdbstore, mdb.Transactions(), poetDbStore)

	syncerConf := syncer.Configuration{
		SyncInterval: time.Duration(app.Config.SyncInterval) * time.Second,
		AlwaysListen: app.Config.AlwaysListen,
	}
	newSyncer := syncer.NewSyncer(ctx, syncerConf, clock, msh, layerFetch, app.addLogger(SyncLogger, lg))
	// TODO(dshulyak) this needs to be improved, but dependency graph is a bit complicated
	tBeacon.SetSyncState(newSyncer)
	blockOracle := blocks.NewMinerBlockOracle(layerSize, layersPerEpoch, atxDB, tBeacon, vrfSigner, nodeID, newSyncer.ListenToGossip, app.addLogger(BlockOracle, lg))

	// TODO: we should probably decouple the apptest and the node (and duplicate as necessary) (#1926)
	var hOracle hare.Rolacle
	if isFixedOracle {
		// fixed rolacle, take the provided rolacle
		hOracle = rolacle
	} else {
		// regular oracle, build and use it
		beacon := eligibility.NewBeacon(tBeacon, app.addLogger(HareBeaconLogger, lg))
		hOracle = eligibility.New(beacon, atxDB, mdb, signing.VRFVerify, vrfSigner, app.Config.LayersPerEpoch, app.Config.HareEligibility, app.addLogger(HareOracleLogger, lg))
		// TODO: genesisMinerWeight is set to app.Config.SpaceToCommit, because PoET ticks are currently hardcoded to 1
	}

	gossipListener := service.NewListener(swarm, layerFetch, newSyncer.ListenToGossip, app.addLogger(GossipListener, lg))
	rabbit := app.HareFactory(ctx, mdb, swarm, sgn, nodeID, newSyncer, msh, hOracle, idStore, clock, lg)

	stateAndMeshProjector := pendingtxs.NewStateAndMeshProjector(processor, msh)
	minerCfg := miner.Config{
		DBPath:         filepath.Join(dbStorepath, "builder"),
		Hdist:          app.Config.Hdist,
		MinerID:        nodeID,
		AtxsPerBlock:   app.Config.AtxsPerBlock,
		LayersPerEpoch: layersPerEpoch,
		TxsPerBlock:    app.Config.TxsPerBlock,
	}

	blockProducer := miner.NewBlockBuilder(
		minerCfg,
		sgn,
		swarm,
		clock.Subscribe(),
		msh,
		trtl,
		blockOracle,
		tBeacon,
		newSyncer,
		stateAndMeshProjector,
		app.txPool,
		app.addLogger(BlockBuilderLogger, lg))

	poetListener := activation.NewPoetListener(swarm, poetDb, app.addLogger(PoetListenerLogger, lg))

	postSetupMgr, err := activation.NewPostSetupManager(util.Hex2Bytes(nodeID.Key), app.Config.POST, app.addLogger(PostLogger, lg))
	if err != nil {
		app.log.Panic("failed to create post setup manager: %v", err)
	}

	nipostBuilder := activation.NewNIPostBuilder(util.Hex2Bytes(nodeID.Key), postSetupMgr, poetClient, poetDb, store, app.addLogger(NipostBuilderLogger, lg))

	coinbaseAddr := types.HexToAddress(app.Config.SMESHING.CoinbaseAccount)
	if app.Config.SMESHING.Start {
		if coinbaseAddr.Big().Uint64() == 0 {
			app.log.Panic("invalid coinbase account")
		}
	}

	builderConfig := activation.Config{
		CoinbaseAccount: coinbaseAddr,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}

	atxBuilder := activation.NewBuilder(builderConfig, nodeID, sgn,
		atxDB, swarm, msh, layersPerEpoch, nipostBuilder,
		postSetupMgr, clock, newSyncer, store, app.addLogger("atxBuilder", lg),
		activation.WithContext(ctx),
	)

	gossipListener.AddListener(ctx, state.IncomingTxProtocol, priorityq.Low, processor.HandleTxGossipData)
	gossipListener.AddListener(ctx, activation.AtxProtocol, priorityq.Low, atxDB.HandleGossipAtx)
	gossipListener.AddListener(ctx, blocks.NewBlockProtocol, priorityq.High, blockListener.HandleBlock)
	gossipListener.AddListener(ctx, tortoisebeacon.TBProposalProtocol, priorityq.Low, tBeacon.HandleSerializedProposalMessage)
	gossipListener.AddListener(ctx, tortoisebeacon.TBFirstVotingProtocol, priorityq.Low, tBeacon.HandleSerializedFirstVotingMessage)
	gossipListener.AddListener(ctx, tortoisebeacon.TBFollowingVotingProtocol, priorityq.Low, tBeacon.HandleSerializedFollowingVotingMessage)
	gossipListener.AddListener(ctx, weakcoin.GossipProtocol, priorityq.Low, wc.HandleSerializedMessage)

	app.blockProducer = blockProducer
	app.blockListener = blockListener
	app.gossipListener = gossipListener
	app.mesh = msh
	app.syncer = newSyncer
	app.clock = clock
	app.state = processor
	app.hare = rabbit
	app.P2P = swarm
	app.watchPeers = peers.Start(swarm)
	app.poetListener = poetListener
	app.atxBuilder = atxBuilder
	app.postSetupMgr = postSetupMgr
	app.oracle = blockOracle
	app.txProcessor = processor
	app.atxDb = atxDB
	app.layerFetch = layerFetch
	app.tortoiseBeacon = tBeacon
	app.svm = svm
	if !app.Config.TIME.Peersync.Disable {
		conf := app.Config.TIME.Peersync
		conf.ResponsesBufferSize = app.Config.P2P.BufferSize
		app.ptimesync = peersync.New(
			swarm.(peersync.Network),
			peersync.WithLog(app.addLogger(TimeSyncLogger, lg)),
			peersync.WithConfig(conf),
		)
	}

	return nil
}

// periodically checks that our clock is sync
func (app *App) checkTimeDrifts() {
	checkTimeSync := time.NewTicker(app.Config.TIME.RefreshNtpInterval)
	defer checkTimeSync.Stop() // close ticker

	for {
		select {
		case <-app.term:
			return

		case <-checkTimeSync.C:
			_, err := timesync.CheckSystemClockDrift()
			if err != nil {
				app.log.With().Error("unable to synchronize system time", log.Err(err))
				cmdp.Cancel()
				return
			}
		}
	}
}

// HareFactory returns a hare consensus algorithm according to the parameters in app.Config.Hare.SuperHare
func (app *App) HareFactory(
	ctx context.Context,
	mdb *mesh.DB,
	swarm service.Service,
	sgn hare.Signer,
	nodeID types.NodeID,
	syncer *syncer.Syncer,
	msh *mesh.Mesh,
	hOracle hare.Rolacle,
	idStore *activation.IdentityStore,
	clock TickProvider,
	lg log.Log,
) HareService {
	if app.Config.HARE.SuperHare {
		hr := turbohare.New(ctx, app.Config.HARE, msh, clock.Subscribe(), app.addLogger(HareLogger, lg))
		mdb.InputVectorBackupFunc = hr.GetResult
		return hr
	}

	ha := hare.New(
		app.Config.HARE,
		swarm,
		sgn,
		nodeID,
		syncer.IsSynced,
		msh,
		hOracle,
		uint16(app.Config.LayersPerEpoch),
		idStore,
		hOracle,
		clock.Subscribe(),
		app.addLogger(HareLogger, lg))
	return ha
}

func (app *App) startServices(ctx context.Context) error {
	app.layerFetch.Start()
	go app.startSyncer(ctx)

	if err := app.tortoiseBeacon.Start(ctx); err != nil {
		return fmt.Errorf("cannot start tortoise beacon: %w", err)
	}
	if err := app.hare.Start(ctx); err != nil {
		return fmt.Errorf("cannot start hare: %w", err)
	}
	if err := app.blockProducer.Start(ctx); err != nil {
		return fmt.Errorf("cannot start block producer: %w", err)
	}

	app.poetListener.Start(ctx)

	if app.Config.SMESHING.Start {
		coinbaseAddr := types.HexToAddress(app.Config.SMESHING.CoinbaseAccount)
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
	go app.checkTimeDrifts()
	return nil
}

func (app *App) startAPIServices(ctx context.Context, net api.NetworkAPI) {
	apiConf := &app.Config.API
	layerDuration := app.Config.LayerDurationSec

	// API SERVICES
	// Since we have multiple GRPC services, we cannot automatically enable them if
	// the gateway server is enabled (since we don't know which ones to enable), so
	// it's an error if the gateway server is enabled without enabling at least one
	// GRPC service.

	// Make sure we only create the server once.
	registerService := func(svc grpcserver.ServiceAPI) {
		if app.grpcAPIService == nil {
			app.grpcAPIService = grpcserver.NewServerWithInterface(apiConf.GrpcServerPort, apiConf.GrpcServerInterface)
		}
		svc.RegisterService(app.grpcAPIService)
	}

	// Register the requested services one by one
	if apiConf.StartDebugService {
		registerService(grpcserver.NewDebugService(app.mesh))
	}
	if apiConf.StartGatewayService {
		registerService(grpcserver.NewGatewayService(net))
	}
	if apiConf.StartGlobalStateService {
		registerService(grpcserver.NewGlobalStateService(app.mesh, app.txPool))
	}
	if apiConf.StartMeshService {
		registerService(grpcserver.NewMeshService(app.mesh, app.clock, app.Config.LayersPerEpoch, app.Config.P2P.NetworkID, layerDuration, app.Config.LayerAvgSize, app.Config.TxsPerBlock))
	}
	if apiConf.StartNodeService {
		nodeService := grpcserver.NewNodeService(net, app.mesh, app.clock, app.syncer, app.atxBuilder)
		registerService(nodeService)
		app.closers = append(app.closers, nodeService)
	}
	if apiConf.StartSmesherService {
		registerService(grpcserver.NewSmesherService(app.postSetupMgr, app.atxBuilder))
	}
	if apiConf.StartTransactionService {
		registerService(grpcserver.NewTransactionService(net, app.mesh, app.txPool, app.syncer))
	}

	// Now that the services are registered, start the server.
	if app.grpcAPIService != nil {
		app.grpcAPIService.Start()
	}

	if apiConf.StartJSONServer {
		if app.grpcAPIService == nil {
			// This panics because it should not happen.
			// It should be caught inside apiConf.
			log.Panic("one or more new grpc services must be enabled with new json gateway server")
		}
		app.jsonAPIService = grpcserver.NewJSONHTTPServer(apiConf.JSONServerPort, apiConf.GrpcServerPort)
		app.jsonAPIService.StartService(
			ctx,
			apiConf.StartDebugService,
			apiConf.StartGatewayService,
			apiConf.StartGlobalStateService,
			apiConf.StartMeshService,
			apiConf.StartNodeService,
			apiConf.StartSmesherService,
			apiConf.StartTransactionService,
		)
	}
}

func (app *App) stopServices() {
	// all go-routines that listen to app.term will close
	// note: there is no guarantee that a listening go-routine will close before stopServices exits
	close(app.term)

	if app.jsonAPIService != nil {
		log.Info("stopping json gateway service")
		if err := app.jsonAPIService.Close(); err != nil {
			log.With().Error("error stopping json gateway server", log.Err(err))
		}
	}

	if app.grpcAPIService != nil {
		log.Info("stopping grpc service")
		// does not return any errors
		_ = app.grpcAPIService.Close()
	}

	if app.blockProducer != nil {
		app.log.Info("closing block producer")
		if err := app.blockProducer.Close(); err != nil {
			log.With().Error("cannot stop block producer", log.Err(err))
		}
	}

	if app.clock != nil {
		app.log.Info("closing clock")
		app.clock.Close()
	}

	if app.gossipListener != nil {
		app.gossipListener.Stop()
	}

	if app.tortoiseBeacon != nil {
		app.log.Info("stopping tortoise beacon")
		app.tortoiseBeacon.Close()
	}

	if app.poetListener != nil {
		app.log.Info("closing poet listener")
		app.poetListener.Close()
	}

	if app.atxBuilder != nil {
		app.log.Info("closing atx builder")
		_ = app.atxBuilder.StopSmeshing(false)
	}

	if app.hare != nil {
		app.log.Info("closing hare")
		app.hare.Close()
	}

	if app.P2P != nil {
		app.log.Info("closing p2p")
		app.P2P.Shutdown()
	}

	if app.layerFetch != nil {
		app.log.Info("closing layerFetch")
		app.layerFetch.Close()
	}

	if app.syncer != nil {
		app.log.Info("closing sync")
		app.syncer.Close()
	}

	if app.mesh != nil {
		app.log.Info("closing mesh")
		app.mesh.Close()
	}

	if app.ptimesync != nil {
		app.ptimesync.Stop()
		app.log.Debug("peer timesync stopped")
	}

	if app.watchPeers != nil {
		app.watchPeers.Close()
	}

	events.CloseEventReporter()
	events.CloseEventPubSub()

	// Close all databases.
	for _, closer := range app.closers {
		if closer != nil {
			closer.Close()
		}
	}
}

// LoadOrCreateEdSigner either loads a previously created ed identity for the node or creates a new one if not exists
func (app *App) LoadOrCreateEdSigner() (*signing.EdSigner, error) {
	filename := filepath.Join(app.Config.SMESHING.Opts.DataDir, edKeyFileName)
	log.Info("Looking for identity file at `%v`", filename)

	data, err := ioutil.ReadFile(filename)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to read identity file: %w", err)
		}

		log.Info("Identity file not found. Creating new identity...")

		edSgn := signing.NewEdSigner()
		err := os.MkdirAll(filepath.Dir(filename), filesystem.OwnerReadWriteExec)
		if err != nil {
			return nil, fmt.Errorf("failed to create directory for identity file: %w", err)
		}
		err = ioutil.WriteFile(filename, edSgn.ToBuffer(), filesystem.OwnerReadWrite)
		if err != nil {
			return nil, fmt.Errorf("failed to write identity file: %w", err)
		}

		log.With().Info("created new identity", edSgn.PublicKey())
		return edSgn, nil
	}

	edSgn, err := signing.NewEdSignerFromBuffer(data)
	if err != nil {
		return nil, fmt.Errorf("failed to construct identity from data file: %w", err)
	}

	log.Info("Loaded existing identity; public key: %v", edSgn.PublicKey())

	return edSgn, nil
}

type identityFileFound struct{}

func (identityFileFound) Error() string {
	return "identity file found"
}

func (app *App) getIdentityFile() (string, error) {
	var f string
	err := filepath.Walk(app.Config.SMESHING.Opts.DataDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && info.Name() == edKeyFileName {
			f = path
			return &identityFileFound{}
		}
		return nil
	})
	if _, ok := err.(*identityFileFound); ok {
		return f, nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to traverse Post data dir: %w", err)
	}
	return "", fmt.Errorf("not found")
}

func (app *App) startSyncer(ctx context.Context) {
	if app.P2P == nil {
		app.log.Error("syncer started before p2p is initialized")
	} else {
		_, err := app.watchPeers.WaitPeers(ctx, app.Config.P2P.SwarmConfig.RandomConnections)
		if err != nil {
			return
		}
	}
	app.syncer.Start(ctx)
}

// Start starts the Spacemesh node and initializes all relevant services according to command line arguments provided.
func (app *App) Start() error {
	// we use the main app context
	ctx := cmdp.Ctx
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

	err = filesystem.ExistOrCreate(app.Config.DataDir())
	if err != nil {
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

	poetClient := activation.NewHTTPPoetClient(app.Config.PoETServer)

	edPubkey := app.edSgn.PublicKey()
	vrfSigner, vrfPub, err := signing.NewVRFSigner(app.edSgn.Sign(edPubkey.Bytes()))
	if err != nil {
		return fmt.Errorf("failed to create vrf signer: %w", err)
	}

	nodeID := types.NodeID{Key: edPubkey.String(), VRFPublicKey: vrfPub}

	lg := logger.Named(nodeID.ShortString()).WithFields(nodeID)

	/* Initialize all protocol services */

	dbStorepath := app.Config.DataDir()
	gTime, err := time.Parse(time.RFC3339, app.Config.GenesisTime)
	if err != nil {
		return fmt.Errorf("cannot parse genesis time %s: %d", app.Config.GenesisTime, err)
	}
	ld := time.Duration(app.Config.LayerDurationSec) * time.Second
	clock := timesync.NewClock(timesync.RealClock{}, ld, gTime, lg.WithName("clock"))

	logger.Info("initializing p2p services")
	swarm, err := p2p.New(ctx, app.Config.P2P, app.addLogger(P2PLogger, lg), dbStorepath)
	if err != nil {
		return fmt.Errorf("error starting p2p services: %w", err)
	}

	if err = app.initServices(ctx,
		nodeID,
		swarm,
		dbStorepath,
		app.edSgn,
		false,
		nil,
		uint32(app.Config.LayerAvgSize),
		poetClient,
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
			swarm.LocalNode().PublicKey().String(), strconv.Itoa(int(app.Config.P2P.NetworkID)))
	}

	if err := app.startServices(ctx); err != nil {
		return fmt.Errorf("error starting services: %w", err)
	}

	app.startAPIServices(ctx, app.P2P)

	// P2P must start last to not block when sending messages to protocols
	if err := app.P2P.Start(ctx); err != nil {
		return fmt.Errorf("error starting p2p services: %w", err)
	}

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
			// if nil node was already stopped
			if syncErr != nil {
				cmdp.Cancel()
			}
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
