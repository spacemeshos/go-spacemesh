package config

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/mitchellh/mapstructure"
	"github.com/spacemeshos/go-spacemesh/cmd/mapstructureutil"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"

	"github.com/spacemeshos/go-spacemesh/filesystem"
)

const (
	ExtraDataText        = "mainnet"
	GenesisTimeText      = "2019-02-13T17:02:00+00:00"
	ExpectedHash         = "0x171f780f0fa34038e92473d3c9bf6af39e75d25d92534b29bdbb4de9436ab6c8"
	ExpectedGenesisId    = "0x1e459123cf26ee0de56d9e2ef6a30f8317c797e1"
	ExpectedGoldenATXID  = "0x1e459123cf26ee0de56d9e2ef6a30f8317c797e1000000000000000000000000"
	ExpectedP2PNetworkId = "0x1e459123"
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
	config.Genesis = DefaultTestGenesisConfig()
	assert.Equal(t, config.Genesis.ExtraData, ExtraDataText)
	assert.Equal(t, config.Genesis.GenesisTime, GenesisTimeText)
	assert.Equal(t, config.Genesis.GoldenATXID, ExpectedGoldenATXID)
}

func TestCorrectGenesisValuesFromFile(t *testing.T) {

	vip := viper.New()

	cfg_read_err := LoadConfig("../config.toml", vip)
	assert.Equal(t, nil, cfg_read_err)

	cfg := &Config{}

	hook := mapstructure.ComposeDecodeHookFunc(
		mapstructure.StringToTimeDurationHookFunc(),
		mapstructure.StringToSliceHookFunc(","),
		mapstructureutil.BigRatDecodeFunc(),
	)
	gen_cfg := &GenesisConfig{}

	err := vip.Unmarshal(&cfg, viper.DecodeHook(hook))
	gen_vip_err := vip.UnmarshalKey("genesis", &gen_cfg)
	assert.Equal(t, gen_vip_err, nil)

	_, json_err := json.Marshal(cfg)
	assert.Equal(t, nil, json_err)
	gen_cfg.GoldenATXID = string(gen_cfg.CalcGoldenATX().Hash32().Hex())
	gen_cfg.CalcGenesisID()
	cfg.Genesis = gen_cfg
	assert.Equal(t, err, nil)
	assert.Equal(t, cfg.Genesis.ExtraData, ExtraDataText)
	assert.Equal(t, cfg.Genesis.GenesisTime, GenesisTimeText)
	assert.Equal(t, cfg.Genesis.GoldenATXID, ExpectedGoldenATXID)

	cfg.NetworkIdFromGenesis(cfg.Genesis)
	assert.Equal(t, util.BytesToUint32(util.FromHex(ExpectedP2PNetworkId)), cfg.P2P.NetworkID)
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

func TestMismatchedGenesisConfigsRaiseException(t *testing.T) {
	cfg1 := DefaultTestGenesisConfig()
	cfg1.GenesisTime = GenesisTimeText
	cfg1.ExtraData = ExtraDataText
	cfg1.GoldenATXID = cfg1.CalcGoldenATX().Hash32().Hex()

	cfg2 := DefaultGenesisConfig()
	_, err := cfg2.Compare(*cfg1)
	//print("Compare result: ", res)
	assert.Error(t, err)
}
