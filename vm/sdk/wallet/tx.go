package wallet

import (
	"bytes"
	"fmt"

	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"
	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/host"
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

// Spawn creates a spawn transaction.
func Spawn(
	pk signing.PrivateKey,
	nonce core.Nonce,
	opts ...sdk.Opt,
) ([]byte, error) {
	options := sdk.Defaults()
	for _, opt := range opts {
		opt(options)
	}

	// Encode using the VM
	libPath, err := host.AthenaLibPath()
	if err != nil {
		panic(fmt.Errorf("loading Athena VM: %w", err))
	}
	vmlib, err := athcon.LoadLibrary(libPath)
	if err != nil {
		panic(fmt.Errorf("loading Athena VM: %w", err))
	}

	meta := core.Metadata{}
	meta.Nonce = nonce
	meta.GasPrice = options.GasPrice

	// note that principal is computed from pk
	athenaPayload := vmlib.EncodeTxSpawn(athcon.Bytes32(signing.Public(pk)))
	principal, err := core.ComputePrincipalFromPubkey(wallet.TemplateAddress, *signing.NewPublicKey(signing.Public(pk)))
	if err != nil {
		return nil, err
	}
	payload := core.Payload(athenaPayload)

	// The payload is already encoded. Why, might you ask, are we encoding it again?
	// Short answer: because, when decoding txs, go-spacemesh can only decode SCALE-encoded data.
	// Fixing this, and allowing a tx to be partially SCALE-encoded, partially raw bytes,
	// is a lot of work for a tiny bit of gain.
	tx := encode(&sdk.TxVersion, &principal, &meta, &payload)
	// tx := encode(&sdk.TxVersion, &principal, &meta)
	// tx = append(tx, payload...)

	// sig := ed25519.Sign(ed25519.PrivateKey(pk), core.SigningBody(options.GenesisID[:], tx))
	sig := ed25519.Sign(ed25519.PrivateKey(pk), tx)
	return append(tx, sig...), nil
}

// Spend creates a spend transaction.
func Spend(pk signing.PrivateKey, to types.Address, amount uint64, nonce types.Nonce, opts ...sdk.Opt) ([]byte, error) {
	options := sdk.Defaults()
	for _, opt := range opts {
		opt(options)
	}

	// Encode using the VM
	libPath, err := host.AthenaLibPath()
	if err != nil {
		panic(fmt.Errorf("loading Athena VM: %w", err))
	}
	vmlib, err := athcon.LoadLibrary(libPath)
	if err != nil {
		panic(fmt.Errorf("loading Athena VM: %w", err))
	}

	principal, err := core.ComputePrincipalFromPubkey(wallet.TemplateAddress, *signing.NewPublicKey(signing.Public(pk)))
	if err != nil {
		return nil, err
	}

	payload := core.Payload(vmlib.EncodeTxSpend(athcon.Address(to), amount))

	meta := core.Metadata{}
	meta.GasPrice = options.GasPrice
	meta.Nonce = nonce

	tx := encode(&sdk.TxVersion, &principal, &meta, &payload)
	// tx := encode(&sdk.TxVersion, &principal, &meta)
	// tx = append(tx, payload...)

	// sig := ed25519.Sign(ed25519.PrivateKey(pk), core.SigningBody(options.GenesisID[:], tx))
	sig := ed25519.Sign(ed25519.PrivateKey(pk), tx)
	return append(tx, sig...), nil
}
