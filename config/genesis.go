package config

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/google/go-cmp/cmp"
	apiConfig "github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spf13/viper"
)

const (
	ExtraDataLen   = 255
	GoldenATXIDLen = 32
	GenesisIDLen   = 20
	SaveToFileName = "GenesisDataChecksum"
)

var (
	DefaultGenesisDataPath = filepath.Join(defaultHomeDir, defaultDataDirName, "/genesis.conf")
)

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

func DefaultGenesisConfig() *GenesisConfig {
	return &GenesisConfig{
		Accounts:    apiConfig.DefaultGenesisAccountConfig(),
		GenesisTime: DefaultGenesisTime(),
		GoldenATXID: DefaultGoldenATXId().Hash32().Hex(),
		ExtraData:   DefaultGenesisExtraData(),
	}
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

	if genesisId, err := CalcGenesisId([]byte(cfg.ExtraData), cfg.GenesisTime); err == nil {
		cfg.GenesisID = genesisId.Hex()
	} else {
		cfg.GenesisID = ""
	}

	return cfg
}

func (conf *GenesisConfig) Compare(cfg GenesisConfig) (string, error) {
	if isEqual := cmp.Equal(conf, cfg); isEqual {
		print("CFGs are Equal!")
		if d, err := json.Marshal(conf); err != nil {
			return "", err
		} else {
			return string(d), nil
		}
	} else {
		return "", fmt.Errorf("genesis config mismatch")
	}
}

func DefaultTestGenesisTime() string {
	return "2019-02-13T17:02:00+00:00"
}

func DefaultTestnetGenesisTime() string {
	return DefaultTestGenesisTime()
}
func DefaultGenesisTime() string {
	return time.Now().Format(time.RFC3339)
}

func DefaultGoldenATXId() types.ATXID {
	genesis_time := DefaultGenesisTime()
	genesis_extra_data := []byte(DefaultGenesisExtraData())
	if id, err := CalcGoldenATX(genesis_extra_data, genesis_time); err == nil {
		return types.ATXID(id)
	} else {
		return *types.EmptyATXID
	}
}

func DefaultTestGoldenATXId() types.ATXID {
	genesis_time := DefaultTestGenesisTime()
	genesis_extra_data := []byte(DefaultTestGenesisExtraData())
	if id, err := CalcGoldenATX(genesis_extra_data, genesis_time); err == nil {
		return types.ATXID(id)
	} else {
		return *types.EmptyATXID
	}
}

func DefaultTestnetGoldenATXId() types.ATXID {
	genesis_time := DefaultTestnetGenesisTime()
	genesis_extra_data := []byte(DefaultTestnetGenesisExtraData())
	if id, err := CalcGoldenATX(genesis_extra_data, genesis_time); err == nil {
		return types.ATXID(id)
	} else {
		return *types.EmptyATXID
	}
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

func (cfg *GenesisConfig) Validate() (err error) {
	return nil
}

func (cfg *GenesisConfig) SaveToFile() (file_path string, err error) {
	return "", nil
}

func CalcGenesisId(genesis_extra_data []byte, genesis_time string) (types.Hash20, error) {
	data_len := len(genesis_extra_data)
	genesis_time_b := []byte(genesis_time)
	if data_len < ExtraDataLen {
		padded := make([]byte, ExtraDataLen)
		copy(padded[0:data_len], genesis_extra_data)
		hasher := hash.New()

		// use hasher versus Hash32(concatenation of genesis_time + genesis_extra_data)
		hasher.Write(genesis_time_b) // genesis ID = hash(genesis_time + genesis_extra_data)
		hasher.Write(padded)
		digest := hasher.Sum([]byte{})
		return types.BytesToHash(digest).ToHash20(), nil
	} else {
		hasher := hash.New()
		hasher.Write(genesis_time_b)
		hasher.Write(genesis_extra_data)
		// full_b := make([]byte, len(genesis_time_b)+len(genesis_extra_data))
		// copy(full_b[:], genesis_time_b)
		// copy(full_b[len(genesis_time_b):], genesis_extra_data)
		// hasher.Write(full_b)
		digest := hasher.Sum([]byte{})
		return types.BytesToHash(digest).ToHash20(), nil
	}
}

func (cfg *GenesisConfig) CalcGoldenATX() types.ATXID {
	genesis_time := cfg.GenesisTime
	genesis_data := cfg.ExtraData

	if id, err := CalcGoldenATX([]byte(genesis_data), genesis_time); err == nil {
		return types.ATXID(id)
	} else {
		return *types.EmptyATXID
	}
}

func (cfg *GenesisConfig) CalcGenesisID() {
	gen_id, _ := CalcGenesisId([]byte(cfg.ExtraData), cfg.GenesisTime)

	cfg.GenesisID = gen_id.Hex()
}

func CalcGoldenATX(genesis_data []byte, genesis_time string) (types.Hash32, error) {
	if id, err := CalcGenesisId([]byte(genesis_data), genesis_time); err == nil {
		return id.ToHash32(), nil
	} else {
		return types.Hash32{}, err
	}
}
