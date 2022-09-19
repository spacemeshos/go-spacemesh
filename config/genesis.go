package config

import (
	"fmt"
	"path/filepath"
	"time"

	apiConfig "github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hash"

	"github.com/google/go-cmp/cmp"
	"github.com/spf13/viper"
)

const (
	ExtraDataLen                 = 255
	GoldenATXIDLen               = 32
	GenesisIDLen                 = 20
	SaveToFileName               = "GenesisDataChecksum"
	DefaultGenesisConfigFileDir  = "data"
	DefaultGenesisConfigFileName = "genesis.conf"
)

var DefaultGenesisDataDir = filepath.Join(defaultDataDir, DefaultGenesisConfigFileDir, "/")

type GenesisConfig struct {
	Accounts    *apiConfig.GenesisAccountConfig `mapstructure:"genesis-api"`
	GenesisTime string                          `mapstructure:"genesis-time"`
	GoldenATXID string                          `mapstructure:"golden-atx"`
	ExtraData   string                          `mapstructure:"genesis-extra-data"`
	GenesisID   string
}

type GenesisConfigResolved struct {
	ExtraData   [255]byte
	GoldenATXID types.ATXID
	GenesisTime string
	GenesisID   types.Hash20
}

func GenesisViper() *viper.Viper {
	genesisVip := viper.New()
	return genesisVip
}

func defaultGenesisConfig() *GenesisConfig {
	return &GenesisConfig{
		Accounts:    apiConfig.DefaultGenesisAccountConfig(),
		GenesisTime: DefaultGenesisTime(),
		GoldenATXID: DefaultGoldenATXId().Hash32().Hex(),
		ExtraData:   DefaultGenesisExtraData(),
	}
}

func DefaultTestnetGenesisConfig() *GenesisConfig {
	return &GenesisConfig{
		Accounts: &apiConfig.GenesisAccountConfig{
			Accounts: map[string]uint64{
				"stest1qqqqqqygdpsq62p4qxfyng8h2mm4f4d94vt7huqqu9mz3": 100000000000000000,
				"stest1qqqqqqylzg8ypces4llx4gnat0dyntqfvr0h6mcprcz66": 100000000000000000,
				"stest1qqqqqq90akdpc97206485eu4m0rmacd3mxfv0wsdrea6k": 100000000000000000,
				"stest1qqqqqq9jpsarr7tnyv0qr0edddwqpg3vcya4cccauypts": 100000000000000000,
				"stest1qqqqqq8lpq7f5ghqt569nvpl8kldv8r66ms2yzgudsd5t": 100000000000000000,
			},
		},
		GenesisTime: DefaultTestnetGenesisTime(),
		GoldenATXID: DefaultGoldenATXId().Hash32().Hex(),
		ExtraData:   DefaultTestnetGenesisExtraData(),
	}
}

func DefaultTestGenesisConfig() *GenesisConfig {
	cfg := &GenesisConfig{
		Accounts:    apiConfig.DefaultTestGenesisAccountConfig(),
		GenesisTime: DefaultTestGenesisTime(),
		GoldenATXID: DefaultTestGoldenATXId().Hash32().Hex(),
		ExtraData:   DefaultGenesisExtraData(),
	}
	cfg.GenesisID = CalcGenesisId([]byte(cfg.ExtraData), cfg.GenesisTime).Hex()
	return cfg
}

func (gc *GenesisConfig) Compare(payload *GenesisConfig) error {
	if diff := cmp.Diff(payload, gc); diff != "" {
		return fmt.Errorf("config files mismatches (-want +got):\n%s", diff)
	}
	return nil
}

func DefaultTestGenesisTime() string {
	return "2022-12-25T00:00:00+00:00"
}

func DefaultTestnetGenesisTime() string {
	return DefaultTestGenesisTime()
}

func DefaultGenesisTime() string {
	return time.Now().Format(time.RFC3339)
}

func DefaultGoldenATXId() types.ATXID {
	return types.ATXID(CalcGoldenATX([]byte(DefaultGenesisExtraData()), DefaultGenesisTime()))
}

func DefaultTestGoldenATXId() types.ATXID {
	return types.ATXID(CalcGoldenATX([]byte(DefaultTestGenesisExtraData()), DefaultTestGenesisTime()))
}

func DefaultTestnetGoldenATXId() types.ATXID {
	return types.ATXID(CalcGoldenATX([]byte(DefaultTestnetGenesisExtraData()), DefaultTestnetGenesisTime()))
}

func DefaultTestnetGenesisExtraData() string {
	return "testnet"
}

func DefaultTestGenesisExtraData() string {
	return DefaultGenesisExtraData()
}

func DefaultGenesisExtraData() string {
	return "mainnet"
}

func CalcGenesisId(genesisExtraData []byte, genesisTime string) types.Hash20 {
	dataLen := len(genesisExtraData)
	genesisTimeB := []byte(genesisTime)
	hasher := hash.New()
	hasher.Write(genesisTimeB)
	if dataLen < ExtraDataLen {
		padded := make([]byte, ExtraDataLen)
		copy(padded[0:dataLen], genesisExtraData)
		hasher.Write(padded)
	} else {
		hasher.Write(genesisExtraData)
	}
	digest := hasher.Sum([]byte{})
	return types.BytesToHash(digest).ToHash20()
}

func (cfg *GenesisConfig) CalcGenesisID() {
	cfg.GenesisID = CalcGenesisId([]byte(cfg.ExtraData), cfg.GenesisTime).Hex()
}

func CalcGoldenATX(genesisData []byte, genesisTime string) types.Hash32 {
	return CalcGenesisId([]byte(genesisData), genesisTime).ToHash32()
}

func (cfg *GenesisConfig) CalcGoldenATX() {
	cfg.GoldenATXID = types.ATXID(CalcGoldenATX([]byte(cfg.ExtraData), cfg.GenesisTime)).Hash32().Hex()
}
