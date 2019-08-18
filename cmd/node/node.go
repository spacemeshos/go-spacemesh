package node

import (
	"fmt"
	"github.com/seehuhn/mt19937"
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/amcl"
	"github.com/spacemeshos/go-spacemesh/amcl/BLS381"
	apiCfg "github.com/spacemeshos/go-spacemesh/api/config"
	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/consensus"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/hare/eligibility"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/metrics"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/nipst"
	"github.com/spacemeshos/go-spacemesh/oracle"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/state"
	"github.com/spacemeshos/go-spacemesh/sync"
	"github.com/spacemeshos/go-spacemesh/tortoise"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/spacemeshos/go-spacemesh/version"
	"github.com/spacemeshos/post/shared"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"time"

	"github.com/spacemeshos/go-spacemesh/accounts"
	"github.com/spacemeshos/go-spacemesh/api"
	cfg "github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/timesync"
	timeCfg "github.com/spacemeshos/go-spacemesh/timesync/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

import _ "net/http/pprof"

const edKeyFileName = "key.bin"

// VersionCmd returns the current version of spacemesh
var Cmd = &cobra.Command{
	Use:   "node",
	Short: "start node",
	Run: func(cmd *cobra.Command, args []string) {
		app := NewSpacemeshApp()
		defer app.Cleanup(cmd, args)

		app.Initialize(cmd, args)
		app.Start(cmd, args)
	},
}

// VersionCmd returns the current version of spacemesh
var VersionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version info",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(version.Version)
	},
}

func init() {
	cmdp.AddCommands(Cmd)
	Cmd.AddCommand(VersionCmd)
}

// SpacemeshApp is the cli app singleton
type SpacemeshApp struct {
	*cobra.Command
	nodeId           types.NodeId
	P2P              p2p.Service
	Config           *cfg.Config
	NodeInitCallback chan bool
	grpcAPIService   *api.SpacemeshGrpcService
	jsonAPIService   *api.JSONHTTPServer
	syncer           *sync.Syncer
	blockListener    *sync.BlockListener
	state            *state.StateDB
	blockProducer    *miner.BlockBuilder
	oracle           *oracle.MinerBlockOracle
	txProcessor      *state.TransactionProcessor
	mesh             *mesh.Mesh
	clock            *timesync.Ticker
	hare             *hare.Hare
	atxBuilder       *activation.Builder
	poetListener     *activation.PoetListener
	edSgn            *signing.EdSigner
	log              log.Log
}

// ParseConfig unmarshal config file into struct
func (app *SpacemeshApp) ParseConfig() (err error) {

	fileLocation := viper.GetString("config")
	vip := viper.New()
	// read in default config if passed as param using viper
	if err = cfg.LoadConfig(fileLocation, vip); err != nil {
		log.Error(fmt.Sprintf("couldn't load config file at location: %s swithing to defaults \n error: %v.",
			fileLocation, err))
		//return err
	}

	conf := cfg.DefaultConfig()
	// load config if it was loaded to our viper
	err = vip.Unmarshal(&conf)
	if err != nil {
		log.Error("Failed to parse config\n")
		return err
	}

	app.Config = &conf

	return nil
}

// NewSpacemeshApp creates an instance of the spacemesh app
func NewSpacemeshApp() *SpacemeshApp {

	defaultConfig := cfg.DefaultConfig()
	node := &SpacemeshApp{
		Config:           &defaultConfig,
		NodeInitCallback: make(chan bool, 1),
	}

	return node

}

func (app *SpacemeshApp) introduction() {
	log.Info("Welcome to Spacemesh. Spacemesh full node is starting...")
}

// this is what he wants to execute Initialize app starts
// this is my persistent pre run that involves parsing the
// toml config file
func (app *SpacemeshApp) Initialize(cmd *cobra.Command, args []string) (err error) {

	// exit gracefully - e.g. with app Cleanup on sig abort (ctrl-c)
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)

	// Goroutine that listens for Crtl ^ C command
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
	cmdp.EnsureCLIFlags(cmd, app.Config)

	// override default config in timesync since timesync is using TimeCongigValues
	timeCfg.TimeConfigValues = app.Config.TIME

	app.setupLogging()

	app.introduction()

	// todo: add misc app setup here (metrics, debug, etc....)

	drift, err := timesync.CheckSystemClockDrift()
	if err != nil {
		return err
	}

	log.Info("System clock synchronized with ntp. drift: %s", drift)

	// ensure all data folders exist
	filesystem.EnsureSpacemeshDataDirectories()

	// load all accounts from store
	accounts.LoadAllAccounts()

	// todo: set coinbase account (and unlock it) based on flags

	return nil
}

// setupLogging configured the app logging system.
func (app *SpacemeshApp) setupLogging() {

	if app.Config.TestMode {
		log.JSONLog(true)
	}

	// setup logging early
	dataDir, err := filesystem.GetSpacemeshDataDirectoryPath()
	if err != nil {
		fmt.Printf("Failed to setup spacemesh data dir")
		log.Panic("Failed to setup spacemesh data dir", err)
	}

	// app-level logging
	log.InitSpacemeshLoggingSystem(dataDir, "spacemesh.log")

	log.Info("%s", app.getAppInfo())
}

func (app *SpacemeshApp) getAppInfo() string {
	return fmt.Sprintf("App version: %s. Git: %s - %s . Go Version: %s. OS: %s-%s ",
		cmdp.Version, cmdp.Branch, cmdp.Commit, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

// Post Execute tasks
func (app *SpacemeshApp) Cleanup(cmd *cobra.Command, args []string) (err error) {
	log.Info("App Cleanup starting...")

	if app.jsonAPIService != nil {
		log.Info("Stopping JSON service api...")
		app.jsonAPIService.StopService()
	}

	if app.grpcAPIService != nil {
		log.Info("Stopping GRPC service ...")
		app.grpcAPIService.StopService()
	}

	app.stopServices()

	// add any other Cleanup tasks here....
	log.Info("App Cleanup completed\n\n")

	return nil
}

func (app *SpacemeshApp) setupGenesis() {
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
		bytes := common.FromHex(id)
		if len(bytes) == 0 {
			//todo: should we panic here?
			log.Error("cannot read config entry for :%s", id)
			continue
		}

		addr := address.BytesToAddress(bytes)
		app.state.CreateAccount(addr)
		app.state.AddBalance(addr, acc.Balance)
		app.state.SetNonce(addr, acc.Nonce)
		app.log.Info("Genesis account created: %s, Balance: %s", id, acc.Balance.Uint64())
	}

	app.state.Commit(false)
	app.mesh.AddBlock(&mesh.GenesisBlock)
}

func (app *SpacemeshApp) setupTestFeatures() {
	// NOTE: any test-related feature enabling should happen here.
	api.ApproveAPIGossipMessages(cmdp.Ctx, app.P2P)
}

func (app *SpacemeshApp) initServices(nodeID types.NodeId, swarm service.Service, dbStorepath string, sgn hare.Signer, isFixedOracle bool, rolacle hare.Rolacle, layerSize uint32, postClient nipst.PostProverClient, poetClient nipst.PoetProvingServiceClient, vrfSigner *BLS381.BlsSigner, layersPerEpoch uint16) error {

	app.nodeId = nodeID
	//todo: should we add all components to a single struct?

	name := nodeID.ShortString()

	lg := log.NewDefault(name).WithFields(log.String("nodeID", name))
	app.log = lg.WithName("app")

	postClient.SetLogger(lg.WithName("post"))

	db, err := database.NewLDBDatabase(dbStorepath, 0, 0, lg.WithName("stateDbStore"))
	if err != nil {
		return err
	}
	st, err := state.New(common.Hash{}, state.NewDatabase(db)) //todo: we probably should load DB with latest hash
	if err != nil {
		return err
	}
	rng := rand.New(mt19937.New())
	processor := state.NewTransactionProcessor(rng, st, app.Config.GAS, lg.WithName("state"))

	coinToss := consensus.WeakCoin{}
	gTime, err := time.Parse(time.RFC3339, app.Config.GenesisTime)
	if err != nil {
		return err
	}
	ld := time.Duration(app.Config.LayerDurationSec) * time.Second
	clock := timesync.NewTicker(timesync.RealClock{}, ld, gTime)

	atxdbstore, err := database.NewLDBDatabase(dbStorepath+"atx", 0, 0, lg.WithName("atxDbStore"))
	if err != nil {
		return err
	}

	poetDbStore, err := database.NewLDBDatabase(dbStorepath+"poet", 0, 0, lg.WithName("poetDbStore"))
	if err != nil {
		return err
	}

	//todo: put in config
	iddbstore, err := database.NewLDBDatabase(dbStorepath+"ids", 0, 0, lg.WithName("stateDbStore"))
	if err != nil {
		return err
	}

	store, err := database.NewLDBDatabase(dbStorepath+"store", 0, 0, lg.WithName("store"))
	if err != nil {
		return err
	}
	idStore := activation.NewIdentityStore(iddbstore)
	poetDb := activation.NewPoetDb(poetDbStore, lg.WithName("poetDb"))
	//todo: this is initialized twice, need to refactor
	validator := nipst.NewValidator(&app.Config.POST, poetDb)
	mdb, err := mesh.NewPersistentMeshDB(dbStorepath, lg.WithName("meshDb"))
	if err != nil {
		return err
	}
	atxdb := activation.NewActivationDb(atxdbstore, idStore, mdb, layersPerEpoch, validator, lg.WithName("atxDb"))
	beaconProvider := &oracle.EpochBeaconProvider{}
	eValidator := oracle.NewBlockEligibilityValidator(layerSize, uint32(app.Config.GenesisActiveSet), layersPerEpoch, atxdb, beaconProvider, BLS381.Verify2, lg.WithName("blkElgValidator"))

	trtl := tortoise.NewAlgorithm(int(layerSize), mdb, app.Config.Hdist, lg.WithName("trtl"))

	txpool := miner.NewTypesTransactionIdMemPool()
	atxpool := miner.NewTypesAtxIdMemPool()

	msh := mesh.NewMesh(mdb, atxdb, app.Config.REWARD, trtl, txpool, atxpool, processor, lg.WithName("mesh")) //todo: what to do with the logger?

	conf := sync.Configuration{Concurrency: 4, LayerSize: int(layerSize), LayersPerEpoch: layersPerEpoch, RequestTimeout: 2000 * time.Millisecond}

	syncer := sync.NewSync(swarm, msh, txpool, atxpool, processor, eValidator, poetDb, conf, clock.Subscribe(), clock.GetCurrentLayer(), lg.WithName("sync"))
	blockOracle := oracle.NewMinerBlockOracle(layerSize, uint32(app.Config.GenesisActiveSet), uint16(layersPerEpoch), atxdb, beaconProvider, vrfSigner, nodeID, syncer.IsSynced, lg.WithName("blockOracle"))

	// TODO: we should probably decouple the apptest and the node (and duplicate as necessary)
	var hOracle hare.Rolacle
	if isFixedOracle { // fixed rolacle, take the provided rolacle
		hOracle = rolacle
	} else { // regular oracle, build and use it
		beacon := eligibility.NewBeacon(trtl, app.Config.HareEligibility.ConfidenceParam)
		hOracle = eligibility.New(beacon, atxdb.CalcActiveSetSize, BLS381.Verify2, vrfSigner, uint16(app.Config.LayersPerEpoch), app.Config.GenesisActiveSet, trtl, app.Config.HareEligibility, lg.WithName("hareOracle"))
	}

	// a function to validate we know the blocks
	validationFunc := func(ids []types.BlockID) bool {
		for _, b := range ids {
			res, err := mdb.GetBlock(b)
			if err != nil {
				app.log.With().Error("failed to validate block", log.BlockId(uint64(b)))
				return false
			}
			if res == nil {
				app.log.With().Error("failed to validate block (BUG BUG BUG - GetBlock return err nil and res nil)", log.BlockId(uint64(b)))
				return false
			}

		}

		return true
	}
	ha := hare.New(app.Config.HARE, swarm, sgn, nodeID, validationFunc, syncer.IsSynced, msh, hOracle, uint16(app.Config.LayersPerEpoch), idStore, hOracle, clock.Subscribe(), lg.WithName("hare"))

	blockProducer := miner.NewBlockBuilder(nodeID, sgn, swarm, clock.Subscribe(), app.Config.Hdist, txpool, atxpool, coinToss, msh, ha, blockOracle, processor, atxdb, syncer, lg.WithName("blockBuilder"))
	blockListener := sync.NewBlockListener(swarm, syncer, 4, lg.WithName("blockListener"))

	poetListener := activation.NewPoetListener(swarm, poetDb, lg.WithName("poetListener"))

	nipstBuilder := nipst.NewNIPSTBuilder(
		common.Hex2Bytes(nodeID.Key), // TODO: use both keys in the nodeID
		app.Config.POST,
		postClient,
		poetClient,
		poetDb,
		lg.WithName("nipstBuilder"),
	)

	coinBase := address.HexToAddress(app.Config.CoinbaseAccount)

	if coinBase.Big().Uint64() == 0 && app.Config.StartMining {
		app.log.Panic("invalid Coinbase account")
	}
	atxBuilder := activation.NewBuilder(nodeID, coinBase, atxdb, swarm, msh, layersPerEpoch, nipstBuilder, clock.Subscribe(), syncer.IsSynced, store, lg.WithName("atxBuilder"))

	app.blockProducer = &blockProducer
	app.blockListener = blockListener
	app.mesh = msh
	app.syncer = syncer
	app.clock = clock
	app.state = st
	app.hare = ha
	app.P2P = swarm
	app.poetListener = poetListener
	app.atxBuilder = atxBuilder
	app.oracle = blockOracle
	return nil
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
		coinBase := address.HexToAddress(app.Config.CoinbaseAccount)
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
}

func (app *SpacemeshApp) stopServices() {

	log.Info("%v closing services ", app.nodeId.Key)

	if err := app.blockProducer.Close(); err != nil {
		log.Error("cannot stop block producer %v", err)
	}

	log.Info("%v closing clock", app.nodeId.Key)
	app.clock.Close()

	app.log.Info("closing PoET listener")
	app.poetListener.Close()

	app.log.Info("closing atx builder")
	app.atxBuilder.Stop()

	log.Info("%v closing blockListener", app.nodeId.Key)
	app.blockListener.Close()

	log.Info("%v closing Hare", app.nodeId.Key)
	app.hare.Close() //todo: need to add this

	log.Info("%v closing p2p", app.nodeId.Key)
	app.P2P.Shutdown()

	log.Info("%v closing mesh", app.nodeId.Key)
	app.mesh.Close()

}

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

func (app *SpacemeshApp) Start(cmd *cobra.Command, args []string) {
	log.Event().Info("Starting Spacemesh")
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

	if app.Config.CpuProfile != "" {
		log.Info("Starting cpu profile")
		f, err := os.Create(app.Config.CpuProfile)
		if err != nil {
			log.Error("could not create CPU profile: ", err)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Error("could not start CPU profile: ", err)
		}
		defer pprof.StopCPUProfile()
	}

	if app.Config.PprofHttpServer {
		log.Info("Starting pprof server")
		go func() {
			err := http.ListenAndServe(":6060", nil)
			if err != nil {
				log.Error("cannot start http server", err)
			}
		}()
	}

	// start p2p services
	log.Info("Initializing P2P services")
	swarm, err := p2p.New(cmdp.Ctx, app.Config.P2P)
	if err != nil {
		log.Error("Error starting p2p services, err: %v", err)
		log.Panic("Error starting p2p services")
	}

	// todo : register all protocols

	app.edSgn, err = app.LoadOrCreateEdSigner()
	if err != nil {
		log.Panic("Could not retrieve identity err=%v", err)
	}

	log.Info("connecting to POET on IP %v", app.Config.PoETServer)
	poetClient, err := nipst.NewRemoteRPCPoetClient(app.Config.PoETServer, 30*time.Second)
	if err != nil {
		log.Error("poet server not found on addr %v, err: %v", app.Config.PoETServer, err)
		return
	}

	postClient := nipst.NewPostClient(&app.Config.POST)

	rng := amcl.NewRAND()
	pub := app.edSgn.PublicKey().Bytes()
	rng.Seed(len(pub), app.edSgn.Sign(pub)) // assuming ed.private is random, the sig can be used as seed
	vrfPriv, vrfPub := BLS381.GenKeyPair(rng)
	vrfSigner := BLS381.NewBlsSigner(vrfPriv)
	nodeID := types.NodeId{Key: app.edSgn.PublicKey().String(), VRFPublicKey: vrfPub}

	apiConf := &app.Config.API

	dbStorepath := app.Config.DataDir

	err = app.initServices(nodeID, swarm, dbStorepath, app.edSgn, false, nil, uint32(app.Config.LayerAvgSize), postClient, poetClient, vrfSigner, uint16(app.Config.LayersPerEpoch))
	if err != nil {
		log.Error("cannot start services %v", err.Error())
		return
	}

	app.setupGenesis()

	if app.Config.TestMode {
		app.setupTestFeatures()
	}

	if app.Config.CollectMetrics {
		metrics.StartCollectingMetrics(app.Config.MetricsPort)
	}
	app.startServices()

	err = app.P2P.Start()

	if err != nil {
		log.Error("Error starting p2p services, err: %v", err)
		log.Panic("Error starting p2p services")
	}

	// todo: if there's no loaded account - do the new account interactive flow here
	// todo: if node has no loaded coin-base account then set the node coinbase to first account
	// todo: if node has a locked coinbase account then prompt for account passphrase to unlock it
	// todo: if node has no POS then start POS creation flow here unless user doesn't want to be a validator via cli
	// todo: start node consensus protocol here only after we have an unlocked account

	// start api servers
	if apiConf.StartGrpcServer || apiConf.StartJSONServer {
		// start grpc if specified or if json rpc specified
		app.grpcAPIService = api.NewGrpcService(app.P2P, app.state, app.mesh.TxProcessor, app.atxBuilder, app.oracle)
		app.grpcAPIService.StartService(nil)
	}

	if apiConf.StartJSONServer {
		app.jsonAPIService = api.NewJSONHTTPServer()
		app.jsonAPIService.StartService(nil)
	}

	log.Info("App started.")

	// app blocks until it receives a signal to exit
	// this signal may come from the node or from sig-abort (ctrl-c)
	app.NodeInitCallback <- true

	<-cmdp.Ctx.Done()
	//return nil
}
