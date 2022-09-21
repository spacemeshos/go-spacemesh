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
	"github.com/spacemeshos/go-spacemesh/log"
)

const (
	DefaultGenesisConfigFileDir  = "data"
	DefaultGenesisConfigFileName = "genesis.conf"
	defaultGenesisExtraData      = "mainnet"

	DefaultTestGenesisTime      = "2022-12-25T00:00:00+00:00"
	defaultTestGenesisExtraData = "test"
)

type GenesisConfig struct {
	Accounts    map[string]uint64 `mapstructure:"accounts"`
	GenesisTime string            `mapstructure:"genesis-time"`
	ExtraData   string            `mapstructure:"genesis-extradata"`
}

func defaultGenesisConfig() *GenesisConfig {
	return &GenesisConfig{
		Accounts:    apiConfig.DefaultGenesisAccounts(),
		GenesisTime: time.Now().Format(time.RFC3339),
		ExtraData:   defaultGenesisExtraData,
	}
}

func GenesisDataDir(dataDirParent string) string {
	return filepath.Join(dataDirParent, DefaultGenesisConfigFileDir)
}

func DefaultTestGenesisConfig() *GenesisConfig {
	return &GenesisConfig{
		Accounts:    apiConfig.DefaultTestGenesisAccounts(),
		GenesisTime: DefaultTestGenesisTime,
		ExtraData:   defaultTestGenesisExtraData,
	}
}

// ToAccountList creates list of types.Account instance from config.
func (g *GenesisConfig) ToAccountList() []types.Account {
	var rst []types.Account
	for addr, balance := range g.Accounts {
		genesisAddr, err := types.StringToAddress(addr)
		if err != nil {
			log.Panic("could not create address from genesis config `%s`: %s", addr, err.Error())
		}
		rst = append(rst, types.Account{
			Address: genesisAddr,
			Balance: balance,
		})
	}
	return rst
}

func (gc *GenesisConfig) Compare(stored *GenesisConfig, path string) error {
	if diff := cmp.Diff(stored, gc); diff != "" {
		fmt.Printf("Genesis config %s changed from previous run (-want +got):\n%s", path, diff)
		return errors.New("failed to match config files")
	}
	return nil
}

func CalcGenesisID(genesisExtraData, genesisTime string) types.Hash20 {
	hasher := hash.New()
	hasher.Write([]byte(genesisTime))
	hasher.Write([]byte(genesisExtraData))
	digest := hasher.Sum([]byte{})
	return types.BytesToHash(digest).ToHash20()
}
