package app

import (
	"context"
	"fmt"
	"github.com/seehuhn/mt19937"
	"github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/consensus"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/hare"
	hareConfig "github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/metrics"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/oracle"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/state"
	"github.com/spacemeshos/go-spacemesh/sync"
	"github.com/spf13/pflag"
	"math/rand"
	"os"
	"os/signal"
	"reflect"
	"runtime"
	"time"

	"github.com/spacemeshos/go-spacemesh/accounts"
	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/app/cmd"
	cfg "github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// SpacemeshApp is the cli app singleton
type SpacemeshApp struct {
	*cobra.Command
	P2P              p2p.Service
	Config           *cfg.Config
	NodeInitCallback chan bool
	grpcAPIService   *api.SpacemeshGrpcService
	jsonAPIService   *api.JSONHTTPServer

	blockListener    *sync.BlockListener
	db               database.Database
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

// EntryPointCreated channel is used to announce that the main App instance was created
// mainly used for testing now.
var EntryPointCreated = make(chan bool, 1)

var (
	// App is main app entry point.
	// It provides access the local identity and other top-level modules.
	App *SpacemeshApp

	// Version is the app's semantic version. Designed to be overwritten by make.
	Version = "0.0.1"

	// Branch is the git branch used to build the App. Designed to be overwritten by make.
	Branch = ""

	// Commit is the git commit used to build the app. Designed to be overwritten by make.
	Commit = ""
	// ctx    cancel used to signal the app to gracefully exit.
	Ctx, Cancel = context.WithCancel(context.Background())
)

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

func EnsureCLIFlags(cmd *cobra.Command, appcfg *cfg.Config) {

	assignFields := func(p reflect.Type, elem reflect.Value, name string) {
		for i := 0; i < p.NumField(); i++ {
			if p.Field(i).Tag.Get("mapstructure") == name {
				elem.Field(i).Set(reflect.ValueOf(viper.Get(name)))
				return
			}
		}
	}
	// this is ugly but we have to do this because viper can't handle nested structs when deserialize
	cmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		if f.Changed {
			name := f.Name

			ff := reflect.TypeOf(appcfg.BaseConfig)
			elem := reflect.ValueOf(&appcfg.BaseConfig).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(*appcfg)
			elem = reflect.ValueOf(&appcfg).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(appcfg.API)
			elem = reflect.ValueOf(&appcfg.API).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(appcfg.P2P)
			elem = reflect.ValueOf(&appcfg.P2P).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(appcfg.P2P.SwarmConfig)
			elem = reflect.ValueOf(&appcfg.P2P.SwarmConfig).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(appcfg.P2P.TimeConfig)
			elem = reflect.ValueOf(&appcfg.P2P.TimeConfig).Elem()
			assignFields(ff, elem, name)

			ff = reflect.TypeOf(appcfg.CONSENSUS)
			elem = reflect.ValueOf(&appcfg.CONSENSUS).Elem()
			assignFields(ff, elem, name)
		}
	})
}

// NewSpacemeshApp creates an instance of the spacemesh app
func newSpacemeshApp() *SpacemeshApp {

	node := &SpacemeshApp{
		Command:          cmd.RootCmd,
		NodeInitCallback: make(chan bool, 1),
	}
	cmd.RootCmd.Version = Version
	cmd.RootCmd.PreRunE = node.before
	cmd.RootCmd.Run = node.startSpacemesh
	cmd.RootCmd.PostRunE = node.cleanup

	return node

}

func (app *SpacemeshApp) introduction() {
	log.Info("Welcome to Spacemesh. Spacemesh full node is starting...")
}

// this is what he wants to execute before app starts
// this is my persistent pre run that involves parsing the
// toml config file
func (app *SpacemeshApp) before(cmd *cobra.Command, args []string) (err error) {

	// exit gracefully - e.g. with app cleanup on sig abort (ctrl-c)
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)

	// Goroutine that listens for Crtl ^ C command
	// and triggers the quit app
	go func() {
		for range signalChan {
			log.Info("Received an interrupt, stopping services...\n")
			Cancel()
		}
	}()

	app.introduction()

	// parse the config file based on flags et al
	err = app.ParseConfig()

	if err != nil {
		log.Error(fmt.Sprintf("couldn't parse the config %v", err))
	}

	// ensure cli flags are higher priority than config file
	EnsureCLIFlags(cmd, app.Config)

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
		log.DebugMode(true)
		log.JSONLog(true)
	}

	// setup logging early
	dataDir, err := filesystem.GetSpacemeshDataDirectoryPath()
	if err != nil {
		fmt.Printf("Failed to setup spacemesh data dir")
		log.Error("Failed to setup spacemesh data dir")
		panic(err)
	}

	// app-level logging
	log.InitSpacemeshLoggingSystem(dataDir, "spacemesh.log")

	log.Info("%s", app.getAppInfo())
}

func (app *SpacemeshApp) getAppInfo() string {
	return fmt.Sprintf("App version: %s. Git: %s - %s . Go Version: %s. OS: %s-%s ",
		Version, Branch, Commit, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

// Post Execute tasks
func (app *SpacemeshApp) cleanup(cmd *cobra.Command, args []string) (err error) {
	log.Info("App cleanup starting...")

	if app.jsonAPIService != nil {
		log.Info("Stopping JSON service api...")
		app.jsonAPIService.StopService()
	}

	if app.grpcAPIService != nil {
		log.Info("Stopping GRPC service ...")
		app.grpcAPIService.StopService()
	}

	app.stopServices()

	// add any other cleanup tasks here....
	log.Info("App cleanup completed\n\n")

	return nil
}

func (app *SpacemeshApp) setupGenesis(cfg *config.GenesisConfig) {
	for id, acc := range cfg.InitialAccounts {
		app.state.CreateAccount(id)
		app.state.AddBalance(id, acc.Balance)
		app.state.SetNonce(id, acc.Nonce)
	}

	app.state.Commit(false)
	app.mesh.AddBlock(consensus.CreateGenesisBlock())
}

func (app *SpacemeshApp) setupTestFeatures() {
	// NOTE: any test-related feature enabling should happen here.
	api.ApproveAPIGossipMessages(Ctx, app.P2P)
}

func (app *SpacemeshApp) initServices(instanceName string, swarm server.Service, dbStorepath string, sgn hare.Signing, blockOracle oracle.BlockOracle, hareOracle hare.Rolacle) error {

	//todo: should we add all components to a single struct?
	lg := log.New("shmekel_"+instanceName, "", "")
	db, err := database.NewLDBDatabase(dbStorepath, 0, 0)
	if err != nil {
		return err
	}
	st, err := state.New(common.Hash{}, state.NewDatabase(db)) //todo: we probably should load DB with latest hash
	if err != nil {
		return err
	}
	rng := rand.New(mt19937.New())
	processor := state.NewTransactionProcessor(rng, st, lg)

	//trtl := consensus.NewTortoise(50, 100)
	trtl := consensus.NewAlgorithm(consensus.NewNinjaTortoise(50))
	mesh := mesh.NewMesh(db, db, db, trtl, processor, lg) //todo: what to do with the logger?

	coinToss := consensus.WeakCoin{}
	clock := timesync.NewTicker(timesync.RealClock{}, 5*time.Second, time.Now())

	blockListener := sync.NewBlockListener(swarm, blockOracle, mesh, 1*time.Second, 1, clock, lg)

	ha := hare.New(hareConfig.DefaultConfig(), swarm, sgn, mesh, hareOracle, clock.Subscribe())

	blockProducer := miner.NewBlockBuilder(instanceName, swarm, clock.Subscribe(), coinToss, mesh, ha, blockOracle, lg)

	app.blockProducer = &blockProducer
	app.blockListener = blockListener
	app.mesh = mesh
	app.clock = clock
	app.state = st
	app.db = db
	app.hare = ha
	app.P2P = swarm

	return nil
}

func (app *SpacemeshApp) startServices() {
	app.blockListener.Start()
	err := app.hare.Start()
	if err != nil {
		panic("cannot start hare")
	}
	err = app.blockProducer.Start()
	if err != nil {
		panic("cannot start block producer")
	}
	app.clock.Start()
}

func (app *SpacemeshApp) stopServices() {
	if app != nil {

	}
	app.clock.Stop()
	err := app.blockProducer.Stop()
	if err != nil {
		log.Error("cannot stop block producer %v", err)
	}
	app.hare.Close() //todo: need to add this
	app.blockListener.Close()

	app.db.Close()

	if app.unregisterOracle != nil {
		app.unregisterOracle()
	}
}

func (app *SpacemeshApp) startSpacemesh(cmd *cobra.Command, args []string) {
	log.Info("Starting Spacemesh")

	// start p2p services
	log.Info("Initializing P2P services")
	swarm, err := p2p.New(Ctx, app.Config.P2P)
	if err != nil {
		log.Error("Error starting p2p services, err: %v", err)
		panic("Error starting p2p services")
	}

	// todo : register all protocols

	sgn := hare.NewMockSigning() //todo: shouldn't be any mock code here
	pub, _ := crypto.NewPublicKey(sgn.Verifier().Bytes())

	oracle.SetServerAddress(app.Config.OracleServer)
	oracleClient := oracle.NewOracleClientWithWorldID(app.Config.OracleServerWorldId)
	oracleClient.Register(true, pub.String()) // todo: configure no faulty nodes

	app.unregisterOracle = func() { oracleClient.Unregister(true, pub.String()) }

	bo := oracle.NewBlockOracleFromClient(oracleClient, int(app.Config.CONSENSUS.NodesPerLayer))
	hareOracle := oracle.NewHareOracleFromClient(oracleClient)

	apiConf := &app.Config.API

	err = app.initServices("x", swarm, "/tmp/", sgn, bo, hareOracle)
	app.setupGenesis(config.DefaultGenesisConfig()) //todo: this is for debug, setup with other config when we have it
	if app.Config.TestMode {
		app.setupTestFeatures()
	}

	if app.Config.CollectMetrics {
		metrics.StartCollectingMetrics(app.Config.MetricsPort)
	}

	if err != nil {
		panic("got error starting services : " + err.Error())
	}

	app.startServices()

	err = app.P2P.Start()

	if err != nil {
		log.Error("Error starting p2p services, err: %v", err)
		panic("Error starting p2p services")
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

	<-Ctx.Done()
	//return nil
}

// Main is the entry point for the Spacemesh console app - responsible for parsing and routing cli flags and commands.
// This is the root of all evil, called from Main.main().
func Main() {
	App = newSpacemeshApp()

	EntryPointCreated <- true

	if err := App.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
