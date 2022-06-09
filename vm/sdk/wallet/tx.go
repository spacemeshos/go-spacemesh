package wallet

import (
	"bytes"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/sdk"
	"github.com/spacemeshos/go-spacemesh/vm/templates/wallet"
)

var (
	zero = scale.U8(0)
	one  = scale.U8(1)
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

// SelfSpawn create spawn transaction.
func SelfSpawn(pk signing.PrivateKey, opts ...sdk.Opt) []byte {
	options := sdk.Defaults()
	for _, opt := range opts {
		opt(options)
	}

	payload := wallet.SpawnPayload{}
	payload.GasPrice = options.GasPrice
	copy(payload.Arguments.PublicKey[:], pk[32:])
	var principal core.Address
	if options.Principal != nil {
		principal = *options.Principal
	} else {
		principal = core.ComputePrincipal(wallet.TemplateAddress, &payload.Arguments)
	}

	tx := encode(&zero, &principal, &zero, &wallet.TemplateAddress, &payload)
	hh := hash.Sum(tx)
	sig := ed25519.Sign(ed25519.PrivateKey(pk), hh[:])
	return append(tx, sig...)
}

// Spend creates spend transaction.
func Spend(pk signing.PrivateKey, to types.Address, amount uint64, opts ...sdk.Opt) []byte {
	options := sdk.Defaults()
	for _, opt := range opts {
		opt(options)
	}
	if options.Nonce == nil {
		panic("nonce is required for spend")
	}

	var principal types.Address
	if options.Principal == nil {
		spawnargs := wallet.SpawnArguments{}
		copy(spawnargs.PublicKey[:], pk[32:])
		principal = core.ComputePrincipal(wallet.TemplateAddress, &spawnargs)
	} else {
		principal = *options.Principal
	}

	payload := wallet.SpendPayload{}
	payload.GasPrice = options.GasPrice
	payload.Arguments.Destination = to
	payload.Arguments.Amount = amount
	payload.Nonce = *options.Nonce

	tx := encode(&zero, &principal, &one, &payload)
	hh := hash.Sum(tx)
	sig := ed25519.Sign(ed25519.PrivateKey(pk), hh[:])
	return append(tx, sig...)
}
