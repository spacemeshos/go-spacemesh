package config

import (
	"github.com/spf13/viper"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/util"
	p2pConfig "github.com/spacemeshos/go-spacemesh/p2p/nodeconfig"
	apiConfig "github.com/spacemeshos/go-spacemesh/api/config"
	"path/filepath"
)

const (
	defaultConfigFileName = "config.toml"
	defaultLogFileName = "spacemesh.log"
	defaultAccountFileName = "accounts"
	defaultDataDirName = "spacemesh"
	defaultAppIntParam = 20
	defaultAppBoolParam = 20
)

var (
	defaultHomeDir = util.AppDataDir("spacemesh", false)
	defaultDataDir = filepath.Join(defaultHomeDir, defaultDataDirName)
	defaultConfigFile = filepath.Join(defaultHomeDir, defaultConfigFileName)
	defaultLogDir = filepath.Join(defaultHomeDir, defaultLogFileName)
	defaultAccountDir = filepath.Join(defaultHomeDir, defaultAccountFileName)
)

// Config defines the top level configuration for a spacemesh node
type Config struct {
	BaseConfig	`mapstructure:"main"`
	P2P *p2pConfig.Config `mapstructure:"p2p"`
	API *apiConfig.Config `mapstructure:"api"`
}

// BaseConfig defines the default configuration options for spacemesh app
type BaseConfig struct {
	
	//
	HomeDir string 
	
	//
	DataDir string
	
	//
	ConfigFile string

	//
	LogDir string
	
	//
	AccountDir string
}

// DefaultConfig returns the default configuration for a spacemesh node
func DefaultConfig() *Config {
	return &Config{
		BaseConfig: defaultBaseConfig(),
		P2P: p2pConfig.DefaultConfig(),
		API: apiConfig.DefaultConfig(),
	}
}

// DefaultBaseConfig returns a default configuration for spacemesh
func defaultBaseConfig() BaseConfig {
	return BaseConfig{
		HomeDir: defaultHomeDir,
		DataDir: defaultDataDir,
		ConfigFile: defaultConfigFile,
		LogDir: defaultLogDir,
		AccountDir: defaultAccountDir,
	}
}

// LoadConfig sets
func LoadConfig(defaultConfig *config.BaseConfig, fileLocation string)(*BaseConfig, error){
	fmt.Println("Parsing config file at path:", filepath)
	if filepath != "" {
		filename := filepath.Base(fileLocation)
		viper.SetConfigName(filename[:len(filename)-len(filepath.Ext(filename))])
		viper.AddConfigPath(filepath.Dir(config))

		err := viper.ReadConfig()
		if err != nil {
			return fmt.Errorf("Failed to read config file", err)
		}
	}

}

// SetConfigFile overrides the default config file path
func (cfg *BaseConfig) SetConfigFile(file string) {
	cfg.ConfigFile = file
}