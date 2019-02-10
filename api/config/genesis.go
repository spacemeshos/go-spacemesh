package config

import (
	"github.com/spacemeshos/go-spacemesh/address"
	"math/big"
)

type GenesisAccount struct {
	Balance *big.Int `json:"balance" gencodec:"required"`
	Nonce   uint64   `json:"nonce,omitempty"`
}

type GenesisConfig struct {
	InitialAccounts map[address.Address]GenesisAccount
}

func DefaultGenesisConfig() *GenesisConfig {
	g := GenesisConfig{}
	g.InitialAccounts = map[address.Address]GenesisAccount{
		address.BytesToAddress([]byte{1}): {Balance: big.NewInt(10000), Nonce: 0},
	}

	return &g
	//todo: implement reading from file
}
