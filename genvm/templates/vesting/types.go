package vesting

import (
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/multisig"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/vault"
)

//go:generate scalegen

// DrainVaultArguments are arguments for drain vault method.
type DrainVaultArguments struct {
	Vault core.Address
	vault.SpendArguments
}

// DrainVaultPayload ...
type DrainVaultPayload = core.Payload

// Part of the multisig signature.
type Part = multisig.Part

// SpendArguments are arguments for Spend call.
type SpendArguments = multisig.SpendArguments
