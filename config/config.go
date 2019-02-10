package config

import (
	"fmt"
	apiConfig "github.com/spacemeshos/go-spacemesh/api/config"
	consensusConfig "github.com/spacemeshos/go-spacemesh/consensus/config"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/log"
	p2pConfig "github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spf13/viper"
	"path/filepath"
)

const (
	defaultConfigFileName  = "./config.toml"
	defaultLogFileName     = "spacemesh.log"
	defaultAccountFileName = "accounts"
	defaultDataDirName     = "spacemesh"
	defaultAppIntParam     = 20
	defaultAppBoolParam    = 20
)

var (
	defaultHomeDir    = filesystem.GetUserHomeDirectory()
	defaultDataDir    = filepath.Join(defaultHomeDir, defaultDataDirName)
	defaultConfigFile = filepath.Join(defaultHomeDir, defaultConfigFileName)
	defaultLogDir     = filepath.Join(defaultHomeDir, defaultLogFileName)
	defaultAccountDir = filepath.Join(defaultHomeDir, defaultAccountFileName)
	defaultTestMode   = false
)

// Config defines the top level configuration for a spacemesh node
type Config struct {
	BaseConfig `mapstructure:"main"`
	P2P        p2pConfig.Config       `mapstructure:"p2p"`
	API        apiConfig.Config       `mapstructure:"api"`
	CONSENSUS  consensusConfig.Config `mapstructure:"consensus"`
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
}

// DefaultConfig returns the default configuration for a spacemesh node
func DefaultConfig() Config {
	return Config{
		BaseConfig: defaultBaseConfig(),
		P2P:        p2pConfig.DefaultConfig(),
		API:        apiConfig.DefaultConfig(),
		CONSENSUS:  consensusConfig.DefaultConfig(),
	}
}

// DefaultBaseConfig returns a default configuration for spacemesh
func defaultBaseConfig() BaseConfig {
	return BaseConfig{
		HomeDir:        defaultHomeDir,
		DataDir:        defaultDataDir,
		ConfigFile:     defaultConfigFileName,
		LogDir:         defaultLogDir,
		AccountDir:     defaultAccountDir,
		TestMode:       defaultTestMode,
		CollectMetrics: false,
		MetricsPort:    1010,
	}
}

// LoadConfig load the config file
func LoadConfig(fileLocation string, vip *viper.Viper) (err error) {
	log.Info("Parsing config file at location: %s", fileLocation)

	if fileLocation == "" {
		fileLocation = defaultConfigFileName
	}

	vip.SetConfigFile(fileLocation)
	err = vip.ReadInConfig()

	if err != nil {
		if fileLocation != defaultConfigFileName {
			log.Warning("failed loading %v trying %v", fileLocation, defaultConfigFileName)
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
