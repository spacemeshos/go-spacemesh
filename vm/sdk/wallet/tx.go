package wallet

import (
	"bytes"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/sdk"
	"github.com/spacemeshos/go-spacemesh/vm/templates/wallet"
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
	// self-spawn has not yet been implemented for Athena
	panic("self-spawn not yet implemented")
}

// Spawn creates a spawn transaction.
func Spawn(
	pk signing.PrivateKey,
	template core.Address,
	args scale.Encodable,
	nonce core.Nonce,
	opts ...sdk.Opt,
) []byte {
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

	// TODO(lane): fix encoding
	tx := encode(&sdk.TxVersion, &principal, &template, &payload, args)
	sig := ed25519.Sign(ed25519.PrivateKey(pk), core.SigningBody(options.GenesisID[:], tx))
	return append(tx, sig...)
}

// Spend creates a spend transaction.
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

	// TODO(lane): fix encoding
	tx := encode(&sdk.TxVersion, &principal, &payload, &args)
	sig := ed25519.Sign(ed25519.PrivateKey(pk), core.SigningBody(options.GenesisID[:], tx))
	return append(tx, sig...)
}