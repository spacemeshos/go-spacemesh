package config

import (
	"fmt"
	apiConfig "github.com/spacemeshos/go-spacemesh/api/config"
	consensusConfig "github.com/spacemeshos/go-spacemesh/consensus/config"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/log"
	p2pConfig "github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
	"github.com/spf13/viper"
	"path/filepath"
)

const (
	defaultConfigFileName  = "config.toml"
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
		HomeDir:    defaultHomeDir,
		DataDir:    defaultDataDir,
		ConfigFile: defaultConfigFile,
		LogDir:     defaultLogDir,
		AccountDir: defaultAccountDir,
	}
}

// LoadConfig load the config file
func LoadConfig(fileLocation string) (err error) {
	log.Info("Parsing config file at location: %s", fileLocation)

	if fileLocation != "" {
		filename := filepath.Base(fileLocation)

		slice := len(filename) - len(filepath.Ext(filename))

		viper.SetConfigName(filename[:slice])
		viper.AddConfigPath(filepath.Dir(filename))
		viper.AddConfigPath(".")
		viper.AddConfigPath("../")

		err = viper.ReadInConfig()

		if err != nil {
			return fmt.Errorf("failed to read config file %v", err)
		}

		return nil
	}

	return fmt.Errorf("failed to read config file")
}

// SetConfigFile overrides the default config file path
func (cfg *BaseConfig) SetConfigFile(file string) {
	cfg.ConfigFile = file
}
