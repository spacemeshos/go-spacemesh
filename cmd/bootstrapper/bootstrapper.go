package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

var (
	bitcoinUrl     string
	nodeEndpoint   string
	genesisTime    string
	layerDuration  time.Duration
	layersPerEpoch uint32
	epochOffset    uint32
	serveUpdate    bool
	port           int
	dataDir        string
	logLevel       string
	logjson        bool
)

func init() {
	// external resources
	cmd.PersistentFlags().StringVar(&bitcoinUrl, "bitcoin-url",
		"https://api.blockcypher.com/v1/btc/main", "specify URL to get bitcoin block hash")
	cmd.PersistentFlags().StringVar(&nodeEndpoint, "node-endpoint",
		"localhost:9092", "specify grpc endpoint for a spacemesh node")

	// network parameters
	cmd.PersistentFlags().StringVar(&genesisTime, "genesis-time",
		"", "Time of the genesis layer in 2019-13-02T17:02:00+00:00 format")
	cmd.PersistentFlags().DurationVar(&layerDuration, "layer-duration",
		5*time.Minute, "duration per layer")
	cmd.PersistentFlags().Uint32Var(&layersPerEpoch, "layers-per-epoch",
		4032, "number of layers per epoch")
	cmd.PersistentFlags().Uint32Var(&epochOffset, "epoch-offset",
		288, "number of layers before the next epoch start to publish update")

	// for systests only
	cmd.PersistentFlags().BoolVar(&serveUpdate, "serve-update",
		false, "if true, starts a http server to serve update too")
	cmd.PersistentFlags().IntVar(&port, "port",
		8080, "if starting a server, the port number to use")

	// admin
	cmd.PersistentFlags().StringVar(&dataDir, "data-dir", os.TempDir(), "directory to store update data")
	cmd.PersistentFlags().StringVar(&logLevel, "level", "info", "logging level")
	cmd.PersistentFlags().BoolVar(&logjson, "log-json", true, "if true, log in json format")
}

var cmd = &cobra.Command{
	Use:   "bootstrapper",
	Short: "generate bootstrapping data",
	RunE: func(cmd *cobra.Command, args []string) error {
		types.SetLayersPerEpoch(layersPerEpoch)
		if logjson {
			log.JSONLog(true)
		}
		lvl, err := zap.ParseAtomicLevel(strings.ToLower(logLevel))
		if err != nil {
			return err
		}
		logger := log.NewWithLevel("", lvl)
		genesis, err := time.Parse(time.RFC3339, genesisTime)
		if err != nil {
			return fmt.Errorf("parse genesis time %v: %w ", genesisTime, err)
		}

		clock, err := timesync.NewClock(
			timesync.WithLayerDuration(layerDuration),
			timesync.WithTickInterval(1*time.Second),
			timesync.WithGenesisTime(genesis.Local()),
			timesync.WithLogger(logger.WithName("clock")),
		)
		if err != nil {
			return fmt.Errorf("create clock %w ", err)
		}

		g := NewGenerator(
			clock,
			epochOffset,
			bitcoinUrl,
			nodeEndpoint,
			WithLogger(logger.WithName("generator")),
		)

		var (
			srv *Server
			eg  errgroup.Group
		)
		if serveUpdate {
			srv = NewServer(afero.NewOsFs(), port, logger.WithName("server"))
			err := srv.Start()
			if err != nil {
				return fmt.Errorf("start http server: %w", err)
			}
		}

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		appErr := g.Run(ctx)

		shutdownCxt, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if srv != nil {
			if err = srv.Stop(shutdownCxt); err != nil {
				logger.Error("failed to gracefully shutdown http server: %w", err)
				return err
			}
		}
		_ = eg.Wait()
		return appErr
	},
}

func main() {
	if err := cmd.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
