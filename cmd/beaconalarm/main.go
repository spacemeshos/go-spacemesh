package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/cmd/beaconalarm/metrics"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/node"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

var (
	serverURL    string
	k8sNamespace string
)

func main() {
	if err := getCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func getCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "alarm",
		Short: "start alarm",
		Run: func(c *cobra.Command, args []string) {
			conf, err := loadConfig(c)
			if err != nil {
				log.With().Fatal("failed to initialize config", log.Err(err))
			}

			if conf.LOGGING.Encoder == config.JSONLogEncoder {
				log.JSONLog(true)
			}
			logger := log.NewDefault("main")

			// Create a context for graceful shutdown
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
			defer cancel()

			// Start the clock
			clock, err := setupClock()
			if err != nil {
				logger.With().Fatal("failed to setup clock", log.Err(err))
			}

			// Start the server
			server, err := metrics.NewServer(":8080")
			if err != nil {
				logger.With().Fatal("failed to create server", log.Err(err))
			}
			if err := server.Start(); err != nil {
				logger.With().Fatal("failed to start server", log.Err(err))
			}
			defer shutdownServer(server, logger)

			clientOpts := []metrics.ClientOptionFunc{
				metrics.WithURL(serverURL),
				metrics.WithBeaconConfig(conf.Beacon),
				metrics.WithLogger(log.NewDefault("client")),
				metrics.WithClock(clock),
			}
			if os.Getenv("MIMIR_USR") != "" && os.Getenv("MIMIR_PWD") != "" && os.Getenv("MIMIR_ORG") != "" {
				clientOpts = append(clientOpts, metrics.WithBasicAuth(os.Getenv("MIMIR_ORG"), os.Getenv("MIMIR_USR"), os.Getenv("MIMIR_PWD")))
			}

			// Start the client
			client, err := metrics.NewClient(clientOpts...)
			if err != nil {
				logger.With().Fatal("failed to create client", log.Err(err))
			}

			var eg errgroup.Group
			eg.Go(func() error {
				logger.Info("starting alarm")

				for epoch := types.EpochID(2); ; epoch++ {
					// Fetch the metric value from the Prometheus server
					value, err := client.FetchBeaconValue(ctx, k8sNamespace, epoch)
					switch {
					case errors.Is(err, context.Canceled):
						return nil
					case err != nil:
						logger.With().Error("failed to fetch beacon value", log.Err(err))
						server.UpdateAlarm(epoch.String(), true)
						continue
					default:
					}
					logger.With().Info("beacon value fetched", log.FieldNamed("epoch", epoch), log.String("value", value))
					server.UpdateAlarm(epoch.String(), false)
				}
			})

			if err := eg.Wait(); err != nil {
				logger.With().Fatal("failed to wait for goroutines", log.Err(err))
			}
		},
	}

	cmd.AddCommands(c)

	c.PersistentFlags().StringVar(&serverURL, "prometheus", "https://mimir.spacemesh.dev/prometheus", "Prometheus server URL")
	c.PersistentFlags().StringVar(&k8sNamespace, "k8sNamespace", "devnet-402-short", "Kubernetes namespace to monitor")
	return c
}

func loadConfig(c *cobra.Command) (*config.Config, error) {
	conf, err := node.LoadConfigFromFile()
	if err != nil {
		return nil, err
	}
	if err := cmd.EnsureCLIFlags(c, conf); err != nil {
		return nil, fmt.Errorf("mapping cli flags to config: %w", err)
	}
	return conf, nil
}

func shutdownServer(server *metrics.Server, logger log.Logger) {
	logger.Info("Shutting down server...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := server.Stop(shutdownCtx); err != nil {
		log.With().Error("failed to stop server", log.Err(err))
	}
}

func setupClock() (*timesync.NodeClock, error) {
	appConfig, err := node.LoadConfigFromFile()
	if err != nil {
		return nil, fmt.Errorf("cannot load config file: %w", err)
	}
	types.SetLayersPerEpoch(appConfig.LayersPerEpoch)
	gTime, err := time.Parse(time.RFC3339, appConfig.Genesis.GenesisTime)
	if err != nil {
		return nil, fmt.Errorf("cannot parse genesis time %s: %w", appConfig.Genesis.GenesisTime, err)
	}
	clock, err := timesync.NewClock(
		timesync.WithLayerDuration(appConfig.LayerDuration),
		timesync.WithTickInterval(1*time.Second),
		timesync.WithGenesisTime(gTime),
		timesync.WithLogger(log.NewDefault("clock")),
	)
	if err != nil {
		return nil, err
	}
	return clock, nil
}
