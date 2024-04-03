package main

import (
	"fmt"
	"os"

	"github.com/urfave/cli/v2"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/cmd/merge-nodes/internal"
)

var version string

func main() {
	cfg := zap.NewProductionConfig()
	cfg.Encoding = "console"
	dbLog, err := cfg.Build()
	if err != nil {
		fmt.Println("create logger:", err)
		os.Exit(1)
	}
	defer dbLog.Sync()

	app := &cli.App{
		Name: "Spacemesh Node Merger",
		Usage: "Merge identities of two Spacemesh nodes into one.\n" +
			"The `from` node will be merged into the `to` node, leaving the `from` node untouched.\n" +
			"The `to` node can be an existing node or an empty folder.\n" +
			"Be sure to backup the `to` node before running this command.\n" +
			"NOTE: both `from` and `to` nodes must be upgraded to the latest version before running this command.\n" +
			"NOTE: after upgrading and starting the nodes at least once, convert them to remote nodes before merging.",
		Version: version,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "from",
				Aliases:  []string{"f"},
				Usage:    "The `data` folder to read identities from and merge into `to`",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "to",
				Aliases:  []string{"t"},
				Usage:    "The `data` folder to write the merged node to. Can be an existing remote node or empty.",
				Required: true,
			},
		},
		Action: func(ctx *cli.Context) error {
			return internal.MergeDBs(ctx.Context, dbLog, ctx.String("from"), ctx.String("to"))
		},
	}

	if err := app.Run(os.Args); err != nil {
		dbLog.Sugar().Warnln("app run:", err)
		os.Exit(1)
	}
}
