package config

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/spacemeshos/go-spacemesh/cmd/mapstructureutil"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"

	"github.com/spacemeshos/go-spacemesh/filesystem"
)

func TestLoadConfig(t *testing.T) {
	// todo: test more
	vip := viper.New()
	err := LoadConfig(".asdasda", vip)
	// verify that after attempting to load a non-existent file, an attempt is made to load the default config
	assert.EqualError(t, err, "failed to read config file open ./config.toml: no such file or directory")
}

func TestConfig_DataDir(t *testing.T) {
	sep := string(filepath.Separator)

	config := DefaultTestConfig()
	config.DataDirParent = "~" + sep + "space-a-mesh"
	config.P2P.NetworkID = 88
	expectedDataDir := filesystem.GetUserHomeDirectory() + sep + "space-a-mesh" + sep + "88"
	assert.Equal(t, expectedDataDir, config.DataDir())

	config.DataDirParent = "~" + sep + "space-a-mesh" + sep // trailing slash should be ignored
	assert.Equal(t, expectedDataDir, config.DataDir())
}

func TestDefaultConfigGenesisValues(t *testing.T) {
	config := DefaultTestConfig()
	assert.Equal(t, config.Genesis.ExtraData, "mainnet")
	assert.Equal(t, config.Genesis.GenesisTime, time.Now().Format(time.RFC3339))
	assert.Equal(t, config.Genesis.GoldenATXID, "0x5678")
}

func TestCorrectGenesisValuesFromFile(t *testing.T) {

	vip := viper.New()
	vip.AddConfigPath("./config.toml")
	cfg := DefaultConfig()

	hook := mapstructure.ComposeDecodeHookFunc(
		mapstructure.StringToTimeDurationHookFunc(),
		mapstructure.StringToSliceHookFunc(","),
		mapstructureutil.BigRatDecodeFunc(),
	)

	// load config if it was loaded to our viper
	err := vip.Unmarshal(&cfg, viper.DecodeHook(hook))
	assert.Equal(t, err, nil)
	assert.Equal(t, cfg.Genesis.ExtraData, "mainnet")
	assert.Equal(t, cfg.Genesis.GenesisTime, time.Now().Format(time.RFC3339))
	assert.Equal(t, cfg.Genesis.GoldenATXID, "0x5678")

	cfg.NetworkIdFromGenesis(cfg.Genesis)
	assert.Equal(t, uint32(1), cfg.P2P.NetworkID)
}
