package vesting

import (
	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/multisig"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/vesting"
	"github.com/spacemeshos/go-spacemesh/hash"
)

type Aggregator = multisig.Aggregator

var (
	NewAggregator = multisig.NewAggregator

	SelfSpawn = multisig.SelfSpawn
	Spawn     = multisig.Spawn
	Spend     = multisig.Spend
)

// DrainVault creates drain vault transaction.
func DrainVault(ref uint8, pk ed25519.PrivateKey, principal, vault, receiver types.Address, amount uint64, nonce core.Nonce, opts ...sdk.Opt) *Aggregator {
	options := sdk.Defaults()
	for _, opt := range opts {
		opt(options)
	}

	payload := core.Payload{}
	payload.Nonce = nonce
	payload.GasPrice = options.GasPrice

	args := vesting.DrainVaultArguments{
		Vault: vault,
	}
	args.Destination = receiver
	args.Amount = amount

	method := scale.U8(vesting.MethodDrainVault)
	tx := sdk.Encode(&sdk.TxVersion, &principal, &method, &payload, &args)
	hh := hash.Sum(options.GenesisID[:], tx)
	sig := ed25519.Sign(ed25519.PrivateKey(pk), hh[:])
	aggregator := NewAggregator(tx)
	part := vesting.Part{Ref: ref}
	copy(part.Sig[:], sig)
	aggregator.Add(part)
	return aggregator
}
