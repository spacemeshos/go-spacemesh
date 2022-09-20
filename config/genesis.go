package config

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/google/go-cmp/cmp"

	apiConfig "github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hash"
)

const (
	ExtraDataLen                 = 255
	DefaultGenesisConfigFileDir  = "data"
	DefaultGenesisConfigFileName = "genesis.conf"
)

type GenesisConfig struct {
	Accounts    *apiConfig.GenesisAccountConfig `mapstructure:"accounts"`
	GenesisTime string                          `mapstructure:"genesis-time"`
	ExtraData   string                          `mapstructure:"genesis-extradata"`
	GoldenATXID string
	GenesisID   string
	Path        string
}

func defaultGenesisConfig() *GenesisConfig {
	return &GenesisConfig{
		Accounts:    apiConfig.DefaultGenesisAccountConfig(),
		GenesisTime: DefaultGenesisTime(),
		GoldenATXID: DefaultGoldenATXId().Hash32().Hex(),
		ExtraData:   DefaultGenesisExtraData(),
		Path:        DefaultGenesisDataDir(),
	}
}

func DefaultTestnetGenesisConfig() *GenesisConfig {
	return &GenesisConfig{
		Accounts: &apiConfig.GenesisAccountConfig{
			Data: map[string]uint64{
				"stest1qqqqqqygdpsq62p4qxfyng8h2mm4f4d94vt7huqqu9mz3": 100000000000000000,
				"stest1qqqqqqylzg8ypces4llx4gnat0dyntqfvr0h6mcprcz66": 100000000000000000,
				"stest1qqqqqq90akdpc97206485eu4m0rmacd3mxfv0wsdrea6k": 100000000000000000,
				"stest1qqqqqq9jpsarr7tnyv0qr0edddwqpg3vcya4cccauypts": 100000000000000000,
				"stest1qqqqqq8lpq7f5ghqt569nvpl8kldv8r66ms2yzgudsd5t": 100000000000000000,
			},
		},
		GenesisTime: DefaultTestnetGenesisTime(),
		ExtraData:   DefaultTestnetGenesisExtraData(),
		GoldenATXID: DefaultTestnetGoldenATXId().Hash32().Hex(),
		GenesisID:   CalcGenesisId([]byte(DefaultTestnetGenesisExtraData()), DefaultTestnetGenesisTime()).Hex(),
		Path:        DefaultGenesisDataDir(),
	}
}

func DefaultTestGenesisConfig() *GenesisConfig {
	return &GenesisConfig{
		Accounts:    apiConfig.DefaultTestGenesisAccountConfig(),
		GenesisTime: DefaultTestGenesisTime(),
		ExtraData:   DefaultTestGenesisExtraData(),
		GoldenATXID: DefaultTestGoldenATXId().Hash32().Hex(),
		GenesisID:   CalcGenesisId([]byte(DefaultTestGenesisExtraData()), DefaultTestGenesisTime()).Hex(),
		Path:        DefaultGenesisDataDir(),
	}
}

func (gc *GenesisConfig) Compare(payload *GenesisConfig) error {
	if diff := cmp.Diff(payload, gc); diff != "" {
		return fmt.Errorf("config files mismatch (-want +got):\n%s", diff)
	}
	return nil
}

func DefaultGenesisDataDir() string {
	return filepath.Join(defaultDataDir, DefaultGenesisConfigFileDir)
}

func DefaultGenesisTime() string {
	return time.Now().Format(time.RFC3339)
}

func DefaultGenesisExtraData() string {
	return "mainnet"
}

func DefaultGoldenATXId() types.ATXID {
	return types.ATXID(CalcGoldenATX([]byte(DefaultGenesisExtraData()), DefaultGenesisTime()))
}

func DefaultTestGenesisTime() string {
	return "2022-12-25T00:00:00+00:00"
}

func DefaultTestGenesisExtraData() string {
	return DefaultGenesisExtraData()
}

func DefaultTestGoldenATXId() types.ATXID {
	return types.ATXID(CalcGoldenATX([]byte(DefaultTestGenesisExtraData()), DefaultTestGenesisTime()))
}

func DefaultTestnetGenesisTime() string {
	return DefaultTestGenesisTime()
}

func DefaultTestnetGenesisExtraData() string {
	return "testnet"
}

func DefaultTestnetGoldenATXId() types.ATXID {
	return types.ATXID(CalcGoldenATX([]byte(DefaultTestnetGenesisExtraData()), DefaultTestnetGenesisTime()))
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
