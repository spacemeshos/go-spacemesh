package app

import (
	//node "github.com/UnrulyOS/go-unruly/node"
	"path/filepath"
	"os"
	"gopkg.in/urfave/cli.v1"
	"fmt"
	"sort"
	"runtime"
)

var (
	gitCommitHash = ""
	app = NewApp(gitCommitHash,"gurn - the go-unruly node")
)

// todo: implement commands, flags, metrics and debug here!!!!

func init() {
	app.Action = startUnrulyNode
	app.HideVersion = true
	app.Copyright = "Copyright 2017 The go-unruly Authors"
	app.Commands = []cli.Command{}
	sort.Sort(cli.CommandsByName(app.Commands))

	app.Before = func(ctx *cli.Context) error {
		runtime.GOMAXPROCS(runtime.NumCPU())

		// todo: pre app setup here (metrics, debug, etc....)
		return nil
	}

	app.After = func(ctx *cli.Context) error {
		// post app cleanup here
		return nil
	}
}

func NewApp(gitCommitHash, usage string) *cli.App {
	app := cli.NewApp()
	app.Name = filepath.Base(os.Args[0])
	app.Author = ""
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
	return nil
}

// The Unruly console application - responsible for parsing and routing cli flags and commands
func Main() {
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
