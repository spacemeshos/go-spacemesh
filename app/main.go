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
)

type SpacemeshApp struct {
	*cli.App
	Node           p2p.LocalNode
	grpcApiService *api.SpaceMeshGrpcService
	jsonApiService *api.JsonHttpServer
}

// the main Spacemesh app - main entry point
// Access the node and the other top-level modules from the app
var App *SpacemeshApp

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
		nodeparams.SwarmBootstrap,
		nodeparams.RoutingTableBucketSizdFlag,
		nodeparams.RoutingTableAlphaFlag,
		nodeparams.RandomConnectionsFlag,
		nodeparams.BootstrapNodesFlag,
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

	// must be done here and not in app.before() so we won't lose any log entries
	sma.setupLogging()

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
		if (filesystem.PathExists(configPath)) {
			log.Info("Loading config file (path): %v", configPath)
			err := altsrc.InitInputSourceWithContext(ctx.App.Flags, func(context *cli.Context) (altsrc.InputSourceContext, error) {
				toml, err := altsrc.NewTomlSourceFromFile(configPath);
				config.CastConfigUints(toml, []map[string]*uint{nodeparams.NodeConfigUints, apiconf.ApiConfigUints})
				//config.CastConfigDurations(toml, []map[string]*time.Duration{nodeparams.NodeConfigDurations})
				return toml, err
			})(ctx)
			if err != nil {
				log.Warning("Config file had an error: %v", err)
			}
		} else {
			log.Warning("Coun'nt find config file %v", configPath)
		}
	} else {
		log.Warning("No config file defined using default config")
	}

	// todo: add misc app setup here (metrics, debug, etc....)

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

func (app *SpacemeshApp) startSpacemeshNode(ctx *cli.Context) error {

	log.Info("Starting local node...")
	port := *nodeparams.LocalTcpPortFlag.Destination
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
	if conf.StartGrpcServer || conf.StartJsonServer {
		// start grpc if specified or if json rpc specified
		app.grpcApiService = api.NewGrpcService()
		app.grpcApiService.StartService(nil)
	}

	if conf.StartJsonServer {
		app.jsonApiService = api.NewJsonHttpServer()
		app.jsonApiService.StartService(nil)
	}

	// app blocks until it receives a signal to exit
	// this signal may come from the node or from sig-abort (ctrl-c)
	<-ExitApp
	return nil
}

// The Spacemesh console app - responsible for parsing and routing cli flags and commands
// This is the root of all evil, called from Main.main()
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
