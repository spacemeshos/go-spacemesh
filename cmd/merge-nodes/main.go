package main

import (
	"fmt"
	"log"
	"os"

	"github.com/urfave/cli/v2"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/cmd/merge-nodes/internal"
)

var version string

func main() {
	app := &cli.App{
		Name:    "Spacemesh Node Merger",
		Usage:   "Merge two or more Spacemesh nodes into one",
		Version: version,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "from",
				Aliases:  []string{"f"},
				Usage:    "The `data` folder(s) to read identities from and merge into one node",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "to",
				Aliases:  []string{"t"},
				Usage:    "The `data` folder to write the merged node to",
				Required: true,
			},
			&cli.StringFlag{
				Name:     "name",
				Aliases:  []string{"n"},
				Usage:    "The `name` of the identity merged from `from` to `to`",
				Required: true,
			},
		},
		Action: func(ctx *cli.Context) error {
			cfg := zap.NewProductionConfig()
			cfg.Encoding = "console"
			dbLog, err := cfg.Build()
			if err != nil {
				return fmt.Errorf("create logger: %w", err)
			}
			defer dbLog.Sync()
			return internal.MergeDBs(ctx.Context, dbLog, ctx.String("name"), ctx.String("from"), ctx.String("to"))
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatalf("app run: %v\n", err)
	}
}
