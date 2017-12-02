package app

import (
	"fmt"
	"gopkg.in/urfave/cli.v1"
	"os"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/UnrulyOS/go-unruly/app/config"
	nodeparams "github.com/UnrulyOS/go-unruly/node/config"
)

var (
	appVersion = "0.0.1"

	gitCommitHash = ""

	app = NewApp(gitCommitHash, "- the go-unruly node")

	appFlags = []cli.Flag{
		config.LoadConfigFileFlag,
		config.DataFolderPath,
		// add all app flags here
	}

	nodeFlags = []cli.Flag{
		nodeparams.KSecurityFlag,
		// add all node flags here
	}

	exitApp = make(chan bool, 1)
)

// todo: implement app commands, flags, metrics and debug here!!!!

// todo: add leveldb support and sample read / write

// add toml config file support and sample toml file

func init() {
	app.Action = startUnrulyNode
	app.HideVersion = true
	app.Copyright = "(c) 2017 The go-unruly Authors"
	app.Commands = []cli.Command{
		VersionCommand(appVersion),
		// add all other commands here
	}

	app.Flags = append(app.Flags, appFlags...)
	app.Flags = append(app.Flags, nodeFlags...)

	sort.Sort(cli.CommandsByName(app.Commands))
	sort.Sort(cli.FlagsByName(app.Flags))

	app.Before = func(ctx *cli.Context) error {

		// max out box for now
		runtime.GOMAXPROCS(runtime.NumCPU())

		// todo: pre app setup here (metrics, debug, etc....)

		return nil
	}

	app.After = func(ctx *cli.Context) error {

		// post app cleanup goes here
		return nil
	}
}

func NewApp(gitCommitHash, usage string) *cli.App {
	app := cli.NewApp()
	app.Name = filepath.Base(os.Args[0])
	app.Author = "The go-unruly authors"
	app.Email = "app@unrulyos.io"
	app.Version = "0.0.1"

	if gitCommitHash != "" {
		app.Version += "-" + gitCommitHash[:8]
	}
	app.Usage = usage
	return app
}

// start the unruly node
func startUnrulyNode(ctx *cli.Context) error {
	// todo: implement me - run the node here - pass it the exitApp chan

	// wait until node exists here and exit properly
	<-exitApp
	return nil
}

// The Unruly console application - responsible for parsing and routing cli flags and commands
// this is the root of all evil, called from Main.main()
func Main() {

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
