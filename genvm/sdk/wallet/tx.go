package wallet

import (
	"bytes"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/types/address"
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/wallet"
	"github.com/spacemeshos/go-spacemesh/hash"
	"github.com/spacemeshos/go-spacemesh/signing"
)

const (
	// TotalGasSpawn is a fixed amount of gas for spawn.
	TotalGasSpawn = wallet.TotalGasSpawn
	// TotalGasSpend is a fixed amount of gas for spend.
	TotalGasSpend = wallet.TotalGasSpend
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
	copy(payload.Arguments.PublicKey[:], signing.Public(pk))
	principal := core.ComputePrincipal(wallet.TemplateAddress, &payload.Arguments)

	tx := encode(&zero, &principal, &zero, &wallet.TemplateAddress, &payload)
	hh := hash.Sum(tx)
	sig := ed25519.Sign(ed25519.PrivateKey(pk), hh[:])
	return append(tx, sig...)
}

// Spend creates spend transaction.
func Spend(pk signing.PrivateKey, to address.Address, amount uint64, nonce types.Nonce, opts ...sdk.Opt) []byte {
	options := sdk.Defaults()
	for _, opt := range opts {
		opt(options)
	}

	spawnargs := wallet.SpawnArguments{}
	copy(spawnargs.PublicKey[:], signing.Public(pk))
	principal := core.ComputePrincipal(wallet.TemplateAddress, &spawnargs)

	payload := wallet.SpendPayload{}
	payload.GasPrice = options.GasPrice
	payload.Arguments.Destination = to
	payload.Arguments.Amount = amount
	payload.Nonce = nonce

	tx := encode(&zero, &principal, &one, &payload)
	hh := hash.Sum(tx)
	sig := ed25519.Sign(ed25519.PrivateKey(pk), hh[:])
	return append(tx, sig...)
}
