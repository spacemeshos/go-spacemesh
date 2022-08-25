package config

import (
	"path/filepath"
	"testing"

	"github.com/mitchellh/mapstructure"
	"github.com/spacemeshos/go-spacemesh/cmd/mapstructureutil"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"

	"github.com/spacemeshos/go-spacemesh/filesystem"
)

const (
	ExtraDataText       = "mainnet"
	GenesisTimeText     = "2019-02-13T17:02:00+00:00"
	ExpectedHash        = "0x171f780f0fa34038e92473d3c9bf6af39e75d25d92534b29bdbb4de9436ab6c8"
	ExpectedGenesisId   = "0x1e459123cf26ee0de56d9e2ef6a30f8317c797e1"
	ExpectedGoldenATXID = "0x1e459123cf26ee0de56d9e2ef6a30f8317c797e1000000000000000000000000"
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
	assert.Equal(t, config.Genesis.ExtraData, ExtraDataText)
	assert.Equal(t, config.Genesis.GenesisTime, GenesisTimeText)
	assert.Equal(t, config.Genesis.GoldenATXID, ExpectedGoldenATXID)
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
	assert.Equal(t, cfg.Genesis.ExtraData, ExtraDataText)
	assert.Equal(t, cfg.Genesis.GenesisTime, GenesisTimeText)
	assert.Equal(t, cfg.Genesis.GoldenATXID, ExpectedGoldenATXID)

	cfg.NetworkIdFromGenesis(cfg.Genesis)
	assert.Equal(t, uint32(1), cfg.P2P.NetworkID)
}

func TestCorrectGenesisIDGeneration(t *testing.T) {
	cfg := DefaultGenesisConfig()
	cfg.GenesisTime = GenesisTimeText
	cfg.ExtraData = ExtraDataText
	cfg.GoldenATXID = cfg.CalcGoldenATX().Hash32().Hex()
	gen_id, err := CalcGenesisId([]byte(cfg.ExtraData), GenesisTimeText)
	assert.Equal(t, err, nil)
	assert.Equal(t, cfg.GoldenATXID != types.ATXID{}.Hash32().Hex(), true)
	print(gen_id.Hex())
	assert.Equal(t, gen_id.Hex(), ExpectedGenesisId)
}
