package app

import (
	"path/filepath"
	"os"
	"gopkg.in/urfave/cli.v1"
	"fmt"
	"sort"
	"runtime"
	"github.com/UnrulyOS/go-unruly/node"
)

var (
	appVersion = "0.0.1"
	gitCommitHash = ""
	app = NewApp(gitCommitHash,"- the go-unruly node")

	appFlags = []cli.Flag{
		cli.StringFlag{
			Name: "config, c",
			Usage: "Load configuration from `FILE`",
			Value: DefaultConfig.ConfigFilePath,
		},
	}

	nodeFlags = []cli.Flag{
		cli.Uint64Flag {
			Name: "k",
			Usage: "Consensus protocol k security param",
			Value: node.DefaultConfig.SecurityParam,
		},
	}
)

// todo: implement app commands, flags, metrics and debug here!!!!

func init() {
	app.Action = startUnrulyNode
	app.HideVersion = true
	app.Copyright = "Copyright 2017 The go-unruly Authors"
	app.Commands = []cli.Command{
		{
			Name:    "version",
			Aliases: []string{"v"},
			Usage:   "print versions",
			ArgsUsage: " ",
			Category:  "General commands",
			Action:  func(c *cli.Context) error {
				fmt.Println("Version:", appVersion)
				fmt.Println("Go Version:", runtime.Version())
				fmt.Println("OS:", runtime.GOOS)
				fmt.Println("Arch:", runtime.GOARCH)
				return nil
				},
		},
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
	// todo: implement me - run the node here

	// wait until node exists here
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
