package config

import (
	"errors"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
)

const (
	DefaultGenesisConfigFileName = "genesis.conf"
	defaultGenesisExtraData      = "mainnet"

	account1Private = "0x2dcddb8e0ddd2269f536da5768e890790f2b84366e0fb8396bdcd15c0d7c30b90002abedccd3ffcbf46f35f11b314d17c05a2905f918d0d72f2f6989640fbb43"
	account2Private = "0x0bb3f2936d42f463e597f5fb2c48bbd8475ce74ba91f1eaae97df4084d306b49feaf3d38b6ef430933ebedeb073af7bec018e8d2e379fa47df6a9fa07a6a8344"
)

type GenesisConfig struct {
	Accounts    map[string]uint64 `mapstructure:"accounts"`
	GenesisTime string            `mapstructure:"genesis-time"`
	ExtraData   string            `mapstructure:"genesis-extradata"`
}

func defaultGenesisConfig() *GenesisConfig {
	return &GenesisConfig{
		Accounts:    GenerateDefaultGenesisAccounts(),
		GenesisTime: time.Now().Format(time.RFC3339),
		ExtraData:   defaultGenesisExtraData,
	}
}

func GenerateDefaultGenesisAccounts() map[string]uint64 {
	genesisTime := time.Now().Format(time.RFC3339)
	acc1Signer, err := signing.NewEdSignerFromBuffer(util.FromHex(account1Private), CalcGenesisID(defaultGenesisExtraData, genesisTime))
	if err != nil {
		panic("could not build ed signer")
	}

	acc2Signer, err := signing.NewEdSignerFromBuffer(util.FromHex(account2Private), CalcGenesisID(defaultGenesisExtraData, genesisTime))
	if err != nil {
		panic("could not build ed signer")
	}

	// we default to 10^8 SMH per account which is 10^17 smidge
	// each genesis account starts off with 10^17 smidge
	return map[string]uint64{
		types.GenerateAddress(acc1Signer.PublicKey().Bytes()).String(): 100000000000000000,
		types.GenerateAddress(acc2Signer.PublicKey().Bytes()).String(): 100000000000000000,
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

func (gc *GenesisConfig) Compare(stored *GenesisConfig, path string, lg log.Log) error {
	if diff := cmp.Diff(stored, gc); diff != "" {
		lg.Error("Genesis config %s changed from previous run (-want +got):\n%s", path, diff)
		return errors.New("failed to match config file")
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

func (gc *GenesisConfig) GenesisID() uint64 {
	return CalcGenesisID(gc.ExtraData, gc.GenesisTime).Big().Uint64()
}
