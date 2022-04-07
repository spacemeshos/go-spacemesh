// Package node contains the main executable for go-spacemesh node
package node

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/pyroscope-io/pyroscope/pkg/agent/profiler"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/beacon"
	"github.com/spacemeshos/go-spacemesh/beacon/weakcoin"
	"github.com/spacemeshos/go-spacemesh/blocks"
	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/cmd/mapstructureutil"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/config/presets"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/hare/eligibility"
	"github.com/spacemeshos/go-spacemesh/layerfetcher"
	"github.com/spacemeshos/go-spacemesh/layerpatrol"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/errcode"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/metrics"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/proposals"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/svm"
	"github.com/spacemeshos/go-spacemesh/syncer"
	"github.com/spacemeshos/go-spacemesh/system"
	"github.com/spacemeshos/go-spacemesh/timesync"
	timeCfg "github.com/spacemeshos/go-spacemesh/timesync/config"
	"github.com/spacemeshos/go-spacemesh/timesync/peersync"
	"github.com/spacemeshos/go-spacemesh/tortoise"
	"github.com/spacemeshos/go-spacemesh/turbohare"
	"github.com/spacemeshos/go-spacemesh/txs"
)

const edKeyFileName = "key.bin"

// Logger names.
const (
	AppLogger              = "app"
	P2PLogger              = "p2p"
	PostLogger             = "post"
	StateDbLogger          = "stateDbStore"
	BeaconLogger           = "beacon"
	StoreLogger            = "store"
	PoetDbLogger           = "poetDb"
	MeshDBLogger           = "meshDb"
	TrtlLogger             = "trtl"
	AtxDbLogger            = "atxDb"
	MeshLogger             = "mesh"
	SyncLogger             = "sync"
	HareOracleLogger       = "hareOracle"
	HareLogger             = "hare"
	BlockGenLogger         = "blockGenerator"
	BlockHandlerLogger     = "blockHandler"
	TxHandlerLogger        = "txHandler"
	ProposalBuilderLogger  = "proposalBuilder"
	ProposalListenerLogger = "proposalListener"
	ProposalDBLogger       = "proposalStore"
	PoetListenerLogger     = "poetListener"
	NipostBuilderLogger    = "nipostBuilder"
	LayerFetcher           = "layerFetcher"
	TimeSyncLogger         = "timesync"
	SVMLogger              = "SVM"
	ConStateLogger         = "conState"
)

// Cmd is the cobra wrapper for the node, that allows adding parameters to it.
var Cmd = &cobra.Command{
	Use:   "node",
	Short: "start node",
	Run: func(cmd *cobra.Command, args []string) {
		conf, err := loadConfig(cmd)
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
		starter := func() error {
			if err := app.Initialize(); err != nil {
				return fmt.Errorf("init node: %w", err)
			}
			// This blocks until the context is finished or until an error is produced
			if err := app.Start(); err != nil {
				return fmt.Errorf("start node: %w", err)
			}

			return nil
		}
		err = starter()
		app.Cleanup()
		if err != nil {
			log.With().Fatal("Failed to run the node. See logs for details.", log.Err(err))
		}
	},
}

// VersionCmd returns the current version of spacemesh.
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
	cmdp.AddCommands(Cmd)
	Cmd.AddCommand(VersionCmd)
}

// Service is a general service interface that specifies the basic start/stop functionality.
type Service interface {
	Start(ctx context.Context) error
	Close()
}

// HareService is basic definition of hare algorithm service, providing consensus results for a layer.
type HareService interface {
	Service
	GetHareMsgHandler() pubsub.GossipHandler
}

// TickProvider is an interface to a glopbal system clock that releases ticks on each layer.
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

func loadConfig(cmd *cobra.Command) (*config.Config, error) {
	conf, err := LoadConfigFromFile()
	if err != nil {
		return nil, fmt.Errorf("loading config from file: %w", err)
	}
	if err := cmdp.EnsureCLIFlags(cmd, conf); err != nil {
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

// WithConfig overvwrites default App config.
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

// App is the cli app singleton.
type App struct {
	*cobra.Command
	nodeID           types.NodeID
	Config           *config.Config
	grpcAPIService   *grpcserver.Server
	jsonAPIService   *grpcserver.JSONHTTPServer
	gatewaySvc       *grpcserver.GatewayService
	globalstateSvc   *grpcserver.GlobalStateService
	txService        *grpcserver.TransactionService
	syncer           *syncer.Syncer
	proposalListener *proposals.Handler
	proposalBuilder  *miner.ProposalBuilder
	mesh             *mesh.Mesh
	clock            TickProvider
	hare             HareService
	postSetupMgr     *activation.PostSetupManager
	atxBuilder       *activation.Builder
	atxDb            *activation.DB
	proposalDB       *proposals.DB
	poetListener     *activation.PoetListener
	edSgn            *signing.EdSigner
	beaconProtocol   *beacon.ProtocolDriver
	closers          []interface{ Close() }
	log              log.Log
	svm              *svm.SVM
	conState         *txs.ConservativeState
	layerFetch       *layerfetcher.Logic
	ptimesync        *peersync.Sync
	tortoise         *tortoise.Tortoise

	host *p2p.Host

	loggers map[string]*zap.AtomicLevel
	term    chan struct{} // this channel is closed when closing services, goroutines should wait on this channel in order to terminate
	started chan struct{} // this channel is closed once the app has finished starting
}

func (app *App) introduction() {
	log.Info("Welcome to Spacemesh. Spacemesh full node is starting...")
}

type clockErrorDetails struct {
	Drift time.Duration
}

func (c *clockErrorDetails) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddDuration("drift", c.Drift)
	return nil
}

type clockError struct {
	err     error
	details clockErrorDetails
}

func (c *clockError) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddString("code", errcode.ErrClockDrift)
	encoder.AddString("errmsg", c.err.Error())
	if err := encoder.AddObject("details", &c.details); err != nil {
		return fmt.Errorf("add object: %w", err)
	}

	return nil
}

func (c *clockError) Error() string {
	return c.err.Error()
}

// Initialize sets up an exit signal, logging and checks the clock, returns error if clock is not in sync.
func (app *App) Initialize() (err error) {
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

	// ensure all data folders exist
	err = filesystem.ExistOrCreate(app.Config.DataDir())
	if err != nil {
		return fmt.Errorf("ensure folders exist: %w", err)
	}

	// exit gracefully - e.g. with app Cleanup on sig abort (ctrl-c)
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)

	// Goroutine that listens for Ctrl ^ C command
	// and triggers the quit app
	go func() {
		for range signalChan {
			app.log.Info("Received an interrupt, stopping services...\n")

			cmdp.Cancel()()
		}
	}()

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
		cmdp.Version, cmdp.Branch, cmdp.Commit, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

// Cleanup stops all app services.
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
	isFixedOracle bool,
	rolacle hare.Rolacle,
	layerSize uint32,
	poetClient activation.PoetProvingServiceClient,
	vrfSigner *signing.VRFSigner,
	layersPerEpoch uint32, clock TickProvider,
) error {
	app.nodeID = nodeID

	lg := app.log.Named(nodeID.ShortString()).WithFields(nodeID)
	types.SetLayersPerEpoch(app.Config.LayersPerEpoch)

	app.log = app.addLogger(AppLogger, lg)

	stateDBStore, err := database.NewLDBDatabase(filepath.Join(dbStorepath, "state"), 0, 0, app.addLogger(StateDbLogger, lg))
	if err != nil {
		return fmt.Errorf("create state DB: %w", err)
	}
	app.closers = append(app.closers, stateDBStore)

	idDBStore, err := database.NewLDBDatabase(filepath.Join(dbStorepath, "ids"), 0, 0, app.addLogger(StateDbLogger, lg))
	if err != nil {
		return fmt.Errorf("create IDs DB: %w", err)
	}
	app.closers = append(app.closers, idDBStore)

	store, err := database.NewLDBDatabase(filepath.Join(dbStorepath, "store"), 0, 0, app.addLogger(StoreLogger, lg))
	if err != nil {
		return fmt.Errorf("create store DB: %w", err)
	}
	app.closers = append(app.closers, store)

	sqlDB, err := sql.Open("file:" + filepath.Join(dbStorepath, "state.sql"))
	if err != nil {
		return fmt.Errorf("open sqlite db %w", err)
	}

	idStore := activation.NewIdentityStore(idDBStore)
	poetDb := activation.NewPoetDb(sqlDB, app.addLogger(PoetDbLogger, lg))
	validator := activation.NewValidator(poetDb, app.Config.POST)

	if err := os.MkdirAll(dbStorepath, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create %s: %w", dbStorepath, err)
	}

	mdb, err := mesh.NewPersistentMeshDB(sqlDB, app.addLogger(MeshDBLogger, lg))
	if err != nil {
		return fmt.Errorf("create mesh DB: %w", err)
	}

	appliedTxs, err := database.NewLDBDatabase(filepath.Join(dbStorepath, "appliedTxs"), 0, 0, lg.WithName("appliedTxs"))
	if err != nil {
		return fmt.Errorf("create applied txs DB: %w", err)
	}
	app.closers = append(app.closers, appliedTxs)
	state := svm.New(stateDBStore, appliedTxs, app.addLogger(SVMLogger, lg))

	app.conState = txs.NewConservativeState(state, sqlDB, app.addLogger(ConStateLogger, lg))

	goldenATXID := types.ATXID(types.HexToHash32(app.Config.GoldenATXID))
	if goldenATXID == *types.EmptyATXID {
		return errors.New("invalid golden atx id")
	}

	fetcherWrapped := &layerFetcher{}
	atxDB := activation.NewDB(sqlDB, fetcherWrapped, idStore, layersPerEpoch, goldenATXID, validator, app.addLogger(AtxDbLogger, lg))

	beaconProtocol := beacon.New(nodeID, app.host, atxDB, sgn, vrfSigner, sqlDB, clock,
		beacon.WithContext(ctx),
		beacon.WithConfig(app.Config.Beacon),
		beacon.WithLogger(app.addLogger(BeaconLogger, lg)))

	var trtl *tortoise.Tortoise

	processed, err := mdb.GetProcessedLayer()
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return fmt.Errorf("failed to load processed layer: %w", err)
	}
	verified, err := mdb.GetVerifiedLayer()
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return fmt.Errorf("failed to load verified layer: %w", err)
	}

	trtlCfg := app.Config.Tortoise
	trtlCfg.LayerSize = layerSize
	trtlCfg.BadBeaconVoteDelayLayers = app.Config.LayersPerEpoch
	trtlCfg.MeshProcessed = processed
	trtlCfg.MeshVerified = verified

	var updater blockValidityUpdater
	trtl = tortoise.New(mdb, atxDB, beaconProtocol, &updater,
		tortoise.WithContext(ctx),
		tortoise.WithLogger(app.addLogger(TrtlLogger, lg)),
		tortoise.WithConfig(trtlCfg),
	)

	var msh *mesh.Mesh
	if mdb.PersistentData() {
		msh = mesh.NewRecoveredMesh(mdb, atxDB, trtl, app.conState, app.addLogger(MeshLogger, lg))
	} else {
		msh = mesh.NewMesh(mdb, atxDB, trtl, app.conState, app.addLogger(MeshLogger, lg))
		if err := state.SetupGenesis(app.Config.Genesis); err != nil {
			return fmt.Errorf("setup genesis: %w", err)
		}
	}
	updater.Mesh = msh

	proposalDB, err := proposals.NewProposalDB(sqlDB, app.addLogger(ProposalDBLogger, lg))
	if err != nil {
		return fmt.Errorf("create proposal DB: %w", err)
	}
	app.proposalDB = proposalDB

	// we can't have an epoch offset which is greater/equal than the number of layers in an epoch

	if app.Config.HareEligibility.EpochOffset >= app.Config.BaseConfig.LayersPerEpoch {
		return fmt.Errorf("epoch offset cannot be greater than or equal to the number of layers per epoch. epoch_offset: %d. layers_per_epoch: %d",
			app.Config.HareEligibility.EpochOffset, app.Config.BaseConfig.LayersPerEpoch)
	}

	proposalListener := proposals.NewHandler(fetcherWrapped, beaconProtocol, atxDB, msh, proposalDB,
		proposals.WithLogger(app.addLogger(ProposalListenerLogger, lg)),
		proposals.WithLayerPerEpoch(layersPerEpoch),
		proposals.WithLayerSize(layerSize),
		proposals.WithGoldenATXID(goldenATXID),
		proposals.WithMaxExceptions(trtlCfg.MaxExceptions))

	blockHandller := blocks.NewHandler(fetcherWrapped, msh,
		blocks.WithLogger(app.addLogger(BlockHandlerLogger, lg)))

	txHandler := txs.NewTxHandler(app.conState, app.addLogger(TxHandlerLogger, lg))

	dbStores := fetch.LocalDataSource{
		fetch.BallotDB:   msh.Ballots(),
		fetch.BlockDB:    msh.Blocks(),
		fetch.ProposalDB: proposalDB,
		fetch.ATXDB:      atxDB.ATXs(),
		fetch.TXDB:       app.conState.Transactions(),
		fetch.POETDB:     poetDb.PoETs(),
	}
	dataHanders := layerfetcher.DataHandlers{
		ATX:      atxDB,
		Block:    blockHandller,
		Ballot:   proposalListener,
		Proposal: proposalListener,
		TX:       txHandler,
	}
	layerFetch := layerfetcher.NewLogic(ctx, app.Config.FETCH, poetDb, atxDB, msh, app.host, dataHanders, dbStores, app.addLogger(LayerFetcher, lg))
	fetcherWrapped.Fetcher = layerFetch

	patrol := layerpatrol.New()
	syncerConf := syncer.Configuration{
		SyncInterval: time.Duration(app.Config.SyncInterval) * time.Second,
		AlwaysListen: app.Config.AlwaysListen,
	}
	newSyncer := syncer.NewSyncer(ctx, syncerConf, clock, beaconProtocol, msh, layerFetch, patrol, app.addLogger(SyncLogger, lg))
	// TODO(dshulyak) this needs to be improved, but dependency graph is a bit complicated
	beaconProtocol.SetSyncState(newSyncer)

	// TODO: we should probably decouple the apptest and the node (and duplicate as necessary) (#1926)
	var hOracle hare.Rolacle
	if isFixedOracle {
		// fixed rolacle, take the provided rolacle
		hOracle = rolacle
	} else {
		// regular oracle, build and use it
		hOracle = eligibility.New(beaconProtocol, atxDB, mdb, signing.VRFVerify, vrfSigner, app.Config.LayersPerEpoch, app.Config.HareEligibility, app.addLogger(HareOracleLogger, lg))
		// TODO: genesisMinerWeight is set to app.Config.SpaceToCommit, because PoET ticks are currently hardcoded to 1
	}

	blockGen := blocks.NewGenerator(atxDB, msh, app.conState, blocks.WithConfig(app.Config.REWARD), blocks.WithGeneratorLogger(app.addLogger(BlockGenLogger, lg)))
	rabbit := app.HareFactory(ctx, sgn, blockGen, nodeID, patrol, newSyncer, msh, proposalDB, beaconProtocol, fetcherWrapped, hOracle, idStore, clock, lg)

	proposalBuilder := miner.NewProposalBuilder(
		ctx,
		clock.Subscribe(),
		sgn,
		vrfSigner,
		sqlDB,
		atxDB,
		app.host,
		trtl,
		beaconProtocol,
		newSyncer,
		app.conState,
		miner.WithMinerID(nodeID),
		miner.WithTxsPerProposal(app.Config.TxsPerBlock),
		miner.WithLayerSize(layerSize),
		miner.WithLayerPerEpoch(layersPerEpoch),
		miner.WithLogger(app.addLogger(ProposalBuilderLogger, lg)))

	poetListener := activation.NewPoetListener(poetDb, app.addLogger(PoetListenerLogger, lg))

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
	atxBuilder := activation.NewBuilder(builderConfig, nodeID, sgn, atxDB, app.host, nipostBuilder,
		postSetupMgr, clock, newSyncer, store, app.addLogger("atxBuilder", lg), activation.WithContext(ctx),
	)

	syncHandler := func(_ context.Context, _ p2p.Peer, _ []byte) pubsub.ValidationResult {
		if newSyncer.ListenToGossip() {
			return pubsub.ValidationAccept
		}
		return pubsub.ValidationIgnore
	}

	app.host.Register(weakcoin.GossipProtocol, pubsub.ChainGossipHandler(syncHandler, beaconProtocol.HandleWeakCoinProposal))
	app.host.Register(beacon.ProposalProtocol,
		pubsub.ChainGossipHandler(syncHandler, beaconProtocol.HandleProposal))
	app.host.Register(beacon.FirstVotesProtocol,
		pubsub.ChainGossipHandler(syncHandler, beaconProtocol.HandleFirstVotes))
	app.host.Register(beacon.FollowingVotesProtocol,
		pubsub.ChainGossipHandler(syncHandler, beaconProtocol.HandleFollowingVotes))
	app.host.Register(proposals.NewProposalProtocol, pubsub.ChainGossipHandler(syncHandler, proposalListener.HandleProposal))
	app.host.Register(activation.AtxProtocol, pubsub.ChainGossipHandler(syncHandler, atxDB.HandleGossipAtx))
	app.host.Register(txs.IncomingTxProtocol, pubsub.ChainGossipHandler(syncHandler, txHandler.HandleGossipTransaction))
	app.host.Register(activation.PoetProofProtocol, poetListener.HandlePoetProofMessage)
	hareGossipHandler := rabbit.GetHareMsgHandler()
	if hareGossipHandler != nil {
		app.host.Register(hare.ProtoName, pubsub.ChainGossipHandler(syncHandler, rabbit.GetHareMsgHandler()))
	}

	app.proposalBuilder = proposalBuilder
	app.proposalListener = proposalListener
	app.mesh = msh
	app.syncer = newSyncer
	app.clock = clock
	app.svm = state
	app.hare = rabbit
	app.poetListener = poetListener
	app.atxBuilder = atxBuilder
	app.postSetupMgr = postSetupMgr
	app.atxDb = atxDB
	app.layerFetch = layerFetch
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

// HareFactory returns a hare consensus algorithm according to the parameters in app.Config.Hare.SuperHare.
func (app *App) HareFactory(
	ctx context.Context,
	sgn hare.Signer,
	blockGen *blocks.Generator,
	nodeID types.NodeID,
	patrol *layerpatrol.LayerPatrol,
	syncer system.SyncStateProvider,
	msh *mesh.Mesh,
	proposalDB *proposals.DB,
	beacons system.BeaconGetter,
	pFetcher system.ProposalFetcher,
	hOracle hare.Rolacle,
	idStore *activation.IdentityStore,
	clock TickProvider,
	lg log.Log,
) HareService {
	if app.Config.HARE.SuperHare {
		hr := turbohare.New(ctx, app.Config.HARE, msh, proposalDB, blockGen, clock.Subscribe(), app.addLogger(HareLogger, lg))
		return hr
	}

	ha := hare.New(
		app.Config.HARE,
		app.host.ID(),
		app.host,
		sgn,
		nodeID,
		blockGen,
		syncer,
		msh,
		proposalDB,
		beacons,
		pFetcher,
		hOracle,
		patrol,
		uint16(app.Config.LayersPerEpoch),
		idStore,
		hOracle,
		clock,
		app.addLogger(HareLogger, lg))
	return ha
}

func (app *App) startServices(ctx context.Context) error {
	app.layerFetch.Start()
	go app.startSyncer(ctx)
	app.beaconProtocol.Start(ctx)

	if err := app.hare.Start(ctx); err != nil {
		return fmt.Errorf("cannot start hare: %w", err)
	}
	if err := app.proposalBuilder.Start(ctx); err != nil {
		return fmt.Errorf("cannot start block producer: %w", err)
	}

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
	services := []grpcserver.ServiceAPI{}
	registerService := func(svc grpcserver.ServiceAPI) {
		if app.grpcAPIService == nil {
			app.grpcAPIService = grpcserver.NewServerWithInterface(apiConf.GrpcServerPort, apiConf.GrpcServerInterface)
		}
		services = append(services, svc)
		svc.RegisterService(app.grpcAPIService)
	}

	// Register the requested services one by one
	if apiConf.StartDebugService {
		registerService(grpcserver.NewDebugService(app.conState, app.host))
	}
	if apiConf.StartGatewayService {
		registerService(grpcserver.NewGatewayService(app.host))
	}
	if apiConf.StartGlobalStateService {
		registerService(grpcserver.NewGlobalStateService(app.mesh, app.conState))
	}
	if apiConf.StartMeshService {
		registerService(grpcserver.NewMeshService(app.mesh, app.conState, app.clock, app.Config.LayersPerEpoch, app.Config.P2P.NetworkID, layerDuration, app.Config.LayerAvgSize, app.Config.TxsPerBlock))
	}
	if apiConf.StartNodeService {
		nodeService := grpcserver.NewNodeService(app.host, app.mesh, app.clock, app.syncer, app.atxBuilder)
		registerService(nodeService)
		app.closers = append(app.closers, nodeService)
	}
	if apiConf.StartSmesherService {
		registerService(grpcserver.NewSmesherService(app.postSetupMgr, app.atxBuilder))
	}
	if apiConf.StartTransactionService {
		registerService(grpcserver.NewTransactionService(app.host, app.mesh, app.conState, app.syncer))
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
		app.jsonAPIService = grpcserver.NewJSONHTTPServer(apiConf.JSONServerPort)
		app.jsonAPIService.StartService(ctx, services...)
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

	if app.layerFetch != nil {
		app.log.Info("closing layerFetch")
		app.layerFetch.Close()
	}

	if app.syncer != nil {
		app.log.Info("closing sync")
		app.syncer.Close()
	}

	if app.proposalDB != nil {
		app.log.Info("closing proposal db")
	}

	if app.mesh != nil {
		app.log.Info("closing mesh")
		app.mesh.Close()
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

	events.CloseEventReporter()

	// Close all databases.
	for _, closer := range app.closers {
		if closer != nil {
			closer.Close()
		}
	}
}

// LoadOrCreateEdSigner either loads a previously created ed identity for the node or creates a new one if not exists.
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
func (app *App) Start() error {
	// we use the main app context
	ctx := cmdp.Ctx()
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

	lg.Info("initializing p2p services")

	cfg := app.Config.P2P
	cfg.DataDir = filepath.Join(app.Config.DataDir(), "p2p")
	p2plog := app.addLogger(P2PLogger, lg)
	// if addLogger won't add a level we will use a default 0 (info).
	cfg.LogLevel = app.getLevel(P2PLogger)
	app.host, err = p2p.New(ctx, p2plog, cfg,
		p2p.WithNodeReporter(events.ReportNodeStatusUpdate),
	)
	if err != nil {
		return fmt.Errorf("failed to initialize p2p host: %w", err)
	}

	if err = app.initServices(ctx,
		nodeID,
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
			app.host.ID().String(), strconv.Itoa(int(app.Config.P2P.NetworkID)))
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
			// if nil node was already stopped
			if syncErr != nil {
				cmdp.Cancel()()
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

type layerFetcher struct {
	system.Fetcher
}

type blockValidityUpdater struct {
	*mesh.Mesh
}

func decodeLoggers(cfg config.LoggerConfig) (map[string]string, error) {
	rst := map[string]string{}
	if err := mapstructure.Decode(cfg, &rst); err != nil {
		return nil, fmt.Errorf("mapstructure decode: %w", err)
	}
	return rst, nil
}
