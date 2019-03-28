package node

import (
	"fmt"
	"github.com/seehuhn/mt19937"
	apiCfg "github.com/spacemeshos/go-spacemesh/api/config"
	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/consensus"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/metrics"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/oracle"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/state"
	"github.com/spacemeshos/go-spacemesh/sync"
	"github.com/spacemeshos/go-spacemesh/version"
	"math/rand"

	"os"
	"os/signal"
	"runtime"
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
	instanceName     string
	P2P              p2p.Service
	Config           *cfg.Config
	NodeInitCallback chan bool
	grpcAPIService   *api.SpacemeshGrpcService
	jsonAPIService   *api.JSONHTTPServer
	syncer           *sync.Syncer
	blockListener    *sync.BlockListener
	state            *state.StateDB
	blockProducer    *miner.BlockBuilder
	mesh             *mesh.Mesh
	clock            *timesync.Ticker
	hare             *hare.Hare
	unregisterOracle func()
}

type MiningEnabler interface {
	MiningEligible() bool
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

	app.introduction()

	// parse the config file based on flags et al
	err = app.ParseConfig()

	if err != nil {
		log.Error(fmt.Sprintf("couldn't parse the config %v", err))
	}

	// ensure cli flags are higher priority than config file
	cmdp.EnsureCLIFlags(cmd, app.Config)

	// override default config in timesync since timesync is using TimeCongigValues
	timeCfg.TimeConfigValues = app.Config.TIME

	app.setupLogging()

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

func (app *SpacemeshApp) setupGenesis(cfg *apiCfg.GenesisConfig) {
	for id, acc := range cfg.InitialAccounts {
		app.state.CreateAccount(id)
		app.state.AddBalance(id, acc.Balance)
		app.state.SetNonce(id, acc.Nonce)
	}

	genesis := mesh.CreateGenesisBlock()
	app.state.Commit(false)
	app.mesh.AddBlock(genesis)
}

func (app *SpacemeshApp) setupTestFeatures() {
	// NOTE: any test-related feature enabling should happen here.
	api.ApproveAPIGossipMessages(cmdp.Ctx, app.P2P)
}

func (app *SpacemeshApp) initServices(instanceName string, swarm server.Service, dbStorepath string, sgn hare.Signing, blockOracle oracle.BlockOracle, hareOracle hare.Rolacle, layerSize int) error {
	app.instanceName = instanceName
	//todo: should we add all components to a single struct?

	name := instanceName
	if len(instanceName) > 5 {
		name = instanceName[len(instanceName)-5:]
	}

	lg := log.New("shmekel_"+name, "", "")

	db, err := database.NewLDBDatabase(dbStorepath, 0, 0)
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
	mdb := mesh.NewPersistentMeshDB(dbStorepath, lg.WithName("meshdb"))
	trtl := consensus.NewAlgorithm(consensus.NewNinjaTortoise(layerSize, mdb, lg.WithName("trtl")))
	msh := mesh.NewMesh(mdb, app.Config.REWARD, trtl, processor, lg.WithName("mesh")) //todo: what to do with the logger?

	conf := sync.Configuration{SyncInterval: 1 * time.Second, Concurrency: 4, LayerSize: int(layerSize), RequestTimeout: 100 * time.Millisecond}
	syncer := sync.NewSync(swarm, msh, blockOracle, conf, clock.Subscribe(), lg)

	ha := hare.New(app.Config.HARE, swarm, sgn, msh, hareOracle, clock.Subscribe(), lg.WithName("hare"))

	blockProducer := miner.NewBlockBuilder(instanceName, swarm, clock.Subscribe(), coinToss, msh, ha, blockOracle, lg.WithName("blockProducer"))
	blockListener := sync.NewBlockListener(swarm, blockOracle, msh, 2*time.Second, 4, lg.WithName("blockListener"))

	app.blockProducer = &blockProducer
	app.blockListener = blockListener
	app.mesh = msh
	app.syncer = syncer
	app.clock = clock
	app.state = st
	app.hare = ha
	app.P2P = swarm

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
	app.clock.Start()
}

func (app SpacemeshApp) stopServices() {

	log.Info("%v closing services ", app.instanceName)

	log.Info("%v closing clock", app.instanceName)
	app.clock.Close()

	log.Info("%v closing Hare", app.instanceName)
	app.hare.Close() //todo: need to add this

	log.Info("%v closing p2p", app.instanceName)
	app.P2P.Shutdown()

	if err := app.blockProducer.Close(); err != nil {
		log.Error("cannot stop block producer %v", err)
	}

	log.Info("%v closing blockListener", app.instanceName)
	app.blockListener.Close()

	log.Info("%v closing mesh", app.instanceName)
	app.mesh.Close()

	log.Info("%v closing sync", app.instanceName)
	app.syncer.Close()

	log.Info("unregister from oracle")
	if app.unregisterOracle != nil {
		app.unregisterOracle()
	}

}

func (app *SpacemeshApp) Start(cmd *cobra.Command, args []string) {
	log.Info("Starting Spacemesh")

	// start p2p services
	log.Info("Initializing P2P services")
	swarm, err := p2p.New(cmdp.Ctx, app.Config.P2P)
	if err != nil {
		log.Error("Error starting p2p services, err: %v", err)
		log.Panic("Error starting p2p services")
	}

	// todo : register all protocols

	sgn := hare.NewMockSigning() //todo: shouldn't be any mock code here
	pub, _ := crypto.NewPublicKey(sgn.Verifier().Bytes())

	oracle.SetServerAddress(app.Config.OracleServer)
	oracleClient := oracle.NewOracleClientWithWorldID(uint64(app.Config.OracleServerWorldId))
	oracleClient.Register(true, pub.String()) // todo: configure no faulty nodes

	app.unregisterOracle = func() { oracleClient.Unregister(true, pub.String()) }

	bo := oracle.NewBlockOracleFromClient(oracleClient, int(app.Config.CONSENSUS.NodesPerLayer))
	hareOracle := oracle.NewHareOracleFromClient(oracleClient)

	apiConf := &app.Config.API

	err = app.initServices("x", swarm, "/tmp/", sgn, bo, hareOracle, 50)
	if err != nil {
		log.Error("cannot start services %v", err.Error())
		return
	}
	app.setupGenesis(apiCfg.DefaultGenesisConfig()) //todo: this is for debug, setup with other config when we have it
	if app.Config.TestMode {
		app.setupTestFeatures()
	}

	if app.Config.CollectMetrics {
		metrics.StartCollectingMetrics(app.Config.MetricsPort)
	}

	if err != nil {
		log.Panic("got error starting services : " + err.Error())
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
		app.grpcAPIService = api.NewGrpcService(app.P2P, app.state)
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
