// Package app provides the cli app shell of an unrily p2p node
package app

import (
	"fmt"
	"github.com/UnrulyOS/go-unruly/accounts"
	api "github.com/UnrulyOS/go-unruly/api"
	apiconf "github.com/UnrulyOS/go-unruly/api/config"
	"github.com/UnrulyOS/go-unruly/filesystem"
	"github.com/UnrulyOS/go-unruly/log"
	"github.com/UnrulyOS/go-unruly/p2p"
	"gopkg.in/urfave/cli.v1"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/UnrulyOS/go-unruly/app/config"
	nodeparams "github.com/UnrulyOS/go-unruly/p2p/nodeconfig"
)

type UnrulyApp struct {
	*cli.App
	Node           p2p.LocalNode
	grpcApiService *api.UnrulyGrpcService
	jsonApiService *api.JsonHttpServer
}

// the main unruly app - main entry point
// Access the node and the other top-level modules from the app
var App *UnrulyApp

var (
	appFlags = []cli.Flag{
		config.LoadConfigFileFlag,
		config.DataFolderPathFlag,

		// add all additional app flags here ...
	}
	nodeFlags = []cli.Flag{
		nodeparams.KSecurityFlag,
		nodeparams.LocalTcpPortFlag,
		nodeparams.NodeIdFlag,
		nodeparams.NetworkDialTimeout,
		nodeparams.NetworkConnKeepAlive,

		// add all additional node flags here ...
	}
	apiFlags = []cli.Flag{
		apiconf.StartGrpcApiServerFlag,
		apiconf.GrpcServerPortFlag,
		apiconf.StartJsonApiServerFlag,
		apiconf.JsonServerPortFlag,
	}

	ExitApp = make(chan bool, 1)

	// App semantic version. Can be over-written by build tool
	Version = "0.0.1"

	// build git branch. Can be over-written by build tool
	Branch = ""

	// build git commit. Can be over-written by build tool
	Commit = ""
)

// add toml config file support and sample toml file

func newUnrulyApp() *UnrulyApp {
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

	unrulyApp := &UnrulyApp{app, nil, nil, nil}

	// setup callbacks
	app.Before = unrulyApp.before
	app.Action = unrulyApp.startUnrulyNode
	app.After = unrulyApp.cleanup

	// must be done here and not in app.before() so we won't lose any log entries
	unrulyApp.setupLogging()

	return unrulyApp
}

// start the unruly node
func startUnrulyNode(ctx *cli.Context) error {
	return App.startUnrulyNode(ctx)
}

// setup app logging system
func (app *UnrulyApp) setupLogging() {

	// setup logging early
	dataDir, err := filesystem.GetUnrulyDataDirectoryPath()
	if err != nil {
		log.Error("Failed to setup unruly data dir")
		panic(err)
	}

	// todo: support configurable log file name (low priority)
	log.InitUnrulyLoggingSystem(dataDir, "unruly.log")

	log.Info("\n\nUnruly app session starting... %s", app.getAppInfo())
}

func (app *UnrulyApp) getAppInfo() string {
	return fmt.Sprintf("App version: %s. Git: %s - %s . Go Version: %s. OS: %s-%s ",
		Version, Branch, Commit, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

func (app *UnrulyApp) before(ctx *cli.Context) error {

	// max out box for now
	runtime.GOMAXPROCS(runtime.NumCPU())

	// exit gracefully - e.g. with app cleanup on sig abort (ctrl-c)
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)
	go func() {
		for _ = range signalChan {
			log.Info("Received an interrupt, stopping services...\n")
			ExitApp <- true
		}
	}()

	// todo: add misc app setup here (metrics, debug, etc....)

	// ensure all data folders exist
	filesystem.EnsureUnrulyDataDirectories()

	// load all accounts from store
	accounts.LoadAllAccounts()

	// todo: set coinbase account (and unlock it) based on flags

	return nil
}

// Unruly app cleanup tasks
func (app *UnrulyApp) cleanup(ctx *cli.Context) error {

	log.Info("App cleanup starting...")
	if app.jsonApiService != nil {
		app.jsonApiService.Stop()
	}

	if app.grpcApiService != nil {
		app.grpcApiService.StopService()
	}

	// add any other cleanup tasks here....
	log.Info("App cleanup completed\n\n")

	return nil
}

func (app *UnrulyApp) startUnrulyNode(ctx *cli.Context) error {

	log.Info("Starting local node...")
	port := *nodeparams.LocalTcpPortFlag.Destination
	address := fmt.Sprintf("localhost:%d", port)

	// start a new node passing the app-wide node config values
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

	// todo: start node consensus protocol here only after we have a locked

	// start api servers
	if conf.StartGrpcServer || conf.StartJsonServer {
		app.grpcApiService = api.NewGrpcService()
		go app.grpcApiService.StartService()
	}

	if conf.StartJsonServer {
		app.jsonApiService = api.NewJsonHttpServer()
		go app.jsonApiService.StartService()
	}

	// app blocks until it receives a signal to exit
	// this signal may come from the node or from sig-abort (ctrl-c)
	<-ExitApp
	return nil
}

// The Unruly console application - responsible for parsing and routing cli flags and commands
// this is the root of all evil, called from Main.main()
func Main(commit, branch, version string) {

	// setup vars before creating the app - ugly but works
	Version = version
	Branch = branch
	Commit = commit

	App = newUnrulyApp()

	if err := App.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
