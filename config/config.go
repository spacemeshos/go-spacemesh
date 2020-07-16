// Package config contains go-spacemesh node configuration definitions
package config

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/spacemeshos/go-spacemesh/activation"
	apiConfig "github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	hareConfig "github.com/spacemeshos/go-spacemesh/hare/config"
	eligConfig "github.com/spacemeshos/go-spacemesh/hare/eligibility/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	p2pConfig "github.com/spacemeshos/go-spacemesh/p2p/config"
	timeConfig "github.com/spacemeshos/go-spacemesh/timesync/config"
	postConfig "github.com/spacemeshos/post/config"
	"github.com/spf13/viper"
)

const (
	defaultConfigFileName = "./config.toml"
	defaultDataDirName    = "spacemesh"
	// NewBlockProtocol indicates the protocol name for new blocks arriving.
	NewBlockProtocol = "newBlock"
)

var (
	defaultHomeDir  = filesystem.GetUserHomeDirectory()
	defaultDataDir  = filepath.Join(defaultHomeDir, defaultDataDirName, "/")
	defaultTestMode = false
)

// Config defines the top level configuration for a spacemesh node
type Config struct {
	BaseConfig      `mapstructure:"main"`
	P2P             p2pConfig.Config      `mapstructure:"p2p"`
	API             apiConfig.Config      `mapstructure:"api"`
	HARE            hareConfig.Config     `mapstructure:"hare"`
	HareEligibility eligConfig.Config     `mapstructure:"hare-eligibility"`
	TIME            timeConfig.TimeConfig `mapstructure:"time"`
	REWARD          mesh.Config           `mapstructure:"reward"`
	POST            postConfig.Config     `mapstructure:"post"`
	LOGGING         LoggerConfig          `mapstructure:"logging"`
}

// DataDir returns the absolute path to use for the node's data. This is the tilde-expanded path given in the config
// with a subfolder named after the network ID.
func (cfg *Config) DataDir() string {
	return filepath.Join(filesystem.GetCanonicalPath(cfg.DataDirParent), fmt.Sprint(cfg.P2P.NetworkID))
}

// BaseConfig defines the default configuration options for spacemesh app
type BaseConfig struct {
	DataDirParent string `mapstructure:"data-folder"`

	ConfigFile string `mapstructure:"config"`

	TestMode bool `mapstructure:"test-mode"`

	CollectMetrics bool `mapstructure:"metrics"`
	MetricsPort    int  `mapstructure:"metrics-port"`

	OracleServer        string `mapstructure:"oracle_server"`
	OracleServerWorldID int    `mapstructure:"oracle_server_worldid"`

	GenesisTime      string `mapstructure:"genesis-time"`
	LayerDurationSec int    `mapstructure:"layer-duration-sec"`
	LayerAvgSize     int    `mapstructure:"layer-average-size"`
	LayersPerEpoch   int    `mapstructure:"layers-per-epoch"`
	Hdist            int    `mapstructure:"hdist"`

	PoETServer string `mapstructure:"poet-server"`

	MemProfile string `mapstructure:"mem-profile"`

	CPUProfile string `mapstructure:"cpu-profile"`

	PprofHTTPServer bool `mapstructure:"pprof-server"`

	GenesisConfPath string `mapstructure:"genesis-conf"`

	CoinbaseAccount string `mapstructure:"coinbase"`

	GenesisActiveSet int `mapstructure:"genesis-active-size"` // the active set size for genesis

	SyncRequestTimeout int `mapstructure:"sync-request-timeout"` // ms the timeout for direct request in the sync

	SyncInterval int `mapstructure:"sync-interval"` // sync interval in seconds

	SyncValidationDelta int `mapstructure:"sync-validation-delta"` // sync interval in seconds

	PublishEventsURL string `mapstructure:"events-url"`

	StartMining bool `mapstructure:"start-mining"`

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
	AtxDbStoreLoggerLevel     string `mapstructure:"atxDb"`
	PoetDbStoreLoggerLevel    string `mapstructure:"poetDb"`
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
	NipstBuilderLoggerLevel   string `mapstructure:"nipst"`
	AtxBuilderLoggerLevel     string `mapstructure:"atx-builder"`
	HareBeaconLoggerLevel     string `mapstructure:"hare-beacon"`
}

// DefaultConfig returns the default configuration for a spacemesh node
func DefaultConfig() Config {
	return Config{
		BaseConfig:      defaultBaseConfig(),
		P2P:             p2pConfig.DefaultConfig(),
		API:             apiConfig.DefaultConfig(),
		HARE:            hareConfig.DefaultConfig(),
		HareEligibility: eligConfig.DefaultConfig(),
		TIME:            timeConfig.DefaultConfig(),
		REWARD:          mesh.DefaultMeshConfig(),
		POST:            activation.DefaultConfig(),
	}
}

// DefaultBaseConfig returns a default configuration for spacemesh
func defaultBaseConfig() BaseConfig {
	return BaseConfig{
		DataDirParent:       defaultDataDir,
		ConfigFile:          defaultConfigFileName,
		TestMode:            defaultTestMode,
		CollectMetrics:      false,
		MetricsPort:         1010,
		OracleServer:        "http://localhost:3030",
		OracleServerWorldID: 0,
		GenesisTime:         time.Now().Format(time.RFC3339),
		LayerDurationSec:    30,
		LayersPerEpoch:      3,
		PoETServer:          "127.0.0.1",
		Hdist:               5,
		GenesisActiveSet:    5,
		BlockCacheSize:      20,
		SyncRequestTimeout:  2000,
		SyncInterval:        10,
		SyncValidationDelta: 30,
		AtxsPerBlock:        100,
		TxsPerBlock:         100,
	}
}

// LoadConfig load the config file
func LoadConfig(fileLocation string, vip *viper.Viper) (err error) {
	if fileLocation == "" {
		fileLocation = defaultConfigFileName
	}

	vip.SetConfigFile(fileLocation)
	err = vip.ReadInConfig()

	if err != nil {
		if fileLocation != defaultConfigFileName {
			log.Warning("failed loading config from %v trying %v", fileLocation, defaultConfigFileName)
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

// SetConfigFile overrides the default config file path
func (cfg *BaseConfig) SetConfigFile(file string) {
	cfg.ConfigFile = file
}
