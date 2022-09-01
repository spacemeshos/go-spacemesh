package vesting

import (
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/multisig"
)

//go:generate scalegen

// DrainVaultArguments are arguments for drain vault method.
type DrainVaultArguments struct {
	Vault    core.Address
	Receiver core.Address
	Amount   uint64
}

// DrainVaultPayload ...
type DrainVaultPayload = multisig.SpendPayload

type Part = multisig.Part

type SpendArguments = multisig.SpendArguments
