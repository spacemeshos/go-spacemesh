package tokenwallet

import (
	// FIXME: use go-scale when we add a tag to encode uint8 non-compact.
	"github.com/ChainSafe/gossamer/pkg/scale"
	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"
	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/sdk"
	"github.com/spacemeshos/go-spacemesh/vm/templates"
	tokenwallet "github.com/spacemeshos/go-spacemesh/vm/templates/token_wallet"
)

func EncodeSpawnArgs(mintTemplate, walletTemplate types.Address, pubkey core.PublicKey) []byte {
	args := tokenwallet.SpawnArguments{
		Owner:          pubkey,
		MintTemplate:   mintTemplate,
		WalletTemplate: walletTemplate,
	}
	return scale.MustMarshal(args)
}

func EncodeSendTokenArgs(tokenID, to types.Address, amount uint64) []byte {
	args := tokenwallet.SendTokenArguments{
		TokenId: tokenID,
		To:      to,
		Amount:  amount,
	}
	return scale.MustMarshal(args)
}

// Spawn creates a raw SPAWN transaction, which needs to be signed by the required
// number of signers.
func SpawnTx(
	pubkey ed25519.PublicKey,
	mintTemplate types.Address,
	nonce core.Nonce,
	opts ...sdk.Opt,
) *core.Tx {
	options := sdk.Defaults()
	for _, opt := range opts {
		opt(options)
	}
	encodedArgs := EncodeSpawnArgs(mintTemplate, tokenwallet.TemplateAddress, core.PublicKey(pubkey))
	payload := athcon.Payload{
		Selector: &templates.SpawnSelector,
		Input:    encodedArgs,
	}
	return &core.Tx{
		Version:   1,
		Principal: core.ComputePrincipalFromBlob(tokenwallet.TemplateAddress, encodedArgs),
		Template:  &tokenwallet.TemplateAddress,
		Metadata: core.Metadata{
			Nonce:    nonce,
			GasPrice: options.GasPrice,
		},
		Payload: scale.MustMarshal(payload),
	}
}

func Spawn(
	pk signing.PrivateKey,
	mintTemplate types.Address,
	nonce core.Nonce,
	opts ...sdk.Opt,
) ([]byte, error) {
	options := sdk.Defaults()
	for _, opt := range opts {
		opt(options)
	}

	tx := SpawnTx(signing.Public(pk), mintTemplate, nonce, opts...)

	return core.SignedTx(tx, options.GenesisID, pk)
}

func SendTokenTx(tokenID, principal, to types.Address, amount uint64, nonce types.Nonce, opts ...sdk.Opt) *core.Tx {
	options := sdk.Defaults()
	for _, opt := range opts {
		opt(options)
	}

	payload := athcon.Payload{
		Selector: &tokenwallet.SendTokenSelector,
		Input:    EncodeSendTokenArgs(tokenID, to, amount),
	}

	return &core.Tx{
		Version:   uint8(sdk.TxVersion),
		Principal: principal,
		Metadata: core.Metadata{
			Nonce:    nonce,
			GasPrice: options.GasPrice,
		},
		Payload: scale.MustMarshal(payload),
	}
}

// SendToken creates a signed transaction to send tokens.
func SendToken(
	pk ed25519.PrivateKey,
	tokenID, principal, to types.Address,
	amount uint64,
	nonce types.Nonce,
	opts ...sdk.Opt,
) ([]byte, error) {
	options := sdk.Defaults()
	for _, opt := range opts {
		opt(options)
	}

	return core.SignedTx(SendTokenTx(tokenID, principal, to, amount, nonce, opts...), options.GenesisID, pk)
}
