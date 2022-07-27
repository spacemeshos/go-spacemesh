package config

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/types/address"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/signing"
)

// GenesisConfig defines accounts that will exist in state at genesis.
type GenesisConfig struct {
	NetworkID address.Network
	Accounts  map[string]uint64 `mapstructure:"accounts"`
}

// ToAccounts creates list of types.Account instance from config.
func (g *GenesisConfig) ToAccounts() []types.Account {
	var rst []types.Account
	for addr, balance := range g.Accounts {
		addrBytes := util.FromHex(addr)
		addrGenerated, err := address.GenerateAddress(g.NetworkID, addrBytes)
		if err != nil {
			panic(fmt.Sprintf("could not generate address from string `%s`: %s", addr, err.Error()))
		}
		rst = append(rst, types.Account{
			Address: addrGenerated,
			Balance: balance,
		})
	}
	return rst
}

// Account1Address is the address from Account1Private.
const Account1Address = "0x0002abedccd3ffcbf46f35f11b314d17c05a2905f918d0d7"

// Account1Private is the private key for test account.
const Account1Private = "0x2dcddb8e0ddd2269f536da5768e890790f2b84366e0fb8396bdcd15c0d7c30b90002abedccd3ffcbf46f35f11b314d17c05a2905f918d0d72f2f6989640fbb43"

// Account2Address is the address from Account2Address.
const Account2Address = "0xfeaf3d38b6ef430933ebedeb073af7bec018e8d2e379fa47"

// Account2Private is the private key for secode test account.
const Account2Private = "0x0bb3f2936d42f463e597f5fb2c48bbd8475ce74ba91f1eaae97df4084d306b49feaf3d38b6ef430933ebedeb073af7bec018e8d2e379fa47df6a9fa07a6a8344"

// DefaultGenesisConfig is the default configuration for the node.
func DefaultGenesisConfig() *GenesisConfig {
	// NOTE(dshulyak) keys in default config are used in some tests
	g := GenesisConfig{
		NetworkID: address.MainnetID,
	}

	// we default to 10^5 SMH per account which is 10^17 smidge
	// each genesis account starts off with 10^17 smidge
	g.Accounts = map[string]uint64{
		Account1Address: 100000000000000000,
		Account2Address: 100000000000000000,
	}
	return &g
}

// DefaultTestGenesisConfig is the default test configuration for the node.
func DefaultTestGenesisConfig() *GenesisConfig {
	// NOTE(dshulyak) keys in default config are used in some tests
	g := GenesisConfig{
		NetworkID: address.TestnetID,
	}

	acc1Signer, err := signing.NewEdSignerFromBuffer(util.FromHex(Account1Private))
	if err != nil {
		panic("could not build ed signer")
	}

	acc2Signer, err := signing.NewEdSignerFromBuffer(util.FromHex(Account2Private))
	if err != nil {
		panic("could not build ed signer")
	}
	addr1, err := address.GenerateAddress(address.TestnetID, acc1Signer.PublicKey().Bytes())
	if err != nil {
		panic("could not generate address: " + err.Error())
	}
	addr2, err := address.GenerateAddress(address.TestnetID, acc2Signer.PublicKey().Bytes())
	if err != nil {
		panic("could not generate address: " + err.Error())
	}

	// we default to 10^5 SMH per account which is 10^17 smidge
	// each genesis account starts off with 10^17 smidge
	g.Accounts = map[string]uint64{
		addr1.String(): 100000000000000000,
		addr2.String(): 100000000000000000,
	}

	return &g
}
