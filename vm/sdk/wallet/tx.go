package wallet

import (
	"bytes"
	"fmt"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/host"
	"github.com/spacemeshos/go-spacemesh/vm/sdk"
	"github.com/spacemeshos/go-spacemesh/vm/templates/wallet"

	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"
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
	return Spawn(pk, wallet.TemplateAddress, nonce, opts...)
}

// Spawn creates a spawn transaction.
func Spawn(
	pk signing.PrivateKey,
	template core.Address,
	nonce core.Nonce,
	opts ...sdk.Opt,
) []byte {
	options := sdk.Defaults()
	for _, opt := range opts {
		opt(options)
	}

	// Encode using the VM
	vmlib, err := athcon.LoadLibrary(host.AthenaLibPath())
	if err != nil {
		panic(fmt.Errorf("loading Athena VM: %w", err))
	}

	meta := core.Metadata{}
	meta.Nonce = nonce
	meta.GasPrice = options.GasPrice

	// note that principal is computed from pk
	athenaPayload := vmlib.EncodeTxSpawn(athcon.Bytes32(signing.Public(pk)))
	principal := core.ComputePrincipal(wallet.TemplateAddress, athenaPayload)
	payload := core.Payload(athenaPayload)

	tx := encode(&sdk.TxVersion, &principal, &template, &meta, &payload)

	sig := ed25519.Sign(ed25519.PrivateKey(pk), core.SigningBody(options.GenesisID[:], tx))
	return append(tx, sig...)
}

// Spend creates a spend transaction.
func Spend(pk signing.PrivateKey, to types.Address, amount uint64, nonce types.Nonce, opts ...sdk.Opt) []byte {
	options := sdk.Defaults()
	for _, opt := range opts {
		opt(options)
	}

	// Encode using the VM
	vmlib, err := athcon.LoadLibrary(host.AthenaLibPath())
	if err != nil {
		panic(fmt.Errorf("loading Athena VM: %w", err))
	}

	// We need a provisional spawn payload to calculate the principal address
	spawnPayload := vmlib.EncodeTxSpawn(athcon.Bytes32(signing.Public(pk)))
	principal := core.ComputePrincipal(wallet.TemplateAddress, spawnPayload)

	payload := core.Payload(vmlib.EncodeTxSpend(athcon.Address(to), nonce))

	meta := core.Metadata{}
	meta.GasPrice = options.GasPrice
	meta.Nonce = nonce

	tx := encode(&sdk.TxVersion, &principal, &meta, &payload)

	sig := ed25519.Sign(ed25519.PrivateKey(pk), core.SigningBody(options.GenesisID[:], tx))
	return append(tx, sig...)
}
