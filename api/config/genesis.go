package config

import (
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/common"
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
		address.BytesToAddress(common.FromHex("0x7be017a967db77fd10ac7c891b3d6d946dea7e3e14756e2f0f9e09b9663f0d9c")): {Balance: big.NewInt(10000), Nonce: 0},
	}
	// public  0x7be017a967db77fd10ac7c891b3d6d946dea7e3e14756e2f0f9e09b9663f0d9c
	// private 0x81c90dd832e18d1cf9758254327cb3135961af6688ac9c2a8c5d71f73acc5ce57be017a967db77fd10ac7c891b3d6d946dea7e3e14756e2f0f9e09b9663f0d9c
	return &g
	//todo: implement reading from file
}
