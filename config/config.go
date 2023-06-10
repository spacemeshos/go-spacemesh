// Package config contains go-spacemesh node configuration definitions
package config

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/beacon"
	"github.com/spacemeshos/go-spacemesh/bootstrap"
	"github.com/spacemeshos/go-spacemesh/checkpoint"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	vm "github.com/spacemeshos/go-spacemesh/genvm"
	hareConfig "github.com/spacemeshos/go-spacemesh/hare/config"
	eligConfig "github.com/spacemeshos/go-spacemesh/hare/eligibility/config"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/syncer"
	timeConfig "github.com/spacemeshos/go-spacemesh/timesync/config"
	"github.com/spacemeshos/go-spacemesh/tortoise"
)

const (
	defaultDataDirName = "spacemesh"
	// NewBlockProtocol indicates the protocol name for new blocks arriving.
)

var (
	defaultHomeDir string
	defaultDataDir string
)

func init() {
	defaultHomeDir, _ = os.UserHomeDir()
	defaultDataDir = filepath.Join(defaultHomeDir, defaultDataDirName, "/")
}

// Config defines the top level configuration for a spacemesh node.
type Config struct {
	BaseConfig      `mapstructure:"main"`
	Address         *types.Config         `mapstructure:"address"`
	Genesis         *GenesisConfig        `mapstructure:"genesis"`
	Tortoise        tortoise.Config       `mapstructure:"tortoise"`
	P2P             p2p.Config            `mapstructure:"p2p"`
	API             grpcserver.Config     `mapstructure:"api"`
	HARE            hareConfig.Config     `mapstructure:"hare"`
	HareEligibility eligConfig.Config     `mapstructure:"hare-eligibility"`
	Beacon          beacon.Config         `mapstructure:"beacon"`
	TIME            timeConfig.TimeConfig `mapstructure:"time"`
	VM              vm.Config             `mapstructure:"vm"`
	POST            activation.PostConfig `mapstructure:"post"`
	POET            activation.PoetConfig `mapstructure:"poet"`
	SMESHING        SmeshingConfig        `mapstructure:"smeshing"`
	LOGGING         LoggerConfig          `mapstructure:"logging"`
	FETCH           fetch.Config          `mapstructure:"fetch"`
	Bootstrap       bootstrap.Config      `mapstructure:"bootstrap"`
	Sync            syncer.Config         `mapstructure:"syncer"`
	Recovery        checkpoint.Config     `mapstructure:"recovery"`
}

// DataDir returns the absolute path to use for the node's data. This is the tilde-expanded path given in the config
// with a subfolder named after the network ID.
func (cfg *Config) DataDir() string {
	return filepath.Clean(cfg.DataDirParent)
}

// BaseConfig defines the default configuration options for spacemesh app.
type BaseConfig struct {
	DataDirParent string `mapstructure:"data-folder"`
	FileLock      string `mapstructure:"filelock"`
	Standalone    bool   `mapstructure:"standalone"`

	ConfigFile string `mapstructure:"config"`

	CollectMetrics bool `mapstructure:"metrics"`
	MetricsPort    int  `mapstructure:"metrics-port"`

	MetricsPush       string `mapstructure:"metrics-push"`
	MetricsPushPeriod int    `mapstructure:"metrics-push-period"`
	MetricsPushUser   string `mapstructure:"metrics-push-user"`
	MetricsPushPass   string `mapstructure:"metrics-push-pass"`

	ProfilerName string `mapstructure:"profiler-name"`
	ProfilerURL  string `mapstructure:"profiler-url"`

	OracleServer        string `mapstructure:"oracle_server"`
	OracleServerWorldID int    `mapstructure:"oracle_server_worldid"`

	LayerDuration  time.Duration `mapstructure:"layer-duration"`
	LayerAvgSize   uint32        `mapstructure:"layer-average-size"`
	LayersPerEpoch uint32        `mapstructure:"layers-per-epoch"`

	PoETServers []string `mapstructure:"poet-server"`

	PprofHTTPServer bool `mapstructure:"pprof-server"`

	PublishEventsURL string `mapstructure:"events-url"`

	TxsPerProposal int    `mapstructure:"txs-per-proposal"`
	BlockGasLimit  uint64 `mapstructure:"block-gas-limit"`
	// if the number of proposals with the same mesh state crosses this threshold (in percentage),
	// then we optimistically filter out infeasible transactions before constructing the block.
	OptFilterThreshold int    `mapstructure:"optimistic-filtering-threshold"`
	TickSize           uint64 `mapstructure:"tick-size"`

	DatabaseConnections     int  `mapstructure:"db-connections"`
	DatabaseLatencyMetering bool `mapstructure:"db-latency-metering"`
}

// SmeshingConfig defines configuration for the node's smeshing (mining).
type SmeshingConfig struct {
	Start           bool                              `mapstructure:"smeshing-start"`
	CoinbaseAccount string                            `mapstructure:"smeshing-coinbase"`
	Opts            activation.PostSetupOpts          `mapstructure:"smeshing-opts"`
	ProvingOpts     activation.PostProvingOpts        `mapstructure:"smeshing-proving-opts"`
	VerifyingOpts   activation.PostProofVerifyingOpts `mapstructure:"smeshing-verifying-opts"`
}

// DefaultConfig returns the default configuration for a spacemesh node.
func DefaultConfig() Config {
	return Config{
		Address:         types.DefaultAddressConfig(),
		BaseConfig:      defaultBaseConfig(),
		Genesis:         DefaultGenesisConfig(),
		Tortoise:        tortoise.DefaultConfig(),
		P2P:             p2p.DefaultConfig(),
		API:             grpcserver.DefaultConfig(),
		HARE:            hareConfig.DefaultConfig(),
		HareEligibility: eligConfig.DefaultConfig(),
		Beacon:          beacon.DefaultConfig(),
		TIME:            timeConfig.DefaultConfig(),
		VM:              vm.DefaultConfig(),
		POST:            activation.DefaultPostConfig(),
		POET:            activation.DefaultPoetConfig(),
		SMESHING:        DefaultSmeshingConfig(),
		FETCH:           fetch.DefaultConfig(),
		LOGGING:         defaultLoggingConfig(),
		Bootstrap:       bootstrap.DefaultConfig(),
		Sync:            syncer.DefaultConfig(),
		Recovery:        checkpoint.DefaultConfig(),
	}
}

// DefaultTestConfig returns the default config for tests.
func DefaultTestConfig() Config {
	conf := DefaultConfig()
	conf.BaseConfig = defaultTestConfig()
	conf.P2P = p2p.DefaultConfig()
	conf.API = grpcserver.DefaultTestConfig()
	conf.Address = types.DefaultTestAddressConfig()
	return conf
}

// DefaultBaseConfig returns a default configuration for spacemesh.
func defaultBaseConfig() BaseConfig {
	return BaseConfig{
		DataDirParent:       defaultDataDir,
		FileLock:            filepath.Join(os.TempDir(), "spacemesh.lock"),
		CollectMetrics:      false,
		MetricsPort:         1010,
		MetricsPush:         "", // "" = doesn't push
		MetricsPushPeriod:   60,
		ProfilerURL:         "",
		ProfilerName:        "gp-spacemesh",
		OracleServer:        "http://localhost:3030",
		OracleServerWorldID: 0,
		LayerDuration:       30 * time.Second,
		LayersPerEpoch:      3,
		PoETServers:         []string{"127.0.0.1"},
		TxsPerProposal:      100,
		BlockGasLimit:       math.MaxUint64,
		OptFilterThreshold:  90,
		TickSize:            100,
		DatabaseConnections: 16,
	}
}

// DefaultSmeshingConfig returns the node's default smeshing configuration.
func DefaultSmeshingConfig() SmeshingConfig {
	return SmeshingConfig{
		Start:           false,
		CoinbaseAccount: "",
		Opts:            activation.DefaultPostSetupOpts(),
		ProvingOpts:     activation.DefaultPostProvingOpts(),
		VerifyingOpts:   activation.DefaultPostVerifyingOpts(),
	}
}

func defaultTestConfig() BaseConfig {
	conf := defaultBaseConfig()
	conf.MetricsPort += 10000
	return conf
}

// LoadConfig load the config file.
func LoadConfig(config string, vip *viper.Viper) error {
	if config == "" {
		return nil
	}
	vip.SetConfigFile(config)
	if err := vip.ReadInConfig(); err != nil {
		return fmt.Errorf("can't load config at %s: %w", config, err)
	}
	return nil
}
