package mint

import (
	// FIXME: use go-scale when we add a tag to encode uint8 non-compact.
	"github.com/ChainSafe/gossamer/pkg/scale"
	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"
	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/sdk"
	"github.com/spacemeshos/go-spacemesh/vm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/vm/templates"
	"github.com/spacemeshos/go-spacemesh/vm/templates/mint"
)

func EncodeSpawnArgs(supply, price uint64, pubkey core.PublicKey) []byte {
	args := mint.SpawnArguments{
		Owner:     pubkey,
		MaxSupply: supply,
		Price:     price,
	}
	return scale.MustMarshal(args)
}

func EncodeSpendArgs(to types.Address, amount uint64) []byte {
	args := mint.SpendArguments{
		To:     to,
		Amount: amount,
	}
	return scale.MustMarshal(args)
}

func EncodeBuyArgs(recipient types.Address) []byte {
	return scale.MustMarshal(mint.BuyArguments{
		Recipient: recipient,
	})
}

// Spawn creates a raw SPAWN transaction, which needs to be signed by the required
// number of signers.
func SpawnTx(
	pubkey ed25519.PublicKey,
	supply uint64,
	price uint64,
	nonce core.Nonce,
	opts ...sdk.Opt,
) *core.Tx {
	options := sdk.Defaults()
	for _, opt := range opts {
		opt(options)
	}
	encodedArgs := EncodeSpawnArgs(supply, price, core.PublicKey(pubkey))
	payload := athcon.Payload{
		Selector: &templates.SpawnSelector,
		Input:    encodedArgs,
	}
	return &core.Tx{
		Version:   1,
		Principal: core.ComputePrincipalFromBlob(mint.TemplateAddress, encodedArgs),
		Template:  &mint.TemplateAddress,
		Metadata: core.Metadata{
			Nonce:    nonce,
			GasPrice: options.GasPrice,
		},
		Payload: scale.MustMarshal(payload),
	}
}

func Spawn(
	pk signing.PrivateKey,
	supply uint64,
	price uint64,
	nonce core.Nonce,
	opts ...sdk.Opt,
) ([]byte, error) {
	options := sdk.Defaults()
	for _, opt := range opts {
		opt(options)
	}

	tx := SpawnTx(signing.Public(pk), supply, price, nonce, opts...)

	return core.SignedTx(tx, options.GenesisID, pk)
}

func SpendTx(principal, to types.Address, amount uint64, nonce types.Nonce, opts ...sdk.Opt) *core.Tx {
	options := sdk.Defaults()
	for _, opt := range opts {
		opt(options)
	}

	selector, _ := athcon.FromString("athexp_spend")
	payload := athcon.Payload{
		Selector: &selector,
		Input:    EncodeSpendArgs(to, amount),
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

// Spend creates a raw SPEND transaction, which needs to be signed by the required
// number of signers.
func Spend(principal, to types.Address, amount uint64, nonce types.Nonce, opts ...sdk.Opt) ([]byte, error) {
	return codec.Encode(SpendTx(principal, to, amount, nonce, opts...))
}

func BuyTx(
	principal, mint, recipient types.Address,
	amount uint64,
	nonce types.Nonce,
	opts ...sdk.Opt,
) (*core.Tx, error) {
	return wallet.ProxyTx(principal, mint, &templates.BuySelector, EncodeBuyArgs(recipient), amount, nonce, opts...)
}
