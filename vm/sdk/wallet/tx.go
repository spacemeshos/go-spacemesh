package wallet

import (
	"bytes"
	"fmt"
	"log"

	gossamerScale "github.com/ChainSafe/gossamer/pkg/scale"
	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"
	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/host"
	"github.com/spacemeshos/go-spacemesh/vm/sdk"
	"github.com/spacemeshos/go-spacemesh/vm/templates"
	"github.com/spacemeshos/go-spacemesh/vm/templates/wallet"
)

func DeployTx(pubkey ed25519.PublicKey, nonce core.Nonce, blob []byte, opts ...sdk.Opt) (*core.Tx, error) {
	options := sdk.Defaults()
	for _, opt := range opts {
		opt(options)
	}
	var blobEncoded bytes.Buffer
	if _, err := codec.EncodeByteSlice(&blobEncoded, blob); err != nil {
		return nil, fmt.Errorf("encoding code blob: %w", err)
	}

	athPayload := athcon.Payload{
		Selector: &templates.DeploySelector,
		Input:    blobEncoded.Bytes(),
	}
	payload, err := gossamerScale.Marshal(athPayload)
	if err != nil {
		return nil, fmt.Errorf("encoding tx payload: %w", err)
	}

	return &core.Tx{
		Version:   uint8(sdk.TxVersion),
		Principal: core.ComputePrincipalFromBlob(wallet.TemplateAddress, pubkey),
		Metadata: core.Metadata{
			Nonce:    nonce,
			GasPrice: options.GasPrice,
		},
		Payload: payload,
	}, nil
}

func Deploy(pk signing.PrivateKey, nonce core.Nonce, blob []byte, opts ...sdk.Opt) ([]byte, error) {
	options := sdk.Defaults()
	for _, opt := range opts {
		opt(options)
	}
	tx, err := DeployTx(signing.Public(pk), nonce, blob, opts...)
	if err != nil {
		return nil, err
	}

	return core.SignedTx(tx, options.GenesisID, pk)
}

func EncodeSpawnArgs(pubkey ed25519.PublicKey) []byte {
	args := wallet.SpawnArgs{
		Pubkey: [32]byte(pubkey),
	}
	encoded, err := gossamerScale.Marshal(args)
	if err != nil {
		log.Panicf("encoding spawn arguments: %v", err)
	}
	return encoded
}

func SpawnTx(pubkey ed25519.PublicKey, nonce core.Nonce, opts ...sdk.Opt) (*core.Tx, error) {
	options := sdk.Defaults()
	for _, opt := range opts {
		opt(options)
	}

	// Encode using the VM
	libPath, err := host.AthenaLibPath()
	if err != nil {
		return nil, fmt.Errorf("looking up Athena VM library path: %w", err)
	}
	vmlib, err := athcon.LoadLibrary(libPath)
	if err != nil {
		return nil, fmt.Errorf("loading Athena VM: %w", err)
	}
	defer vmlib.Close()

	return &core.Tx{
		Version:   uint8(sdk.TxVersion),
		Principal: core.ComputePrincipalFromBlob(wallet.TemplateAddress, pubkey),
		Template:  (*types.Address)(&wallet.TemplateAddress),
		Metadata: core.Metadata{
			Nonce:    nonce,
			GasPrice: options.GasPrice,
		},
		Payload: vmlib.EncodeTxSpawn(athcon.Bytes32(pubkey)),
	}, nil
}

func Spawn(
	pk signing.PrivateKey,
	nonce core.Nonce,
	opts ...sdk.Opt,
) ([]byte, error) {
	options := sdk.Defaults()
	for _, opt := range opts {
		opt(options)
	}

	tx, err := SpawnTx(signing.Public(pk), nonce, opts...)
	if err != nil {
		return nil, err
	}

	return core.SignedTx(tx, options.GenesisID, pk)
}

func SpendTx(
	pubkey ed25519.PublicKey,
	to types.Address,
	amount uint64,
	nonce types.Nonce,
	opts ...sdk.Opt,
) (*core.Tx, error) {
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
	defer vmlib.Close()

	return &core.Tx{
		Version:   uint8(sdk.TxVersion),
		Principal: core.ComputePrincipalFromBlob(wallet.TemplateAddress, pubkey),
		Metadata: core.Metadata{
			Nonce:    nonce,
			GasPrice: options.GasPrice,
		},
		Payload: vmlib.EncodeTxSpend(athcon.Address(to), amount),
	}, nil
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
	defer vmlib.Close()

	tx := core.Tx{
		Version:   uint8(sdk.TxVersion),
		Principal: core.ComputePrincipalFromBlob(wallet.TemplateAddress, signing.Public(pk)),
		Metadata: core.Metadata{
			Nonce:    nonce,
			GasPrice: options.GasPrice,
		},
		Payload: vmlib.EncodeTxSpend(athcon.Address(to), amount),
	}
	return core.SignedTx(&tx, options.GenesisID, pk)
}

func ProxyTx(
	principal, to types.Address,
	method *athcon.MethodSelector,
	args []byte,
	amount, nonce uint64,
	opts ...sdk.Opt,
) (*core.Tx, error) {
	options := sdk.Defaults()
	for _, opt := range opts {
		opt(options)
	}
	input := wallet.ProxyArgs{
		Destination: to,
		Method:      method,
		Amount:      amount,
	}
	if len(args) > 0 {
		input.Args = new([]byte)
		*input.Args = args
	}
	inputEncoded, err := gossamerScale.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("encoding proxy method args: %w", err)
	}

	athPayload := athcon.Payload{
		Selector: &templates.ProxySelector,
		Input:    inputEncoded,
	}
	payload, err := gossamerScale.Marshal(athPayload)
	if err != nil {
		return nil, fmt.Errorf("encoding tx payload: %w", err)
	}

	return &core.Tx{
		Version:   uint8(sdk.TxVersion),
		Principal: principal,
		Metadata: core.Metadata{
			Nonce:    nonce,
			GasPrice: options.GasPrice,
		},
		Payload: payload,
	}, nil
}

func Proxy(
	pk signing.PrivateKey,
	to types.Address,
	method *athcon.MethodSelector,
	args []byte,
	amount, nonce uint64,
	opts ...sdk.Opt,
) ([]byte, error) {
	options := sdk.Defaults()
	for _, opt := range opts {
		opt(options)
	}
	tx, err := ProxyTx(Address(signing.Public(pk)), to, method, args, amount, nonce, opts...)
	if err != nil {
		return nil, err
	}

	return core.SignedTx(tx, options.GenesisID, pk)
}
