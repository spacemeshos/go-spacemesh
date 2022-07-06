package config

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/types/address"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/signing"
)

// GenesisConfig defines accounts that will exist in state at genesis.
type GenesisConfig struct {
	Accounts map[string]uint64 `mapstructure:"accounts"`
}

// ToAccounts creates list of types.Account instance from config.
func (g *GenesisConfig) ToAccounts() []types.Account {
	var rst []types.Account
	for addr, balance := range g.Accounts {
		rst = append(rst, types.Account{
			Address: address.HexToAddress(addr),
			Balance: balance,
		})
	}
	return rst
}

// Account1Address is the address from Account1Private.
const Account1Address = "0x1b3d6d946dea7e3e14756e2f0f9e09b9663f0d9c"

// Account1Private is the private key for test account.
const Account1Private = "0x81c90dd832e18d1cf9758254327cb3135961af6688ac9c2a8c5d71f73acc5ce57be017a967db77fd10ac7c891b3d6d946dea7e3e14756e2f0f9e09b9663f0d9c"

// Account2Address is the address from Account2Address.
const Account2Address = "0xe77910b4419a4ee0f1d5483d7dd3b5b6b6922ee9"

// Account2Private is the private key for secode test account.
const Account2Private = "0x9d411020d46d3f4e1214f7b51052219737669f461ac9c9ac6ac49753926d0af222a31a84ab876f82fcafba86e77910b4419a4ee0f1d5483d7dd3b5b6b6922ee9"

// DefaultGenesisConfig is the default configuration for the node.
func DefaultGenesisConfig() *GenesisConfig {
	// NOTE(dshulyak) keys in default config are used in some tests
	g := GenesisConfig{}

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
	g := GenesisConfig{}

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
