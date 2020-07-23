// Package node contains the main executable for go-spacemesh node
package node

import "C"
import (
	"context"
	"fmt"
	"github.com/spacemeshos/amcl"
	"github.com/spacemeshos/amcl/BLS381"
	"github.com/spacemeshos/go-spacemesh/activation"
	apiCfg "github.com/spacemeshos/go-spacemesh/api/config"
	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/hare/eligibility"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/metrics"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/oracle"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/pendingtxs"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/state"
	"github.com/spacemeshos/go-spacemesh/sync"
	"github.com/spacemeshos/go-spacemesh/tortoise"
	"github.com/spacemeshos/go-spacemesh/turbohare"
	"github.com/spacemeshos/post/shared"
	"go.uber.org/zap"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"time"

	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	cfg "github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/timesync"
	timeCfg "github.com/spacemeshos/go-spacemesh/timesync/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

import _ "net/http/pprof" // import for memory and network profiling

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
	PoetListenerLogger   = "poetListener"
	NipstBuilderLogger   = "nipstBuilder"
	AtxBuilderLogger     = "atxBuilder"
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
	Start() error
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

// SpacemeshApp is the cli app singleton
type SpacemeshApp struct {
	*cobra.Command
	nodeID            types.NodeID
	P2P               p2p.Service
	Config            *cfg.Config
	grpcAPIService    *api.SpacemeshGrpcService
	jsonAPIService    *api.JSONHTTPServer
	newgrpcAPIService *grpcserver.Server
	newjsonAPIService *grpcserver.JSONHTTPServer
	syncer            *sync.Syncer
	blockListener     *sync.BlockListener
	state             *state.TransactionProcessor
	blockProducer     *miner.BlockBuilder
	oracle            *oracle.MinerBlockOracle
	txProcessor       *state.TransactionProcessor
	mesh              *mesh.Mesh
	clock             TickProvider
	hare              HareService
	atxBuilder        *activation.Builder
	poetListener      *activation.PoetListener
	edSgn             *signing.EdSigner
	closers           []interface{ Close() }
	log               log.Log
	txPool            *miner.TxMempool
	loggers           map[string]*zap.AtomicLevel
	term              chan struct{} // this channel is closed when closing services, goroutines should wait on this channel in order to terminate
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
	err = app.ParseConfig()

	if err != nil {
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
	log.InitSpacemeshLoggingSystem(app.Config.DataDir(), "spacemesh.log")

	log.Info("%s", app.getAppInfo())

	if app.Config.PublishEventsURL != "" {
		log.Info("pubsubing on %v", app.Config.PublishEventsURL)
		events.InitializeEventPubsub(app.Config.PublishEventsURL)
	}
}

func (app *SpacemeshApp) getAppInfo() string {
	return fmt.Sprintf("App version: %s. Git: %s - %s . Go Version: %s. OS: %s-%s ",
		cmdp.Version, cmdp.Branch, cmdp.Commit, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

// Cleanup stops all app services
func (app *SpacemeshApp) Cleanup(cmd *cobra.Command, args []string) (err error) {
	log.Info("App Cleanup starting...")
	app.stopServices()
	// add any other Cleanup tasks here....
	log.Info("App Cleanup completed\n\n")

	return nil
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
			log.Error("cannot read config entry for :%s", id)
			continue
		}

		addr := types.BytesToAddress(bytes)
		state.CreateAccount(addr)
		state.AddBalance(addr, acc.Balance)
		state.SetNonce(addr, acc.Nonce)
		app.log.Info("Genesis account created: %s, Balance: %s", id, acc.Balance.Uint64())
	}

	_, err := state.Commit()
	if err != nil {
		log.Panic("cannot commit genesis state")
	}
	err = msh.AddBlock(mesh.GenesisBlock)
	if err != nil {
		log.Error("error adding genesis block %v", err)
	}
}

func (app *SpacemeshApp) setupTestFeatures() {
	// NOTE: any test-related feature enabling should happen here.
	api.ApproveAPIGossipMessages(cmdp.Ctx, app.P2P)
}

type weakCoinStub struct {
}

// GetResult returns the weak coin toss result
func (weakCoinStub) GetResult() bool {
	return true
}

func (app *SpacemeshApp) addLogger(name string, logger log.Log) log.Log {
	log.Level()
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

// SetLogLevel sets the specific log level for the specified logger name, Log level can be WARN, INFO, DEBUG
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

func (app *SpacemeshApp) initServices(nodeID types.NodeID,
	swarm service.Service,
	dbStorepath string,
	sgn hare.Signer,
	isFixedOracle bool,
	rolacle hare.Rolacle,
	layerSize uint32,
	postClient activation.PostProverClient,
	poetClient activation.PoetProvingServiceClient,
	vrfSigner *BLS381.BlsSigner,
	layersPerEpoch uint16, clock TickProvider) error {

	app.nodeID = nodeID

	name := nodeID.ShortString()

	lg := log.NewDefault(name).WithFields(nodeID)

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

	app.txPool = miner.NewTxMemPool()
	atxpool := miner.NewAtxMemPool()
	meshAndPoolProjector := pendingtxs.NewMeshAndPoolProjector(mdb, app.txPool)

	appliedTxs, err := database.NewLDBDatabase(filepath.Join(dbStorepath, "appliedTxs"), 0, 0, lg.WithName("appliedTxs"))
	if err != nil {
		return err
	}
	app.closers = append(app.closers, appliedTxs)
	processor := state.NewTransactionProcessor(db, appliedTxs, meshAndPoolProjector, lg.WithName("state"))

	atxdb := activation.NewDB(atxdbstore, idStore, mdb, layersPerEpoch, validator, app.addLogger(AtxDbLogger, lg))
	beaconProvider := &oracle.EpochBeaconProvider{}
	eValidator := oracle.NewBlockEligibilityValidator(layerSize, uint32(app.Config.GenesisActiveSet), layersPerEpoch, atxdb, beaconProvider, BLS381.Verify2, app.addLogger(BlkEligibilityLogger, lg))

	var msh *mesh.Mesh
	var trtl tortoise.Tortoise
	if mdb.PersistentData() {
		trtl = tortoise.NewRecoveredTortoise(mdb, app.addLogger(TrtlLogger, lg))
		msh = mesh.NewRecoveredMesh(mdb, atxdb, app.Config.REWARD, trtl, app.txPool, atxpool, processor, app.addLogger(MeshLogger, lg))
		go msh.CacheWarmUp(app.Config.LayerAvgSize)
	} else {
		trtl = tortoise.NewTortoise(int(layerSize), mdb, app.Config.Hdist, app.addLogger(TrtlLogger, lg))
		msh = mesh.NewMesh(mdb, atxdb, app.Config.REWARD, trtl, app.txPool, atxpool, processor, app.addLogger(MeshLogger, lg))
		app.setupGenesis(processor, msh)
	}

	syncConf := sync.Configuration{Concurrency: 4,
		LayerSize:       int(layerSize),
		LayersPerEpoch:  layersPerEpoch,
		RequestTimeout:  time.Duration(app.Config.SyncRequestTimeout) * time.Millisecond,
		SyncInterval:    time.Duration(app.Config.SyncInterval) * time.Second,
		ValidationDelta: time.Duration(app.Config.SyncValidationDelta) * time.Second,
		Hdist:           app.Config.Hdist,
		AtxsLimit:       app.Config.AtxsPerBlock}

	if app.Config.AtxsPerBlock > miner.AtxsPerBlockLimit { // validate limit
		app.log.Panic("Number of atxs per block required is bigger than the limit atxsPerBlock=%v limit=%v", app.Config.AtxsPerBlock, miner.AtxsPerBlockLimit)
	}

	// we can't have an epoch offset which is greater/equal than the number of layers in an epoch
	if app.Config.HareEligibility.EpochOffset >= app.Config.BaseConfig.LayersPerEpoch {
		app.log.Panic("Epoch offset cannot be greater than or equal to the number of layers per epoch EpochOffset=%v LayersPerEpoch=%v",
			app.Config.HareEligibility.EpochOffset, app.Config.BaseConfig.LayersPerEpoch)
	}

	syncer := sync.NewSync(swarm, msh, app.txPool, atxpool, eValidator, poetDb, syncConf, clock, app.addLogger(SyncLogger, lg))
	blockOracle := oracle.NewMinerBlockOracle(layerSize, uint32(app.Config.GenesisActiveSet), layersPerEpoch, atxdb, beaconProvider, vrfSigner, nodeID, syncer.ListenToGossip, app.addLogger(BlockOracle, lg))

	// TODO: we should probably decouple the apptest and the node (and duplicate as necessary) (#1926)
	var hOracle hare.Rolacle
	if isFixedOracle { // fixed rolacle, take the provided rolacle
		hOracle = rolacle
	} else { // regular oracle, build and use it
		beacon := eligibility.NewBeacon(mdb, app.Config.HareEligibility.ConfidenceParam, app.addLogger(HareBeaconLogger, lg))
		hOracle = eligibility.New(beacon, atxdb.CalcActiveSetSize, BLS381.Verify2, vrfSigner, uint16(app.Config.LayersPerEpoch), app.Config.GenesisActiveSet, mdb, app.Config.HareEligibility, app.addLogger(HareOracleLogger, lg))
	}

	ha := app.HareFactory(mdb, swarm, sgn, nodeID, syncer, msh, hOracle, idStore, clock, lg)

	stateAndMeshProjector := pendingtxs.NewStateAndMeshProjector(processor, msh)
	blockProducer := miner.NewBlockBuilder(nodeID, sgn, swarm, clock.Subscribe(),
		app.Config.Hdist, app.txPool, atxpool, coinToss, msh, ha, blockOracle, processor,
		atxdb, syncer, app.Config.AtxsPerBlock, app.Config.TxsPerBlock, layersPerEpoch,
		stateAndMeshProjector, app.addLogger(BlockBuilderLogger, lg))
	blockListener := sync.NewBlockListener(swarm, syncer, 4, app.addLogger(BlockListenerLogger, lg))

	msh.SetBlockBuilder(blockProducer)

	poetListener := activation.NewPoetListener(swarm, poetDb, app.addLogger(PoetListenerLogger, lg))

	nipstBuilder := activation.NewNIPSTBuilder(util.Hex2Bytes(nodeID.Key), postClient, poetClient, poetDb, store, app.addLogger(NipstBuilderLogger, lg))

	coinBase := types.HexToAddress(app.Config.CoinbaseAccount)

	if coinBase.Big().Uint64() == 0 && app.Config.StartMining {
		app.log.Panic("invalid Coinbase account")
	}
	atxBuilder := activation.NewBuilder(nodeID, coinBase, sgn, atxdb, swarm, msh, layersPerEpoch, nipstBuilder, postClient, clock, syncer, store, app.addLogger("atxBuilder", lg))

	app.blockProducer = blockProducer
	app.blockListener = blockListener
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
				app.log.Error("System time couldn't synchronize %s", err)
				cmdp.Cancel()
				return
			}
		}
	}
}

// HareFactory returns a hare consensus algorithm according to the parameters is app.Config.Hare.SuperHare
func (app *SpacemeshApp) HareFactory(mdb *mesh.DB, swarm service.Service, sgn hare.Signer, nodeID types.NodeID, syncer *sync.Syncer, msh *mesh.Mesh, hOracle hare.Rolacle, idStore *activation.IdentityStore, clock TickProvider, lg log.Log) HareService {
	if app.Config.HARE.SuperHare {
		return turbohare.New(msh)
	}

	// a function to validate we know the blocks
	validationFunc := func(ids []types.BlockID) bool {
		for _, b := range ids {
			res, err := mdb.GetBlock(b)
			if err != nil {
				app.log.With().Error("output set block not in database", b, log.Err(err))
				return false
			}
			if res == nil {
				app.log.With().Error("output set block not in database (BUG BUG BUG - GetBlock return err nil and res nil)", b)
				return false
			}

		}

		return true
	}
	ha := hare.New(app.Config.HARE, swarm, sgn, nodeID, validationFunc, syncer.IsSynced, msh, hOracle, uint16(app.Config.LayersPerEpoch), idStore, hOracle, clock.Subscribe(), app.addLogger(HareLogger, lg))
	return ha
}

func (app *SpacemeshApp) startServices() {
	app.blockListener.Start()
	app.syncer.Start()
	err := app.hare.Start()
	if err != nil {
		log.Panic("cannot start hare")
	}
	err = app.blockProducer.Start()
	if err != nil {
		log.Panic("cannot start block producer")
	}

	app.poetListener.Start()

	if app.Config.StartMining {
		coinBase := types.HexToAddress(app.Config.CoinbaseAccount)
		err := app.atxBuilder.StartPost(coinBase, app.Config.POST.DataDir, app.Config.POST.SpacePerUnit)
		if err != nil {
			log.Error("Error initializing post, err: %v", err)
			log.Panic("Error initializing post")
		}
	} else {
		log.Info("Manual post init")
	}
	app.atxBuilder.Start()
	app.clock.StartNotifying()
	go app.checkTimeDrifts()
}

func (app *SpacemeshApp) startAPIServices(postClient api.PostAPI, net api.NetworkAPI) {
	apiConf := &app.Config.API

	// OLD API SERVICES (deprecated)
	layerDuration := app.Config.LayerDurationSec
	if apiConf.StartGrpcServer || apiConf.StartJSONServer {
		// start grpc if specified or if json rpc specified
		app.grpcAPIService = api.NewGrpcService(apiConf.GrpcServerPort, net, app.state, app.mesh, app.txPool,
			app.atxBuilder, app.oracle, app.clock, postClient, layerDuration, app.syncer, app.Config, app)
		app.grpcAPIService.StartService()
	}

	if apiConf.StartJSONServer {
		app.jsonAPIService = api.NewJSONHTTPServer(apiConf.JSONServerPort, apiConf.GrpcServerPort)
		app.jsonAPIService.StartService()
	}

	// NEW API SERVICES
	// These work a little differently than the old services. Since we have multiple
	// GRPC services, we cannot automatically enable them if the gateway server is
	// enabled (since we don't know which ones to enable), so it's an error if the
	// gateway server is enabled without enabling at least one GRPC service.

	// Make sure we only start the server once
	startService := func(svc grpcserver.ServiceAPI) {
		if app.newgrpcAPIService == nil {
			app.newgrpcAPIService = grpcserver.NewServer(apiConf.NewGrpcServerPort)
			app.newgrpcAPIService.Start()
		}
		svc.RegisterService(app.newgrpcAPIService)
	}

	// Start the requested services one by one
	if apiConf.StartNodeService {
		startService(grpcserver.NewNodeService(net, app.mesh, app.clock, app.syncer))
	}
	if apiConf.StartMeshService {
		startService(grpcserver.NewMeshService(net, app.mesh, app.txPool, app.clock, app.syncer, app.Config.LayersPerEpoch, app.Config.P2P.NetworkID, layerDuration, app.Config.LayerAvgSize, app.Config.TxsPerBlock))
	}

	if apiConf.StartNewJSONServer {
		if app.newgrpcAPIService == nil {
			// This panics because it should not happen.
			// It should be caught inside apiConf.
			log.Panic("one or more new GRPC services must be enabled with new JSON gateway server.")
			return
		}
		app.newjsonAPIService = grpcserver.NewJSONHTTPServer(apiConf.NewJSONServerPort, apiConf.NewGrpcServerPort)
		app.newjsonAPIService.StartService(apiConf.StartNodeService, apiConf.StartMeshService)
	}
}

func (app *SpacemeshApp) stopServices() {
	// all go-routines that listen to app.term will close
	// note: there is no guarantee that a listening go-routine will close before stopServices exits
	close(app.term)

	if app.jsonAPIService != nil {
		log.Info("Stopping JSON service api...")
		app.jsonAPIService.Close()
	}

	if app.grpcAPIService != nil {
		log.Info("Stopping grpc service...")
		app.grpcAPIService.Close()
	}

	if app.newjsonAPIService != nil {
		log.Info("Stopping new JSON gateway service...")
		app.newjsonAPIService.Close()
	}

	if app.newgrpcAPIService != nil {
		log.Info("Stopping new grpc service...")
		app.newgrpcAPIService.Close()
	}

	if app.blockProducer != nil {
		app.log.Info("%v closing block producer", app.nodeID.Key)
		if err := app.blockProducer.Close(); err != nil {
			log.Error("cannot stop block producer %v", err)
		}
	}

	if app.clock != nil {
		app.log.Info("%v closing clock", app.nodeID.Key)
		app.clock.Close()
	}

	if app.poetListener != nil {
		app.log.Info("closing PoET listener")
		app.poetListener.Close()
	}

	if app.atxBuilder != nil {
		app.log.Info("closing atx builder")
		app.atxBuilder.Stop()
	}

	if app.blockListener != nil {
		app.log.Info("%v closing blockListener", app.nodeID.Key)
		app.blockListener.Close()
	}

	if app.hare != nil {
		app.log.Info("%v closing Hare", app.nodeID.Key)
		app.hare.Close()
	}

	if app.P2P != nil {
		app.log.Info("%v closing p2p", app.nodeID.Key)
		app.P2P.Shutdown()
	}

	if app.mesh != nil {
		app.log.Info("%v closing mesh", app.nodeID.Key)
		app.mesh.Close()
	}

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
		log.Warning("Failed to find identity file: %v", err)

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
		log.Warning("Created new identity with public key %v", edSgn.PublicKey())
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
	log.Info("Loaded identity from file ('%s')", f)
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

// Start starts the Spacemesh node and initializes all relevant services according to command line arguments provided.
func (app *SpacemeshApp) Start(cmd *cobra.Command, args []string) {
	log.With().Info("Starting Spacemesh", log.String("data-dir", app.Config.DataDir()), log.String("post-dir", app.Config.POST.DataDir))

	err := filesystem.ExistOrCreate(app.Config.DataDir())
	if err != nil {
		log.Error("data-dir not found or could not be created err:%v", err)
	}

	/* Setup monitoring */

	if app.Config.MemProfile != "" {
		log.Info("Starting mem profiling")
		f, err := os.Create(app.Config.MemProfile)
		if err != nil {
			log.Error("could not create memory profile: ", err)
		}
		defer f.Close()
		runtime.GC() // get up-to-date statistics
		if err := pprof.WriteHeapProfile(f); err != nil {
			log.Error("could not write memory profile: ", err)
		}
	}

	if app.Config.CPUProfile != "" {
		log.Info("Starting cpu profile")
		f, err := os.Create(app.Config.CPUProfile)
		if err != nil {
			log.Error("could not create CPU profile: ", err)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Error("could not start CPU profile: ", err)
		}
		defer pprof.StopCPUProfile()
	}

	if app.Config.PprofHTTPServer {
		log.Info("Starting pprof server")
		srv := &http.Server{Addr: ":6060"}
		defer srv.Shutdown(context.TODO())
		go func() {
			err := srv.ListenAndServe()
			if err != nil {
				log.Error("cannot start http server", err)
			}
		}()

	}

	/* Create or load miner identity */

	app.edSgn, err = app.LoadOrCreateEdSigner()
	if err != nil {
		log.Panic("Could not retrieve identity err=%v", err)
	}

	poetClient := activation.NewHTTPPoetClient(cmdp.Ctx, app.Config.PoETServer)

	rng := amcl.NewRAND()
	pub := app.edSgn.PublicKey().Bytes()
	rng.Seed(len(pub), app.edSgn.Sign(pub)) // assuming ed.private is random, the sig can be used as seed
	vrfPriv, vrfPub := BLS381.GenKeyPair(rng)
	vrfSigner := BLS381.NewBlsSigner(vrfPriv)
	nodeID := types.NodeID{Key: app.edSgn.PublicKey().String(), VRFPublicKey: vrfPub}

	postClient, err := activation.NewPostClient(&app.Config.POST, util.Hex2Bytes(nodeID.Key))
	if err != nil {
		log.Error("failed to create post client: %v", err)
	}

	/* Initialize all protocol services */
	lg := log.NewDefault(nodeID.ShortString())

	dbStorepath := app.Config.DataDir()
	gTime, err := time.Parse(time.RFC3339, app.Config.GenesisTime)
	if err != nil {
		log.Error("cannot parse genesis time %v", err)
	}
	ld := time.Duration(app.Config.LayerDurationSec) * time.Second
	clock := timesync.NewClock(timesync.RealClock{}, ld, gTime, log.NewDefault("clock"))

	log.Info("Initializing P2P services")
	swarm, err := p2p.New(cmdp.Ctx, app.Config.P2P, app.addLogger(P2PLogger, lg), dbStorepath)
	if err != nil {
		log.Panic("Error starting p2p services. err: %v", err)
	}

	err = app.initServices(nodeID, swarm, dbStorepath, app.edSgn, false, nil, uint32(app.Config.LayerAvgSize), postClient, poetClient, vrfSigner, uint16(app.Config.LayersPerEpoch), clock)
	if err != nil {
		log.Error("cannot start services %v", err.Error())
		return
	}

	if app.Config.TestMode {
		app.setupTestFeatures()
	}

	if app.Config.CollectMetrics {
		metrics.StartCollectingMetrics(app.Config.MetricsPort)
	}

	app.startServices()
	// P2P must start last to not block when sending messages to protocols
	err = app.P2P.Start()
	if err != nil {
		log.Panic("Error starting p2p services: %v", err)
	}

	app.startAPIServices(postClient, app.P2P)
	log.Info("App started.")

	// app blocks until it receives a signal to exit
	// this signal may come from the node or from sig-abort (ctrl-c)
	<-cmdp.Ctx.Done()
}
