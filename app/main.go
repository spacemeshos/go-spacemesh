// Package app provides the cli app shell of a Spacemesh p2p node
package app

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/accounts"
	api "github.com/spacemeshos/go-spacemesh/api"
	apiconf "github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"gopkg.in/urfave/cli.v1"
	"gopkg.in/urfave/cli.v1/altsrc"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/spacemeshos/go-spacemesh/app/config"
	nodeparams "github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
	"github.com/spacemeshos/go-spacemesh/p2p/timesync"
)

// SpacemeshApp is the cli app singleton
type SpacemeshApp struct {
	*cli.App
	Node           p2p.LocalNode
	grpcAPIService *api.SpaceMeshGrpcService
	jsonAPIService *api.JSONHTTPServer
}

// App is main app entry point.
// It provides access the local node and other top-level modules.
var App *SpacemeshApp

var (
	appFlags = []cli.Flag{
		config.LoadConfigFileFlag,
		config.DataFolderPathFlag,

		// add all additional app flags here ...
	}
	nodeFlags = []cli.Flag{
		nodeparams.KSecurityFlag,
		nodeparams.LocalTCPPortFlag,
		nodeparams.NodeIDFlag,
		nodeparams.NetworkDialTimeout,
		nodeparams.NetworkConnKeepAlive,
		nodeparams.SwarmBootstrap,
		nodeparams.RoutingTableBucketSizdFlag,
		nodeparams.RoutingTableAlphaFlag,
		nodeparams.RandomConnectionsFlag,
		nodeparams.BootstrapNodesFlag,
		// add all additional node flags here ...
	}
	apiFlags = []cli.Flag{
		apiconf.StartGrpcAPIServerFlag,
		apiconf.GrpcServerPortFlag,
		apiconf.StartJSONApiServerFlag,
		apiconf.JSONServerPortFlag,
	}

	// ExitApp is a channel used to signal the app to gracefully exit.
	ExitApp = make(chan bool, 1)

	// Version is the app's semantic version. Designed to be overwritten by make.
	Version = "0.0.1"

	// Branch is the git branch used to build the App. Designed to be overwritten by make.
	Branch = ""

	// Commit is the git commit used to build the app. Designed to be overwritten by make.
	Commit = ""
)

// add toml config file support and sample toml file

func newSpacemeshApp() *SpacemeshApp {
	app := cli.NewApp()
	app.Name = filepath.Base(os.Args[0])
	app.Author = config.AppAuthor
	app.Email = config.AppAuthorEmail
	app.Version = Version
	if len(Commit) > 8 {
		app.Version += " " + Commit[:8]
	}
	app.Usage = config.AppUsage
	app.HideVersion = true
	app.Copyright = config.AppCopyrightNotice
	app.Commands = []cli.Command{
		config.NewVersionCommand(Version, Branch, Commit),
		// add all other commands here
	}

	app.Flags = append(app.Flags, appFlags...)
	app.Flags = append(app.Flags, nodeFlags...)
	app.Flags = append(app.Flags, apiFlags...)

	sort.Sort(cli.FlagsByName(app.Flags))

	sma := &SpacemeshApp{app, nil, nil, nil}

	// setup callbacks
	app.Before = sma.before
	app.Action = sma.startSpacemeshNode
	app.After = sma.cleanup

	return sma
}

// start the spacemesh node
func startSpacemeshNode(ctx *cli.Context) error {
	return App.startSpacemeshNode(ctx)
}

// setup app logging system
func (app *SpacemeshApp) setupLogging() {

	// setup logging early
	dataDir, err := filesystem.GetSpacemeshDataDirectoryPath()
	if err != nil {
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

func (app *SpacemeshApp) before(ctx *cli.Context) error {

	// exit gracefully - e.g. with app cleanup on sig abort (ctrl-c)
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)
	go func() {
		for range signalChan {
			log.Info("Received an interrupt, stopping services...\n")
			ExitApp <- true
		}
	}()

	if configPath := filesystem.GetCanonicalPath(config.ConfigValues.ConfigFilePath); len(configPath) > 1 {
		if filesystem.PathExists(configPath) {
			log.Info("Loading config file (path):", configPath)
			err := altsrc.InitInputSourceWithContext(ctx.App.Flags, func(context *cli.Context) (altsrc.InputSourceContext, error) {
				toml, err := altsrc.NewTomlSourceFromFile(configPath)
				return toml, err
			})(ctx)
			if err != nil {
				log.Error("Config file had an error:", err)
				return err
			}
		} else {
			log.Warning("Could not find config file using default values path:", configPath)
		}
	} else {
		log.Warning("No config file defined using default config")
	}

	app.setupLogging()

	// todo: add misc app setup here (metrics, debug, etc....)

	err := timesync.CheckSystemClockDrift()
	if err != nil {
		//todo: this shows the help output for some reason
		return err
	}

	// ensure all data folders exist
	filesystem.EnsureSpacemeshDataDirectories()

	// load all accounts from store
	accounts.LoadAllAccounts()

	// todo: set coinbase account (and unlock it) based on flags

	return nil
}

// Spacemesh app cleanup tasks
func (app *SpacemeshApp) cleanup(ctx *cli.Context) error {

	log.Info("App cleanup starting...")

	if app.jsonAPIService != nil {
		app.jsonAPIService.StopService()
	}

	if app.grpcAPIService != nil {
		app.grpcAPIService.StopService()
	}

	// add any other cleanup tasks here....
	log.Info("App cleanup completed\n\n")

	return nil
}

func (app *SpacemeshApp) startSpacemeshNode(ctx *cli.Context) error {
	log.Info("Starting local node...")
	port := *nodeparams.LocalTCPPortFlag.Destination
	address := fmt.Sprintf("0.0.0.0:%d", port)

	// start a new node passing the app-wide node config values and persist it to store
	// so future sessions use the same local node id
	node, err := p2p.NewLocalNode(address, nodeparams.ConfigValues, true)
	if err != nil {
		return err
	}

	app.Node = node

	conf := &apiconf.ConfigValues

	// todo: if there's no loaded account - do the new account interactive flow here

	// todo: if node has no loaded coin-base account then set the node coinbase to first account

	// todo: if node has a locked coinbase account then prompt for account passphrase to unlock it

	// todo: if node has no POS then start POS creation flow here unless user doesn't want to be a validator via cli

	// todo: start node consensus protocol here only after we have an unlocked account

	// start api servers
	if conf.StartGrpcServer || conf.StartJSONServer {
		// start grpc if specified or if json rpc specified
		app.grpcAPIService = api.NewGrpcService()
		app.grpcAPIService.StartService(nil)
	}

	if conf.StartJSONServer {
		app.jsonAPIService = api.NewJSONHTTPServer()
		app.jsonAPIService.StartService(nil)
	}

	// app blocks until it receives a signal to exit
	// this signal may come from the node or from sig-abort (ctrl-c)
	<-ExitApp
	return nil
}

// Main is the entry point for the Spacemesh console app - responsible for parsing and routing cli flags and commands.
// This is the root of all evil, called from Main.main().
func Main(commit, branch, version string) {

	// setup vars before creating the app - ugly but works
	Version = version
	Branch = branch
	Commit = commit

	App = newSpacemeshApp()

	if err := App.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
