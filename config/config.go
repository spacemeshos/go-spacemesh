package config

import (
	"fmt"
	apiConfig "github.com/spacemeshos/go-spacemesh/api/config"
	consensusConfig "github.com/spacemeshos/go-spacemesh/consensus/config"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/log"
	p2pConfig "github.com/spacemeshos/go-spacemesh/p2p/config"
	timeConfig "github.com/spacemeshos/go-spacemesh/timesync/config"
	"github.com/spf13/viper"
	"path/filepath"
)

const (
	defaultConfigFileName  = "./config.toml"
	defaultDataDirName     = "spacemesh"
	defaultAppIntParam     = 20
	defaultAppBoolParam    = 20
)

var (
	defaultHomeDir    = filesystem.GetUserHomeDirectory()
	defaultDataDir    = filepath.Join(defaultHomeDir, defaultDataDirName)
)

// Config defines the top level configuration for a spacemesh node
type Config struct {
	BaseConfig `mapstructure:"main"`
	P2P        p2pConfig.Config       `mapstructure:"p2p"`
	API        apiConfig.Config       `mapstructure:"api"`
	CONSENSUS  consensusConfig.Config `mapstructure:"consensus"`
	TimeConfig timeConfig.Config
}

// BaseConfig defines the default configuration options for spacemesh app
type BaseConfig struct {
	DataDir string `mapstructure:"data-folder"`

}

// DefaultConfig returns the default configuration for a spacemesh node
func DefaultConfig() Config {
	return Config{
		BaseConfig: defaultBaseConfig(),
		P2P:        p2pConfig.DefaultConfig(),
		API:        apiConfig.DefaultConfig(),
		CONSENSUS:  consensusConfig.DefaultConfig(),
		TimeConfig: timeConfig.DefaultConfig(),
	}
}

// DefaultBaseConfig returns a default configuration for spacemesh
func defaultBaseConfig() BaseConfig {
	return BaseConfig{
		DataDir:    defaultDataDir,
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
