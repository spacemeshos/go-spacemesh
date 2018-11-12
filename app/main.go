package app

import (
	"fmt"
	"os"
	"os/signal"
	"runtime"

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
	grpcAPIService   *api.SpaceMeshGrpcService
	jsonAPIService   *api.JSONHTTPServer
}

// EntryPointCreated channel is used to announce that the main App instance was created
// mainly used for testing now.
var EntryPointCreated = make(chan bool, 1)

var (
	// App is main app entry point.
	// It provides access the local identity and other top-level modules.
	App *SpacemeshApp

	// ExitApp is a channel used to signal the app to gracefully exit.
	ExitApp = make(chan bool, 1)

	// Version is the app's semantic version. Designed to be overwritten by make.
	Version = "0.0.1"

	// Branch is the git branch used to build the App. Designed to be overwritten by make.
	Branch = ""

	// Commit is the git commit used to build the app. Designed to be overwritten by make.
	Commit = ""
)

// ParseConfig unmarshal config file into struct
func ParseConfig() (*cfg.Config, error) {
	conf := cfg.DefaultConfig()

	err := viper.Unmarshal(&conf)

	if err != nil {
		log.Error("Failed to parse config\n")
		return nil, err
	}

	return &conf, nil
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
			ExitApp <- true
		}
	}()

	fileLocation := viper.GetString("config")

	// read in default config if passed as param using viper
	if err := cfg.LoadConfig(fileLocation); err != nil {
		log.Error(fmt.Sprintf("couldn't load config file at location: %s \n error: %v",
			fileLocation, err))
		return err
	}

	// parse the config file based on flags et al
	app.Config, err = ParseConfig()

	if err != nil {
		log.Error(fmt.Sprintf("couldn't parse the config toml file %v", err))
	}

	//app.setupLogging(ctx.Bool("debug"))

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

	// add any other cleanup tasks here....
	log.Info("App cleanup completed\n\n")

	return nil
}

func (app *SpacemeshApp) startSpacemesh(cmd *cobra.Command, args []string) {
	log.Info("Starting Spacemesh")

	// start p2p services
	log.Info("Initializing P2P services")
	swarm, err := p2p.New(app.Config.P2P)
	if err != nil {
		log.Error("Error starting p2p services, err: %v", err)
		panic("Error starting p2p services")
	}
	err = swarm.Start()

	if err != nil {
		log.Error("Error starting p2p services, err: %v", err)
		panic("Error starting p2p services")
	}


	app.P2P = swarm
	app.NodeInitCallback <- true

	apiConf := app.Config.API

	// todo: if there's no loaded account - do the new account interactive flow here

	// todo: if node has no loaded coin-base account then set the node coinbase to first account

	// todo: if node has a locked coinbase account then prompt for account passphrase to unlock it

	// todo: if node has no POS then start POS creation flow here unless user doesn't want to be a validator via cli

	// todo: start node consensus protocol here only after we have an unlocked account

	// start api servers
	if apiConf.StartGrpcServer || apiConf.StartJSONServer {
		// start grpc if specified or if json rpc specified
		app.grpcAPIService = api.NewGrpcService()
		app.grpcAPIService.StartService(nil)
	}

	if apiConf.StartJSONServer {
		app.jsonAPIService = api.NewJSONHTTPServer()
		app.jsonAPIService.StartService(nil)
	}

	log.Info("App started.")

	// app blocks until it receives a signal to exit
	// this signal may come from the node or from sig-abort (ctrl-c)
	<-ExitApp
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
