// Package config contains go-spacemesh node configuration definitions
package config

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/viper"

	"github.com/spacemeshos/go-spacemesh/activation"
	apiConfig "github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/blocks"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	hareConfig "github.com/spacemeshos/go-spacemesh/hare/config"
	eligConfig "github.com/spacemeshos/go-spacemesh/hare/eligibility/config"
	"github.com/spacemeshos/go-spacemesh/layerfetcher"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	timeConfig "github.com/spacemeshos/go-spacemesh/timesync/config"
	"github.com/spacemeshos/go-spacemesh/tortoise"
	"github.com/spacemeshos/go-spacemesh/tortoisebeacon"
)

const (
	defaultConfigFileName = "./config.toml"
	defaultDataDirName    = "spacemesh"
	// NewBlockProtocol indicates the protocol name for new blocks arriving.
)

var (
	defaultHomeDir = filesystem.GetUserHomeDirectory()
	defaultDataDir = filepath.Join(defaultHomeDir, defaultDataDirName, "/")
)

// Config defines the top level configuration for a spacemesh node.
type Config struct {
	BaseConfig      `mapstructure:"main"`
	Genesis         *apiConfig.GenesisConfig `mapstructure:"genesis"`
	Tortoise        tortoise.Config          `mapstructure:"tortoise"`
	P2P             p2p.Config               `mapstructure:"p2p"`
	API             apiConfig.Config         `mapstructure:"api"`
	HARE            hareConfig.Config        `mapstructure:"hare"`
	HareEligibility eligConfig.Config        `mapstructure:"hare-eligibility"`
	TortoiseBeacon  tortoisebeacon.Config    `mapstructure:"tortoise-beacon"`
	TIME            timeConfig.TimeConfig    `mapstructure:"time"`
	REWARD          blocks.RewardConfig      `mapstructure:"reward"`
	POST            activation.PostConfig    `mapstructure:"post"`
	SMESHING        SmeshingConfig           `mapstructure:"smeshing"`
	LOGGING         LoggerConfig             `mapstructure:"logging"`
	FETCH           layerfetcher.Config      `mapstructure:"fetch"`
}

// DataDir returns the absolute path to use for the node's data. This is the tilde-expanded path given in the config
// with a subfolder named after the network ID.
func (cfg *Config) DataDir() string {
	return filepath.Join(filesystem.GetCanonicalPath(cfg.DataDirParent), fmt.Sprint(cfg.P2P.NetworkID))
}

// BaseConfig defines the default configuration options for spacemesh app.
type BaseConfig struct {
	DataDirParent string `mapstructure:"data-folder"`

	ConfigFile string `mapstructure:"config"`

	TestMode bool `mapstructure:"test-mode"`

	CollectMetrics bool `mapstructure:"metrics"`
	MetricsPort    int  `mapstructure:"metrics-port"`

	MetricsPush       string `mapstructure:"metrics-push"`
	MetricsPushPeriod int    `mapstructure:"metrics-push-period"`

	ProfilerName string `mapstructure:"profiler-name"`
	ProfilerURL  string `mapstructure:"profiler-url"`

	OracleServer        string `mapstructure:"oracle_server"`
	OracleServerWorldID int    `mapstructure:"oracle_server_worldid"`

	GenesisTime      string `mapstructure:"genesis-time"`
	LayerDurationSec int    `mapstructure:"layer-duration-sec"`
	LayerAvgSize     int    `mapstructure:"layer-average-size"`
	LayersPerEpoch   uint32 `mapstructure:"layers-per-epoch"`

	PoETServer string `mapstructure:"poet-server"`

	PprofHTTPServer bool `mapstructure:"pprof-server"`

	GoldenATXID string `mapstructure:"golden-atx"`

	GenesisActiveSet int `mapstructure:"genesis-active-size"` // the active set size for genesis

	SyncRequestTimeout int `mapstructure:"sync-request-timeout"` // ms the timeout for direct request in the sync

	SyncInterval int `mapstructure:"sync-interval"` // sync interval in seconds

	PublishEventsURL string `mapstructure:"events-url"`

	AtxsPerBlock int `mapstructure:"atxs-per-block"`

	TxsPerBlock int `mapstructure:"txs-per-block"`

	BlockCacheSize int `mapstructure:"block-cache-size"`

	AlwaysListen bool `mapstructure:"always-listen"` // force gossip to always be on (for testing)
}

// LoggerConfig holds the logging level for each module.
type LoggerConfig struct {
	AppLoggerLevel            string `mapstructure:"app"`
	P2PLoggerLevel            string `mapstructure:"p2p"`
	PostLoggerLevel           string `mapstructure:"post"`
	StateDbLoggerLevel        string `mapstructure:"stateDb"`
	StateLoggerLevel          string `mapstructure:"state"`
	AtxDbStoreLoggerLevel     string `mapstructure:"atxDbStore"`
	TBeaconLoggerLevel        string `mapstructure:"tBeacon"`
	WeakCoinLoggerLevel       string `mapstructure:"weakCoin"`
	PoetDbStoreLoggerLevel    string `mapstructure:"poetDbStore"`
	StoreLoggerLevel          string `mapstructure:"store"`
	PoetDbLoggerLevel         string `mapstructure:"poetDb"`
	MeshDBLoggerLevel         string `mapstructure:"meshDb"`
	TrtlLoggerLevel           string `mapstructure:"trtl"`
	AtxDbLoggerLevel          string `mapstructure:"atxDb"`
	BlkEligibilityLoggerLevel string `mapstructure:"block-eligibility"`
	MeshLoggerLevel           string `mapstructure:"mesh"`
	SyncLoggerLevel           string `mapstructure:"sync"`
	BlockOracleLevel          string `mapstructure:"block-oracle"`
	HareOracleLoggerLevel     string `mapstructure:"hare-oracle"`
	HareLoggerLevel           string `mapstructure:"hare"`
	BlockBuilderLoggerLevel   string `mapstructure:"block-builder"`
	BlockListenerLoggerLevel  string `mapstructure:"block-listener"`
	PoetListenerLoggerLevel   string `mapstructure:"poet"`
	NipostBuilderLoggerLevel  string `mapstructure:"nipost"`
	AtxBuilderLoggerLevel     string `mapstructure:"atx-builder"`
	HareBeaconLoggerLevel     string `mapstructure:"hare-beacon"`
	TimeSyncLoggerLevel       string `mapstructure:"timesync"`
}

// SmeshingConfig defines configuration for the node's smeshing (mining).
type SmeshingConfig struct {
	Start           bool                     `mapstructure:"smeshing-start"`
	CoinbaseAccount string                   `mapstructure:"smeshing-coinbase"`
	Opts            activation.PostSetupOpts `mapstructure:"smeshing-opts"`
}

// DefaultConfig returns the default configuration for a spacemesh node.
func DefaultConfig() Config {
	return Config{
		BaseConfig:      defaultBaseConfig(),
		Tortoise:        tortoise.DefaultConfig(),
		P2P:             p2p.DefaultConfig(),
		API:             apiConfig.DefaultConfig(),
		HARE:            hareConfig.DefaultConfig(),
		HareEligibility: eligConfig.DefaultConfig(),
		TortoiseBeacon:  tortoisebeacon.DefaultConfig(),
		TIME:            timeConfig.DefaultConfig(),
		REWARD:          blocks.DefaultRewardConfig(),
		POST:            activation.DefaultPostConfig(),
		SMESHING:        DefaultSmeshingConfig(),
		FETCH:           layerfetcher.DefaultConfig(),
	}
}

// DefaultTestConfig returns the default config for tests.
func DefaultTestConfig() Config {
	conf := DefaultConfig()
	conf.BaseConfig = defaultTestConfig()
	conf.P2P = p2p.DefaultConfig()
	conf.API = apiConfig.DefaultTestConfig()
	return conf
}

// DefaultBaseConfig returns a default configuration for spacemesh.
func defaultBaseConfig() BaseConfig {
	return BaseConfig{
		DataDirParent:       defaultDataDir,
		ConfigFile:          defaultConfigFileName,
		TestMode:            false,
		CollectMetrics:      false,
		MetricsPort:         1010,
		MetricsPush:         "", // "" = doesn't push
		MetricsPushPeriod:   60,
		ProfilerURL:         "",
		ProfilerName:        "gp-spacemesh",
		OracleServer:        "http://localhost:3030",
		OracleServerWorldID: 0,
		GenesisTime:         time.Now().Format(time.RFC3339),
		LayerDurationSec:    30,
		LayersPerEpoch:      3,
		PoETServer:          "127.0.0.1",
		GoldenATXID:         "0x5678", // TODO: Change the value
		BlockCacheSize:      20,
		SyncRequestTimeout:  2000,
		SyncInterval:        10,
		AtxsPerBlock:        100,
		TxsPerBlock:         100,
	}
}

// DefaultSmeshingConfig returns the node's default smeshing configuration.
func DefaultSmeshingConfig() SmeshingConfig {
	return SmeshingConfig{
		Start:           false,
		CoinbaseAccount: "",
		Opts:            activation.DefaultPostSetupOpts(),
	}
}

func defaultTestConfig() BaseConfig {
	conf := defaultBaseConfig()
	conf.MetricsPort += 10000
	return conf
}

// LoadConfig load the config file.
func LoadConfig(fileLocation string, vip *viper.Viper) (err error) {
	if fileLocation == "" {
		fileLocation = defaultConfigFileName
	}

	vip.SetConfigFile(fileLocation)
	err = vip.ReadInConfig()

	if err != nil {
		if fileLocation != defaultConfigFileName {
			log.Warning("failed loading config from %v trying %v. error %v", fileLocation, defaultConfigFileName, err)
			vip.SetConfigFile(defaultConfigFileName)
			err = vip.ReadInConfig()
		}
		// we change err so check again
		if err != nil {
			return fmt.Errorf("failed to read config file %v", err)
		}
	}

	return nil
}

// SetConfigFile overrides the default config file path.
func (cfg *BaseConfig) SetConfigFile(file string) {
	cfg.ConfigFile = file
}
