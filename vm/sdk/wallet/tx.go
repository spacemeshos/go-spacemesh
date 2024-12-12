package wallet

import (
	"bytes"
	"fmt"

	gossamerScale "github.com/ChainSafe/gossamer/pkg/scale"
	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/host"
	"github.com/spacemeshos/go-spacemesh/vm/sdk"
	"github.com/spacemeshos/go-spacemesh/vm/templates/wallet"
)

func Deploy(pk signing.PrivateKey, nonce core.Nonce, blob []byte, opts ...sdk.Opt) ([]byte, error) {
	options := sdk.Defaults()
	for _, opt := range opts {
		opt(options)
	}
	var blobEncoded bytes.Buffer
	if _, err := codec.EncodeByteSlice(&blobEncoded, blob); err != nil {
		return nil, fmt.Errorf("encoding code blob: %w", err)
	}

	athPayload := athcon.Payload{
		Selector: &wallet.DeploySelector,
		Input:    blobEncoded.Bytes(),
	}
	payload, err := gossamerScale.Marshal(athPayload)
	if err != nil {
		return nil, fmt.Errorf("encoding tx payload: %w", err)
	}

	template := options.Template
	if template == nil {
		template = &wallet.TemplateAddress
	}
	tx := core.Tx{
		Version:   uint8(sdk.TxVersion),
		Principal: core.ComputePrincipalFromBlob(*template, signing.Public(pk)),
		Metadata: core.Metadata{
			Nonce:    nonce,
			GasPrice: options.GasPrice,
		},
		Payload: payload,
	}

	return core.SignedTx(&tx, options.GenesisID, pk)
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

	template := options.Template
	if template == nil {
		template = &wallet.TemplateAddress
	}

	tx := core.Tx{
		Version:   uint8(sdk.TxVersion),
		Principal: core.ComputePrincipalFromBlob(*template, signing.Public(pk)),
		Template:  template,
		Metadata: core.Metadata{
			Nonce:    nonce,
			GasPrice: options.GasPrice,
		},
		Payload: vmlib.EncodeTxSpawn(athcon.Bytes32(signing.Public(pk))),
	}
	return core.SignedTx(&tx, options.GenesisID, pk)
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

	template := options.Template
	if template == nil {
		template = &wallet.TemplateAddress
	}

	tx := core.Tx{
		Version:   uint8(sdk.TxVersion),
		Principal: core.ComputePrincipalFromBlob(*template, signing.Public(pk)),
		Metadata: core.Metadata{
			Nonce:    nonce,
			GasPrice: options.GasPrice,
		},
		Payload: vmlib.EncodeTxSpend(athcon.Address(to), amount),
	}
	return core.SignedTx(&tx, options.GenesisID, pk)
}
