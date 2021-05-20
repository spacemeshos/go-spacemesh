// Package node contains the main executable for go-spacemesh node
package node

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/ioutil"
	"net/http"
	_ "net/http/pprof" // import for memory and network profiling
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/layerfetcher"

	"github.com/pyroscope-io/pyroscope/pkg/agent/profiler"
	"github.com/spacemeshos/post/shared"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api"
	apiCfg "github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/blocks"
	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	cfg "github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/hare/eligibility"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/metrics"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/pendingtxs"
	"github.com/spacemeshos/go-spacemesh/priorityq"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/state"
	"github.com/spacemeshos/go-spacemesh/sync"
	"github.com/spacemeshos/go-spacemesh/timesync"
	timeCfg "github.com/spacemeshos/go-spacemesh/timesync/config"
	"github.com/spacemeshos/go-spacemesh/tortoise"
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
	BlockHandlerLogger   = "blockHandler"
	PoetListenerLogger   = "poetListener"
	NipstBuilderLogger   = "nipstBuilder"
	AtxBuilderLogger     = "atxBuilder"
	GossipListener       = "gossipListener"
	Fetcher              = "fetcher"
)

// Cmd is the cobra wrapper for the node, that allows adding parameters to it
var Cmd = &cobra.Command{
	Use:   "node",
	Short: "start node",
	Run: func(cmd *cobra.Command, args []string) {
		app := NewSpacemeshApp()
		defer app.Cleanup(cmd, args)

		err := app.Initialize(cmd, args)
		if err != nil {
			log.With().Error("Failed to initialize node.", log.Err(err))
			return
		}
		// This blocks until the context is finished
		app.Start(cmd, args)
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
	GetResult(id types.LayerID) ([]types.BlockID, error)
}

// TickProvider is an interface to a glopbal system clock that releases ticks on each layer
type TickProvider interface {
	Subscribe() timesync.LayerTimer
	Unsubscribe(timer timesync.LayerTimer)
	GetCurrentLayer() types.LayerID
	StartNotifying()
	GetGenesisTime() time.Time
	LayerToTime(id types.LayerID) time.Time
	Close()
	AwaitLayer(layerID types.LayerID) chan struct{}
}

// Tortoise beacon mock (waiting for #2267)
type tortoiseBeaconMock struct{}

// GetBeacon returns a very simple pseudo-random beacon value based on the input epoch ID
func (tortoiseBeaconMock) GetBeacon(epochID types.EpochID) []byte {
	sha := sha256.Sum256(epochID.ToBytes())
	return sha[:4]
}

// SpacemeshApp is the cli app singleton
type SpacemeshApp struct {
	*cobra.Command
	nodeID         types.NodeID
	P2P            p2p.Service
	Config         *cfg.Config
	grpcAPIService *grpcserver.Server
	jsonAPIService *grpcserver.JSONHTTPServer
	gatewaySvc     *grpcserver.GatewayService
	globalstateSvc *grpcserver.GlobalStateService
	txService      *grpcserver.TransactionService
	syncer         *sync.Syncer
	blockListener  *blocks.BlockHandler
	state          *state.TransactionProcessor
	blockProducer  *miner.BlockBuilder
	oracle         *blocks.Oracle
	txProcessor    *state.TransactionProcessor
	mesh           *mesh.Mesh
	gossipListener *service.Listener
	clock          TickProvider
	hare           HareService
	atxBuilder     *activation.Builder
	atxDb          *activation.DB
	poetListener   *activation.PoetListener
	edSgn          *signing.EdSigner
	closers        []interface{ Close() }
	log            log.Log
	txPool         *state.TxMempool
	layerFetch     *layerfetcher.Logic
	loggers        map[string]*zap.AtomicLevel
	term           chan struct{} // this channel is closed when closing services, goroutines should wait on this channel in order to terminate
	started        chan struct{} // this channel is closed once the app has finished starting
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
	// load config if it was loaded to our viper
	err := vip.Unmarshal(&conf)
	if err != nil {
		log.Error("Failed to parse config\n")
		return nil, err
	}
	return &conf, nil
}

// ParseConfig unmarshal config file into struct
func (app *SpacemeshApp) ParseConfig() error {
	conf, err := LoadConfigFromFile()
	app.Config = conf

	return err
}

// NewSpacemeshApp creates an instance of the spacemesh app
func NewSpacemeshApp() *SpacemeshApp {
	defaultConfig := cfg.DefaultConfig()
	node := &SpacemeshApp{
		Config:  &defaultConfig,
		loggers: make(map[string]*zap.AtomicLevel),
		term:    make(chan struct{}),
		started: make(chan struct{}),
	}

	return node
}

func (app *SpacemeshApp) introduction() {
	log.Info("Welcome to Spacemesh. Spacemesh full node is starting...")
}

// Initialize does pre processing of flags and configuration files, it also initializes data dirs if they dont exist
func (app *SpacemeshApp) Initialize(cmd *cobra.Command, args []string) (err error) {
	// exit gracefully - e.g. with app Cleanup on sig abort (ctrl-c)
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)

	// Goroutine that listens for Ctrl ^ C command
	// and triggers the quit app
	go func() {
		for range signalChan {
			log.Info("Received an interrupt, stopping services...\n")
			cmdp.Cancel()
		}
	}()

	// parse the config file based on flags et al
	if err := app.ParseConfig(); err != nil {
		log.Error(fmt.Sprintf("couldn't parse the config err=%v", err))
	}

	// ensure cli flags are higher priority than config file
	if err := cmdp.EnsureCLIFlags(cmd, app.Config); err != nil {
		return err
	}
	// override default config in timesync since timesync is using TimeCongigValues
	timeCfg.TimeConfigValues = app.Config.TIME

	// ensure all data folders exist
	err = filesystem.ExistOrCreate(app.Config.DataDir())
	if err != nil {
		return err
	}

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
func (app *SpacemeshApp) setupLogging() {
	if app.Config.TestMode {
		log.JSONLog(true)
	}

	// app-level logging
	log.InitSpacemeshLoggingSystemWithHooks(func(entry zapcore.Entry) error {
		// If we report anything less than this we'll end up in an infinite loop
		if entry.Level >= zapcore.ErrorLevel {
			events.ReportError(events.NodeError{
				Msg:   entry.Message,
				Trace: string(debug.Stack()),
				Level: entry.Level,
			})
		}
		return nil
	})

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

func (app *SpacemeshApp) getAppInfo() string {
	return fmt.Sprintf("App version: %s. Git: %s - %s . Go Version: %s. OS: %s-%s ",
		cmdp.Version, cmdp.Branch, cmdp.Commit, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

// Cleanup stops all app services
func (app *SpacemeshApp) Cleanup(*cobra.Command, []string) {
	log.Info("app cleanup starting...")
	app.stopServices()
	// add any other Cleanup tasks here....
	log.Info("app cleanup completed\n\n")
}

func (app *SpacemeshApp) setupGenesis(state *state.TransactionProcessor, msh *mesh.Mesh) {
	var conf *apiCfg.GenesisConfig
	if app.Config.GenesisConfPath != "" {
		var err error
		conf, err = apiCfg.LoadGenesisConfig(app.Config.GenesisConfPath)
		if err != nil {
			app.log.Error("cannot load genesis config from file")
		}
	} else {
		conf = apiCfg.DefaultGenesisConfig()
	}
	for id, acc := range conf.InitialAccounts {
		bytes := util.FromHex(id)
		if len(bytes) == 0 {
			// todo: should we panic here?
			app.log.With().Error("cannot read config entry for genesis account", log.String("acct_id", id))
			continue
		}

		addr := types.BytesToAddress(bytes)
		state.CreateAccount(addr)
		state.AddBalance(addr, acc.Balance)
		state.SetNonce(addr, acc.Nonce)
		app.log.With().Info("genesis account created",
			log.String("acct_id", id),
			log.Uint64("balance", acc.Balance))
	}

	_, err := state.Commit()
	if err != nil {
		log.Panic("cannot commit genesis state")
	}
}

type weakCoinStub struct {
}

// GetResult returns the weak coin toss result
func (weakCoinStub) GetResult() bool {
	return true
}

// Wrap the top-level logger to add context info and set the level for a
// specific module.
func (app *SpacemeshApp) addLogger(name string, logger log.Log) log.Log {
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
	case NipstBuilderLogger:
		err = lvl.UnmarshalText([]byte(app.Config.LOGGING.NipstBuilderLoggerLevel))
	case AtxBuilderLogger:
		err = lvl.UnmarshalText([]byte(app.Config.LOGGING.AtxBuilderLoggerLevel))
	default:
		lvl.SetLevel(log.Level())
	}

	if err != nil {
		log.Error("cannot parse logging for %v error %v", name, err)
		lvl.SetLevel(log.Level())
	}

	app.loggers[name] = &lvl
	return logger.SetLevel(&lvl).WithName(name)
}

// SetLogLevel updates the log level of an existing logger
func (app *SpacemeshApp) SetLogLevel(name, loglevel string) error {
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

type vrfSigner interface {
	Sign([]byte) []byte
}

func (app *SpacemeshApp) initServices(ctx context.Context,
	logger log.Log,
	nodeID types.NodeID,
	swarm service.Service,
	dbStorepath string,
	sgn hare.Signer,
	isFixedOracle bool,
	rolacle hare.Rolacle,
	layerSize uint32,
	postClient activation.PostProverClient,
	poetClient activation.PoetProvingServiceClient,
	vrfSigner vrfSigner,
	layersPerEpoch uint16, clock TickProvider) error {

	app.nodeID = nodeID

	name := nodeID.ShortString()

	// This base logger must be debug level so that other, derived loggers are not a lower level.
	lg := log.NewWithLevel(name, zap.NewAtomicLevelAt(zapcore.DebugLevel)).WithFields(nodeID)

	types.SetLayersPerEpoch(int32(app.Config.LayersPerEpoch))

	app.log = app.addLogger(AppLogger, lg)

	postClient.SetLogger(app.addLogger(PostLogger, lg))

	db, err := database.NewLDBDatabase(filepath.Join(dbStorepath, "state"), 0, 0, app.addLogger(StateDbLogger, lg))
	if err != nil {
		return err
	}
	app.closers = append(app.closers, db)

	coinToss := weakCoinStub{}

	atxdbstore, err := database.NewLDBDatabase(filepath.Join(dbStorepath, "atx"), 0, 0, app.addLogger(AtxDbStoreLogger, lg))
	if err != nil {
		return err
	}
	app.closers = append(app.closers, atxdbstore)

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
	validator := activation.NewValidator(&app.Config.POST, poetDb)
	mdb, err := mesh.NewPersistentMeshDB(filepath.Join(dbStorepath, "mesh"), app.Config.BlockCacheSize, app.addLogger(MeshDBLogger, lg))
	if err != nil {
		return err
	}

	app.txPool = state.NewTxMemPool()
	meshAndPoolProjector := pendingtxs.NewMeshAndPoolProjector(mdb, app.txPool)

	appliedTxs, err := database.NewLDBDatabase(filepath.Join(dbStorepath, "appliedTxs"), 0, 0, lg.WithName("appliedTxs"))
	if err != nil {
		return err
	}
	app.closers = append(app.closers, appliedTxs)
	processor := state.NewTransactionProcessor(db, appliedTxs, meshAndPoolProjector, app.txPool, lg.WithName("state"))

	goldenATXID := types.ATXID(types.HexToHash32(app.Config.GoldenATXID))
	if goldenATXID == *types.EmptyATXID {
		logger.Panic("invalid golden atx id")
	}

	atxdb := activation.NewDB(atxdbstore, idStore, mdb, layersPerEpoch, goldenATXID, validator, app.addLogger(AtxDbLogger, lg))
	beaconProvider := &blocks.EpochBeaconProvider{}

	var msh *mesh.Mesh
	var trtl *tortoise.ThreadSafeVerifyingTortoise
	trtlCfg := tortoise.Config{
		LayerSyze: int(layerSize),
		Database:  mdb,
		Hdist:     app.Config.Hdist,
		Log:       app.addLogger(TrtlLogger, lg),
		Recovered: mdb.PersistentData(),
	}

	trtl = tortoise.NewVerifyingTortoise(trtlCfg)
	if trtlCfg.Recovered {
		msh = mesh.NewRecoveredMesh(mdb, atxdb, app.Config.REWARD, trtl, app.txPool, processor, app.addLogger(MeshLogger, lg))
		go msh.CacheWarmUp(app.Config.LayerAvgSize)
	} else {
		msh = mesh.NewMesh(mdb, atxdb, app.Config.REWARD, trtl, app.txPool, processor, app.addLogger(MeshLogger, lg))
		app.setupGenesis(processor, msh)
	}

	eValidator := blocks.NewBlockEligibilityValidator(layerSize, app.Config.GenesisTotalWeight, layersPerEpoch, atxdb, beaconProvider, signing.VRFVerify, msh, app.addLogger(BlkEligibilityLogger, lg))

	syncConf := sync.Configuration{Concurrency: 4,
		LayerSize:       int(layerSize),
		LayersPerEpoch:  layersPerEpoch,
		RequestTimeout:  time.Duration(app.Config.SyncRequestTimeout) * time.Millisecond,
		SyncInterval:    time.Duration(app.Config.SyncInterval) * time.Second,
		ValidationDelta: time.Duration(app.Config.SyncValidationDelta) * time.Second,
		Hdist:           app.Config.Hdist,
		AtxsLimit:       app.Config.AtxsPerBlock,
		AlwaysListen:    app.Config.AlwaysListen,
		GoldenATXID:     goldenATXID,
	}

	if app.Config.AtxsPerBlock > miner.AtxsPerBlockLimit { // validate limit
		logger.With().Panic("number of atxs per block required is bigger than the limit",
			log.Int("atxs_per_block", app.Config.AtxsPerBlock),
			log.Int("limit", miner.AtxsPerBlockLimit))
	}

	// we can't have an epoch offset which is greater/equal than the number of layers in an epoch
	if app.Config.HareEligibility.EpochOffset >= app.Config.BaseConfig.LayersPerEpoch {
		logger.With().Panic("epoch offset cannot be greater than or equal to the number of layers per epoch",
			log.Int("epoch_offset", app.Config.HareEligibility.EpochOffset),
			log.Int("layers_per_epoch", app.Config.BaseConfig.LayersPerEpoch))
	}

	bCfg := blocks.Config{
		Depth:       app.Config.Hdist,
		GoldenATXID: goldenATXID,
	}
	blockListener := blocks.NewBlockHandler(bCfg, msh, eValidator, lg)

	remoteFetchService := fetch.NewFetch(ctx, app.Config.FETCH, swarm, app.addLogger(Fetcher, lg))

	layerFetch := layerfetcher.NewLogic(ctx, app.Config.LAYERS, blockListener, atxdb, poetDb, atxdb, processor, swarm, remoteFetchService, msh, app.addLogger(Fetcher, lg))
	layerFetch.AddDBs(mdb.Blocks(), atxdbstore, mdb.Transactions(), poetDbStore, mdb.InputVector())

	syncer := sync.NewSync(ctx, swarm, msh, app.txPool, atxdb, eValidator, poetDb, syncConf, clock, layerFetch, app.addLogger(SyncLogger, lg))
	blockOracle := blocks.NewMinerBlockOracle(layerSize, app.Config.GenesisTotalWeight, layersPerEpoch, atxdb, beaconProvider, vrfSigner, nodeID, syncer.ListenToGossip, app.addLogger(BlockOracle, lg))

	// TODO: we should probably decouple the apptest and the node (and duplicate as necessary) (#1926)
	var hOracle hare.Rolacle
	if isFixedOracle {
		// fixed rolacle, take the provided rolacle
		hOracle = rolacle
	} else {
		// regular oracle, build and use it
		// TODO: this mock will be replaced by the real Tortoise beacon once
		//   https://github.com/spacemeshos/go-spacemesh/pull/2267 is complete
		beacon := eligibility.NewBeacon(tortoiseBeaconMock{}, app.Config.HareEligibility.ConfidenceParam, app.addLogger(HareBeaconLogger, lg))
		hOracle = eligibility.New(beacon, atxdb.GetMinerWeightsInEpochFromView, signing.VRFVerify, vrfSigner, uint16(app.Config.LayersPerEpoch), app.Config.POST.SpacePerUnit, app.Config.GenesisTotalWeight, app.Config.SpaceToCommit, mdb, app.Config.HareEligibility, app.addLogger(HareOracleLogger, lg))
		// TODO: genesisMinerWeight is set to app.Config.SpaceToCommit, because PoET ticks are currently hardcoded to 1
	}

	gossipListener := service.NewListener(swarm, layerFetch, syncer.ListenToGossip, app.addLogger(GossipListener, lg))
	ha := app.HareFactory(ctx, mdb, swarm, sgn, nodeID, syncer, msh, hOracle, idStore, clock, lg)

	stateAndMeshProjector := pendingtxs.NewStateAndMeshProjector(processor, msh)
	minerCfg := miner.Config{
		Hdist:          app.Config.Hdist,
		MinerID:        nodeID,
		AtxsPerBlock:   app.Config.AtxsPerBlock,
		LayersPerEpoch: layersPerEpoch,
		TxsPerBlock:    app.Config.TxsPerBlock,
	}

	database.SwitchCreationContext(dbStorepath, "") // currently only blockbuilder uses this mechanism
	blockProducer := miner.NewBlockBuilder(minerCfg, sgn, swarm, clock.Subscribe(), coinToss, msh, trtl, ha, blockOracle, syncer, stateAndMeshProjector, app.txPool, atxdb, app.addLogger(BlockBuilderLogger, lg))

	poetListener := activation.NewPoetListener(swarm, poetDb, app.addLogger(PoetListenerLogger, lg))

	nipstBuilder := activation.NewNIPSTBuilder(util.Hex2Bytes(nodeID.Key), postClient, poetClient, poetDb, store, app.addLogger(NipstBuilderLogger, lg))

	coinBase := types.HexToAddress(app.Config.CoinbaseAccount)

	if coinBase.Big().Uint64() == 0 && app.Config.StartMining {
		logger.Panic("invalid coinbase account")
	}
	if app.Config.SpaceToCommit == 0 {
		app.Config.SpaceToCommit = app.Config.POST.SpacePerUnit
	}

	builderConfig := activation.Config{
		CoinbaseAccount: coinBase,
		GoldenATXID:     goldenATXID,
		LayersPerEpoch:  layersPerEpoch,
	}

	atxBuilder := activation.NewBuilder(builderConfig, nodeID, app.Config.SpaceToCommit, sgn, atxdb, swarm, msh, layersPerEpoch, nipstBuilder, postClient, clock, syncer, store, app.addLogger("atxBuilder", lg))

	gossipListener.AddListener(ctx, state.IncomingTxProtocol, priorityq.Low, processor.HandleTxGossipData)
	gossipListener.AddListener(ctx, activation.AtxProtocol, priorityq.Low, atxdb.HandleGossipAtx)
	gossipListener.AddListener(ctx, blocks.NewBlockProtocol, priorityq.High, blockListener.HandleBlock)

	app.blockProducer = blockProducer
	app.blockListener = blockListener
	app.gossipListener = gossipListener
	app.mesh = msh
	app.syncer = syncer
	app.clock = clock
	app.state = processor
	app.hare = ha
	app.P2P = swarm
	app.poetListener = poetListener
	app.atxBuilder = atxBuilder
	app.oracle = blockOracle
	app.txProcessor = processor
	app.atxDb = atxdb
	app.layerFetch = layerFetch

	return nil
}

// periodically checks that our clock is sync
func (app *SpacemeshApp) checkTimeDrifts() {
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

// HareFactory returns a hare consensus algorithm according to the parameters is app.Config.Hare.SuperHare
func (app *SpacemeshApp) HareFactory(ctx context.Context, mdb *mesh.DB, swarm service.Service, sgn hare.Signer, nodeID types.NodeID, syncer *sync.Syncer, msh *mesh.Mesh, hOracle hare.Rolacle, idStore *activation.IdentityStore, clock TickProvider, lg log.Log) HareService {
	if app.Config.HARE.SuperHare {
		hr := turbohare.New(ctx, msh)
		mdb.InputVectorBackupFunc = hr.GetResult
		return hr
	}

	// a function to validate we know the blocks
	validationFunc := func(ids []types.BlockID) bool {
		for _, b := range ids {
			res, err := mdb.GetBlock(b)
			if err != nil {
				app.log.WithContext(ctx).With().Error("output set block not in database", b, log.Err(err))
				return false
			}
			if res == nil {
				app.log.WithContext(ctx).With().Error("output set block not in database (BUG BUG BUG - FetchBlock return err nil and res nil)", b)
				return false
			}
		}

		return true
	}
	ha := hare.New(app.Config.HARE, swarm, sgn, nodeID, validationFunc, syncer.IsHareSynced, msh, hOracle, uint16(app.Config.LayersPerEpoch), idStore, hOracle, clock.Subscribe(), app.addLogger(HareLogger, lg))
	return ha
}

func (app *SpacemeshApp) startServices(ctx context.Context, logger log.Log) {
	app.layerFetch.Start()
	go app.startSyncer(ctx)

	if err := app.hare.Start(ctx); err != nil {
		logger.Panic("cannot start hare")
	}
	if err := app.blockProducer.Start(ctx); err != nil {
		logger.Panic("cannot start block producer")
	}

	app.poetListener.Start(ctx)

	if app.Config.StartMining {
		coinBase := types.HexToAddress(app.Config.CoinbaseAccount)
		if err := app.atxBuilder.StartPost(ctx, coinBase, app.Config.POST.DataDir, app.Config.SpaceToCommit); err != nil {
			logger.With().Panic("error initializing post", log.Err(err))
		}
	} else {
		logger.Info("manual post init")
	}
	app.atxBuilder.Start(ctx)
	app.clock.StartNotifying()
	go app.checkTimeDrifts()
}

func (app *SpacemeshApp) startAPIServices(ctx context.Context, net api.NetworkAPI) {
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
		registerService(grpcserver.NewMeshService(app.mesh, app.txPool, app.clock, app.Config.LayersPerEpoch, app.Config.P2P.NetworkID, layerDuration, app.Config.LayerAvgSize, app.Config.TxsPerBlock))
	}
	if apiConf.StartNodeService {
		registerService(grpcserver.NewNodeService(net, app.mesh, app.clock, app.syncer))
	}
	if apiConf.StartSmesherService {
		registerService(grpcserver.NewSmesherService(app.atxBuilder))
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

func (app *SpacemeshApp) stopServices() {
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

	if app.poetListener != nil {
		app.log.Info("closing poet listener")
		app.poetListener.Close()
	}

	if app.atxBuilder != nil {
		app.log.Info("closing atx builder")
		app.atxBuilder.Stop()
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
		app.log.Info("%v closing layerFetch", app.nodeID.Key)
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

	if app.gossipListener != nil {
		app.gossipListener.Stop()
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
func (app *SpacemeshApp) LoadOrCreateEdSigner() (*signing.EdSigner, error) {
	f, err := app.getIdentityFile()
	if err != nil {
		log.With().Warning("failed to find identity file", log.Err(err))

		edSgn := signing.NewEdSigner()
		f = filepath.Join(shared.GetInitDir(app.Config.POST.DataDir, edSgn.PublicKey().Bytes()), edKeyFileName)
		err := os.MkdirAll(filepath.Dir(f), filesystem.OwnerReadWriteExec)
		if err != nil {
			return nil, fmt.Errorf("failed to create directory for identity file: %v", err)
		}
		err = ioutil.WriteFile(f, edSgn.ToBuffer(), filesystem.OwnerReadWrite)
		if err != nil {
			return nil, fmt.Errorf("failed to write identity file: %v", err)
		}
		log.With().Warning("created new identity", edSgn.PublicKey())
		return edSgn, nil
	}

	buff, err := ioutil.ReadFile(f)
	if err != nil {
		return nil, fmt.Errorf("failed to read identity from file: %v", err)
	}
	edSgn, err := signing.NewEdSignerFromBuffer(buff)
	if err != nil {
		return nil, fmt.Errorf("failed to construct identity from data file: %v", err)
	}
	if edSgn.PublicKey().String() != filepath.Base(filepath.Dir(f)) {
		return nil, fmt.Errorf("identity file path ('%s') does not match public key (%s)", filepath.Dir(f), edSgn.PublicKey().String())
	}
	log.With().Info("loaded identity from file", log.String("file", f))
	return edSgn, nil
}

type identityFileFound struct{}

func (identityFileFound) Error() string {
	return "identity file found"
}

func (app *SpacemeshApp) getIdentityFile() (string, error) {
	var f string
	err := filepath.Walk(app.Config.POST.DataDir, func(path string, info os.FileInfo, err error) error {
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
		return "", fmt.Errorf("failed to traverse PoST data dir: %v", err)
	}
	return "", fmt.Errorf("not found")
}

func (app *SpacemeshApp) startSyncer(ctx context.Context) {
	if app.P2P == nil {
		app.log.Error("syncer started before p2p is initialized")
	} else {
		<-app.P2P.GossipReady()
	}
	app.syncer.Start(ctx)
}

// Start starts the Spacemesh node and initializes all relevant services according to command line arguments provided.
func (app *SpacemeshApp) Start(*cobra.Command, []string) {
	// we use the main app context
	ctx := cmdp.Ctx

	// Create a contextual logger for local usage (lower-level modules will create their own contextual loggers
	// using context passed down to them)
	logger := log.AppLog.WithContext(ctx)

	logger.With().Info("starting spacemesh",
		log.String("data-dir", app.Config.DataDir()),
		log.String("post-dir", app.Config.POST.DataDir))

	err := filesystem.ExistOrCreate(app.Config.DataDir())
	if err != nil {
		logger.With().Error("data-dir not found or could not be created", log.Err(err))
	}

	/* Setup monitoring */
	if app.Config.PprofHTTPServer {
		logger.Info("starting pprof server")
		srv := &http.Server{Addr: ":6060"}
		defer srv.Shutdown(ctx)
		go func() {
			if err := srv.ListenAndServe(); err != nil {
				logger.With().Error("cannot start pprof http server", log.Err(err))
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
			logger.With().Error("cannot start profiling client")
		} else {
			defer p.Stop()
		}

	}

	/* Create or load miner identity */

	app.edSgn, err = app.LoadOrCreateEdSigner()
	if err != nil {
		logger.With().Panic("could not retrieve identity", log.Err(err))
	}

	poetClient := activation.NewHTTPPoetClient(ctx, app.Config.PoETServer)

	edPubkey := app.edSgn.PublicKey()
	vrfSigner, vrfPub, err := signing.NewVRFSigner(app.edSgn.Sign(edPubkey.Bytes()))
	if err != nil {
		logger.With().Panic("failed to create vrf signer", log.Err(err))
	}

	nodeID := types.NodeID{Key: edPubkey.String(), VRFPublicKey: vrfPub}

	postClient, err := activation.NewPostClient(&app.Config.POST, util.Hex2Bytes(nodeID.Key))
	if err != nil {
		logger.With().Error("failed to create post client", log.Err(err))
	}

	// This base logger must be debug level so that other, derived loggers are not a lower level.
	lg := log.NewWithLevel(nodeID.ShortString(), zap.NewAtomicLevelAt(zapcore.DebugLevel)).WithFields(nodeID)

	/* Initialize all protocol services */

	dbStorepath := app.Config.DataDir()
	gTime, err := time.Parse(time.RFC3339, app.Config.GenesisTime)
	if err != nil {
		logger.With().Error("cannot parse genesis time", log.Err(err))
	}
	ld := time.Duration(app.Config.LayerDurationSec) * time.Second
	clock := timesync.NewClock(timesync.RealClock{}, ld, gTime, log.NewDefault("clock"))

	logger.Info("initializing p2p services")
	swarm, err := p2p.New(ctx, app.Config.P2P, app.addLogger(P2PLogger, lg), dbStorepath)
	if err != nil {
		logger.With().Panic("error starting p2p services", log.Err(err))
	}

	if err = app.initServices(ctx,
		logger,
		nodeID,
		swarm,
		dbStorepath,
		app.edSgn,
		false,
		nil,
		uint32(app.Config.LayerAvgSize),
		postClient,
		poetClient,
		vrfSigner,
		uint16(app.Config.LayersPerEpoch),
		clock); err != nil {
		logger.With().Error("cannot start services", log.Err(err))
		return
	}

	if app.Config.CollectMetrics {
		metrics.StartCollectingMetrics(app.Config.MetricsPort)
	}

	app.startServices(ctx, logger)

	// P2P must start last to not block when sending messages to protocols
	if err = app.P2P.Start(ctx); err != nil {
		logger.With().Panic("error starting p2p services", log.Err(err))
	}

	app.startAPIServices(ctx, app.P2P)
	events.SubscribeToLayers(clock.Subscribe())
	logger.Info("app started")

	// notify anyone who might be listening that the app has finished starting.
	// this can be used by, e.g., app tests.
	close(app.started)

	// app blocks until it receives a signal to exit
	// this signal may come from the node or from sig-abort (ctrl-c)
	<-ctx.Done()
	events.ReportError(events.NodeError{
		Msg:   "node is shutting down",
		Level: zapcore.InfoLevel,
	})
}
