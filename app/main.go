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
	"github.com/spacemeshos/go-spacemesh/p2p/timesync"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// SpacemeshApp is the cli app singleton
type SpacemeshApp struct {
	*cobra.Command
	Node             p2p.LocalNode
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
	// It provides access the local node and other top-level modules.
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
		fmt.Printf("Failed to parse config ")
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
	cmd.RootCmd.Run = node.startSpacemeshNode
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
		fmt.Printf("%v", err)
		return err
	}

	// parse the config file based on flags et al
	app.Config, err = ParseConfig()

	if err != nil {
		fmt.Printf("couldn't parse the config toml file %v", err)
		return err
	}

	//app.setupLogging(ctx.Bool("debug"))

	fmt.Printf("\n app loging checking config %v \n", app.Config)

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

	log.Info("\n\nSpacemesh app session starting... %s", app.getAppInfo())
}

func (app *SpacemeshApp) getAppInfo() string {
	return fmt.Sprintf("App version: %s. Git: %s - %s . Go Version: %s. OS: %s-%s ",
		Version, Branch, Commit, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

// Post Execute tasks
func (app *SpacemeshApp) cleanup(cmd *cobra.Command, args []string) (err error) {

	log.Debug("App cleanup starting...")

	if app.jsonAPIService != nil {
		log.Info("Stopping JSON service api...")
		app.jsonAPIService.StopService()
	}

	if app.grpcAPIService != nil {
		app.grpcAPIService.StopService()
	}

	// add any other cleanup tasks here....
	log.Debug("App cleanup completed\n\n")

	return nil
}

func (app *SpacemeshApp) startSpacemeshNode(cmd *cobra.Command, args []string) {
	log.Info("Starting local node...")

	port := app.Config.P2P.TCPPort
	address := fmt.Sprintf("0.0.0.0:%d", port)

	// start a new node passing the app-wide node config values and persist it to store
	// so future sessions use the same local node id
	node, err := p2p.NewLocalNode(address, app.Config.P2P, true)
	if err != nil {
		fmt.Printf("%v", err)
		//return err
	}

	node.NotifyOnShutdown(ExitApp)
	app.Node = node
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
