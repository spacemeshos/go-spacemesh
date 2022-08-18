package config

import (
	"time"

	apiConfig "github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spf13/viper"
)

type GenesisConfig struct {
	Accounts    *apiConfig.GenesisAccountConfig `mapstructure:"genesis-api"`
	GenesisTime string                          `mapstructure:"genesis-time"`
	GoldenATXID string                          `mapstructure:"golden-atx"`
	ExtraData   string                          `mapstructure:"genesis-extra-data"`
}

func GenesisViper() *viper.Viper {
	genesisVip := viper.New()
	return genesisVip
}

func DefaultGenesisConfig() *GenesisConfig {
	return &GenesisConfig{
		Accounts:    apiConfig.DefaultGenesisAccountConfig(),
		GenesisTime: DefaultTestGenesisTime(),
		GoldenATXID: DefaultGoldenATXId(),
		ExtraData:   DefaultGenesisExtraData(),
	}
}

func DefaultTestGenesisTime() string {
	return time.Now().Format(time.RFC3339)
}
func DefaultGoldenATXId() string {
	return "0x5678"
}

func DefaultGenesisExtraData() string {
	return "mainnet"
}
func DefaultTestnetGenesisConfig() *GenesisConfig {
	//accountConfig := &apiConfig
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
		GenesisTime: DefaultTestGenesisTime(),
		GoldenATXID: DefaultGoldenATXId(),
	}
}

func DefaultTestGenesisConfig() *GenesisConfig {
	return &GenesisConfig{
		Accounts:    apiConfig.DefaultTestGenesisAccountConfig(),
		GenesisTime: DefaultTestGenesisTime(),
		GoldenATXID: DefaultGoldenATXId(),
		ExtraData:   DefaultGenesisExtraData(),
	}
}
