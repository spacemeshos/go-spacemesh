package config

import (
	"fmt"
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
	"path/filepath"
	"time"
)

const (
	defaultConfigFileName  = "./config.toml"
	defaultLogFileName     = "spacemesh.log"
	defaultAccountFileName = "accounts"
	defaultDataDirName     = "spacemesh"
	Genesis                = mesh.Genesis
	NewBlockProtocol       = "newBlock"
)

var (
	defaultHomeDir    = filesystem.GetUserHomeDirectory()
	defaultDataDir    = filepath.Join(defaultHomeDir, defaultDataDirName, "/")
	defaultConfigFile = filepath.Join(defaultHomeDir, defaultConfigFileName)
	defaultLogDir     = filepath.Join(defaultHomeDir, defaultLogFileName)
	defaultAccountDir = filepath.Join(defaultHomeDir, defaultAccountFileName)
	defaultTestMode   = false
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

// BaseConfig defines the default configuration options for spacemesh app
type BaseConfig struct {
	HomeDir string

	DataDir string `mapstructure:"data-folder"`

	ConfigFile string `mapstructure:"config"`

	LogDir string `mapstructure:"log-dir"`

	AccountDir string `mapstructure:"account-dir"`

	TestMode bool `mapstructure:"test-mode"`

	CollectMetrics bool `mapstructure:"metrics"`
	MetricsPort    int  `mapstructure:"metrics-port"`

	OracleServer        string `mapstructure:"oracle_server"`
	OracleServerWorldId int    `mapstructure:"oracle_server_worldid"`

	GenesisTime      string `mapstructure:"genesis-time"`
	LayerDurationSec int    `mapstructure:"layer-duration-sec"`
	LayerAvgSize     int    `mapstructure:"layer-average-size"`
	LayersPerEpoch   int    `mapstructure:"layers-per-epoch"`
	Hdist            int    `mapstructure:"hdist"`

	PoETServer string `mapstructure:"poet-server"`

	MemProfile string `mapstructure:"mem-profile"`

	CpuProfile string `mapstructure:"cpu-profile"`

	PprofHttpServer bool `mapstructure:"pprof-server"`

	GenesisConfPath string `mapstructure:"genesis-conf"`

	CoinbaseAccount string `mapstructure:"coinbase"`

	GenesisActiveSet int `mapstructure:"genesis-active-size"` // the active set size for genesis

	SyncRequestTimeout int `mapstructure:"sync-request-timeout"` // the timeout for direct request in the sync

	PublishEventsUrl string `mapstructure:"events-url"`

	StartMining bool `mapstructure:"start-mining"`

	AtxsPerBlock int `mapstructure:"atxs-per-block"`
}

type LoggerConfig struct {
	AppLoggerLevel            string `mapstructure:"app"`
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
		HomeDir:             defaultHomeDir,
		DataDir:             defaultDataDir,
		ConfigFile:          defaultConfigFileName,
		LogDir:              defaultLogDir,
		AccountDir:          defaultAccountDir,
		TestMode:            defaultTestMode,
		CollectMetrics:      false,
		MetricsPort:         1010,
		OracleServer:        "http://localhost:3030",
		OracleServerWorldId: 0,
		GenesisTime:         time.Now().Format(time.RFC3339),
		LayerDurationSec:    30,
		LayersPerEpoch:      3,
		PoETServer:          "127.0.0.1",
		Hdist:               5,
		GenesisActiveSet:    5,
		SyncRequestTimeout:  2000,
		AtxsPerBlock:        100,
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
