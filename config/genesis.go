package config

import (
	"errors"
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
	Accounts    *apiConfig.GenesisAccountConfig `mapstructure:"genesis-api"`
	GenesisTime string                          `mapstructure:"genesis-time"`
	ExtraData   string                          `mapstructure:"genesis-extradata"`
}

func defaultGenesisConfig() *GenesisConfig {
	return &GenesisConfig{
		Accounts:    apiConfig.DefaultGenesisAccountConfig(),
		GenesisTime: DefaultGenesisTime(),
		ExtraData:   DefaultGenesisExtraData(),
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
	}
}

func DefaultTestGenesisConfig() *GenesisConfig {
	return &GenesisConfig{
		Accounts:    apiConfig.DefaultTestGenesisAccountConfig(),
		GenesisTime: DefaultTestGenesisTime(),
		ExtraData:   DefaultTestGenesisExtraData(),
	}
}

func (gc *GenesisConfig) Compare(stored *GenesisConfig, path string) error {
	if diff := cmp.Diff(stored, gc); diff != "" {
		fmt.Printf("Genesis config %s changed from previous run (-want +got):\n%s", path, diff)
		return errors.New("failed to match config files")
	}
	return nil
}

func GenesisDataDir(dataDirParent string) string {
	return filepath.Join(dataDirParent, DefaultGenesisConfigFileDir)
}

func DefaultGenesisTime() string {
	return time.Now().Format(time.RFC3339)
}

func DefaultGenesisExtraData() string {
	return "mainnet"
}

func DefaultGoldenATXId() types.ATXID {
	return types.ATXID(CalcGenesisID(DefaultGenesisExtraData(), DefaultGenesisTime()).ToHash32())
}

func DefaultTestGenesisTime() string {
	return "2022-12-25T00:00:00+00:00"
}

func DefaultTestGenesisExtraData() string {
	return DefaultGenesisExtraData()
}

func DefaultTestGoldenATXId() types.ATXID {
	return types.ATXID(CalcGenesisID(DefaultTestGenesisExtraData(), DefaultTestGenesisTime()).ToHash32())
}

func DefaultTestnetGenesisTime() string {
	return DefaultTestGenesisTime()
}

func DefaultTestnetGenesisExtraData() string {
	return "testnet"
}

func DefaultTestnetGoldenATXId() types.ATXID {
	return types.ATXID(CalcGenesisID(DefaultTestnetGenesisExtraData(), DefaultTestnetGenesisTime()).ToHash32())
}

func CalcGenesisID(genesisExtraData, genesisTime string) types.Hash20 {
	hasher := hash.New()
	hasher.Write([]byte(genesisTime))
	hasher.Write([]byte(genesisExtraData))
	digest := hasher.Sum([]byte{})
	return types.BytesToHash(digest).ToHash20()
}
