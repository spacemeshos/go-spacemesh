package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/cmd/beaconalarm/metrics"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/config/presets"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/node"
	"github.com/spacemeshos/go-spacemesh/node/mapstructureutil"
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
	conf := config.MainnetConfig()
	var configPath *string
	c := &cobra.Command{
		Use:   "alarm",
		Short: "start alarm",
		RunE: func(c *cobra.Command, args []string) error {
			preset := conf.Preset // might be set via CLI flag
			if err := loadConfig(&conf, preset, *configPath); err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			// apply CLI args to config
			if err := c.ParseFlags(os.Args[1:]); err != nil {
				return fmt.Errorf("parsing flags: %w", err)
			}

			if conf.LOGGING.Encoder == config.JSONLogEncoder {
				log.JSONLog(true)
			}
			logger := log.NewDefault("main")

			// Start the clock
			clock, err := setupClock(conf)
			if err != nil {
				return fmt.Errorf("failed to setup clock: %w", err)
			}

			// Start the server
			server, err := metrics.NewServer(":8080")
			if err != nil {
				return fmt.Errorf("failed to create server: %w", err)
			}
			if err := server.Start(); err != nil {
				return fmt.Errorf("failed to start server: %w", err)
			}
			defer shutdownServer(server, logger)

			clientOpts := []metrics.ClientOptionFunc{
				metrics.WithURL(serverURL),
				metrics.WithBeaconConfig(conf.Beacon),
				metrics.WithLogger(log.NewDefault("client")),
				metrics.WithClock(clock),
			}
			if os.Getenv("MIMIR_USR") != "" && os.Getenv("MIMIR_PWD") != "" && os.Getenv("MIMIR_ORG") != "" {
				clientOpts = append(clientOpts,
					metrics.WithBasicAuth(os.Getenv("MIMIR_ORG"), os.Getenv("MIMIR_USR"), os.Getenv("MIMIR_PWD")),
				)
			}

			// Start the client
			client, err := metrics.NewClient(clientOpts...)
			if err != nil {
				return fmt.Errorf("failed to create client: %w", err)
			}

			// Create a context for graceful shutdown
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
			defer cancel()

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
				return fmt.Errorf("failed to wait for goroutines: %w", err)
			}
			return nil
		},
	}

	configPath = cmd.AddFlags(c.PersistentFlags(), &conf)

	c.PersistentFlags().StringVar(&serverURL, "prometheus",
		"https://mimir.spacemesh.dev/prometheus", "Prometheus server URL")
	c.PersistentFlags().StringVar(&k8sNamespace, "k8sNamespace",
		"devnet-402-short", "Kubernetes namespace to monitor")
	return c
}

// loadConfig loads config and preset (if provided) into the provided config.
// It first loads the preset and then overrides it with values from the config file.
func loadConfig(cfg *config.Config, preset, path string) error {
	v := viper.New()
	// read in config from file
	if err := config.LoadConfig(path, v); err != nil {
		return err
	}

	// override default config with preset if provided
	if len(preset) == 0 && v.IsSet("preset") {
		preset = v.GetString("preset")
	}
	if len(preset) > 0 {
		p, err := presets.Get(preset)
		if err != nil {
			return err
		}
		*cfg = p
	}

	// Unmarshall config file into config struct
	hook := mapstructure.ComposeDecodeHookFunc(
		mapstructure.StringToTimeDurationHookFunc(),
		mapstructure.StringToSliceHookFunc(","),
		mapstructureutil.AddressListDecodeFunc(),
		mapstructureutil.BigRatDecodeFunc(),
		mapstructureutil.PostProviderIDDecodeFunc(),
		mapstructureutil.DeprecatedHook(),
		mapstructure.TextUnmarshallerHookFunc(),
	)

	opts := []viper.DecoderConfigOption{
		viper.DecodeHook(hook),
		node.WithZeroFields(),
		node.WithIgnoreUntagged(),
		node.WithErrorUnused(),
	}

	// load config if it was loaded to the viper
	if err := v.Unmarshal(cfg, opts...); err != nil {
		return fmt.Errorf("unmarshal config: %w", err)
	}
	return nil
}

func shutdownServer(server *metrics.Server, logger log.Logger) {
	logger.Info("Shutting down server...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := server.Stop(shutdownCtx); err != nil {
		log.With().Error("failed to stop server", log.Err(err))
	}
}

func setupClock(conf config.Config) (*timesync.NodeClock, error) {
	types.SetLayersPerEpoch(conf.LayersPerEpoch)
	gTime, err := time.Parse(time.RFC3339, conf.Genesis.GenesisTime)
	if err != nil {
		return nil, fmt.Errorf("cannot parse genesis time %s: %w", conf.Genesis.GenesisTime, err)
	}
	return timesync.NewClock(
		timesync.WithLayerDuration(conf.LayerDuration),
		timesync.WithTickInterval(1*time.Second),
		timesync.WithGenesisTime(gTime),
		timesync.WithLogger(log.NewDefault("clock")),
	)
}
