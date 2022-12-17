package wallet

import (
	"bytes"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/wallet"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func encode(fields ...scale.Encodable) []byte {
	buf := bytes.NewBuffer(nil)
	encoder := scale.NewEncoder(buf)
	for _, field := range fields {
		_, err := field.EncodeScale(encoder)
		if err != nil {
			panic(err)
		}
	}
	return buf.Bytes()
}

// SelfSpawn creates a self-spawn transaction.
func SelfSpawn(pk signing.PrivateKey, nonce core.Nonce, opts ...sdk.Opt) []byte {
	args := wallet.SpawnArguments{}
	copy(args.PublicKey[:], signing.Public(pk))
	return Spawn(pk, wallet.TemplateAddress, &args, nonce, opts...)
}

// Spawn creates a spawn transaction.
func Spawn(pk signing.PrivateKey, template core.Address, args scale.Encodable, nonce core.Nonce, opts ...sdk.Opt) []byte {
	options := sdk.Defaults()
	for _, opt := range opts {
		opt(options)
	}

	payload := core.Payload{}
	payload.Nonce = nonce
	payload.GasPrice = options.GasPrice

	public := &core.PublicKey{}
	copy(public[:], signing.Public(pk))
	// note that principal is computed from pk
	principal := core.ComputePrincipal(wallet.TemplateAddress, public)

	tx := encode(&sdk.TxVersion, &principal, &sdk.MethodSpawn, &template, &payload, args)
	hh := hash.Sum(options.GenesisID[:], tx)
	sig := ed25519.Sign(ed25519.PrivateKey(pk), hh[:])
	return append(tx, sig...)
}

// Spend creates spend transaction.
func Spend(pk signing.PrivateKey, to types.Address, amount uint64, nonce types.Nonce, opts ...sdk.Opt) []byte {
	options := sdk.Defaults()
	for _, opt := range opts {
		opt(options)
	}

	spawnargs := wallet.SpawnArguments{}
	copy(spawnargs.PublicKey[:], signing.Public(pk))
	principal := core.ComputePrincipal(wallet.TemplateAddress, &spawnargs)

	payload := core.Payload{}
	payload.GasPrice = options.GasPrice
	payload.Nonce = nonce

	args := wallet.SpendArguments{}
	args.Destination = to
	args.Amount = amount

	tx := encode(&sdk.TxVersion, &principal, &sdk.MethodSpend, &payload, &args)
	hh := hash.Sum(options.GenesisID[:], tx)
	sig := ed25519.Sign(ed25519.PrivateKey(pk), hh[:])
	return append(tx, sig...)
}
