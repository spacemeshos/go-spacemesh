package vesting

import (
	"github.com/spacemeshos/go-spacemesh/vm/genvm/core"
	"github.com/spacemeshos/go-spacemesh/vm/genvm/templates/multisig"
	"github.com/spacemeshos/go-spacemesh/vm/genvm/templates/vault"
)

//go:generate scalegen

// DrainVaultArguments are arguments for drain vault method.
type DrainVaultArguments struct {
	Vault core.Address
	vault.SpendArguments
}

// Part of the multisig signature.
type Part = multisig.Part
