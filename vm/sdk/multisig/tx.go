package multisig

import (
	"bytes"
	"maps"
	"slices"

	// FIXME: use go-scale when we add a tag to encode uint8 non-compact.
	"github.com/ChainSafe/gossamer/pkg/scale"
	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/sdk"
	"github.com/spacemeshos/go-spacemesh/vm/templates/multisig"
)

// part contains a reference to public key and signature from private key counterpart.
type part struct {
	Ref uint8
	Sig core.Signature
}

func NewSignatureAggregator(unsigned []byte) *SignatureAggregator {
	return &SignatureAggregator{unsigned: unsigned, parts: map[uint8]part{}}
}

// SignatureAggregator is a signature accumulator.
type SignatureAggregator struct {
	unsigned []byte
	parts    map[uint8]part
}

// Add signature and reference to the public key counterpart.
func (a *SignatureAggregator) Add(ref uint8, sig core.Signature) {
	if _, exists := a.parts[ref]; exists {
		panic("signature already exists")
	}
	a.parts[ref] = part{
		Ref: ref,
		Sig: sig,
	}
}

// Raw returns full raw transaction including payload and signatures.
func (a *SignatureAggregator) Raw() []byte {
	buf := bytes.NewBuffer(a.unsigned)
	enc := scale.NewEncoder(buf)
	keys := slices.Sorted(maps.Keys(a.parts))
	for _, ref := range keys {
		if err := enc.Encode(a.parts[ref]); err != nil {
			panic(err)
		}
	}
	return buf.Bytes()
}

func EncodeSpawnArgs(required uint8, pubkeys []core.PublicKey) []byte {
	args := multisig.SpawnArguments{
		Required:   required,
		PublicKeys: pubkeys,
	}
	return scale.MustMarshal(args)
}

func EncodeSpendArgs(to types.Address, amount uint64) []byte {
	args := multisig.SpendArguments{
		To:     to,
		Amount: amount,
	}
	return scale.MustMarshal(args)
}

// Spawn creates a raw SPAWN transaction, which needs to be signed by the required
// number of signers.
func Spawn(
	template types.Address,
	required uint8,
	pubkeys []core.PublicKey,
	nonce core.Nonce,
	opts ...sdk.Opt,
) ([]byte, error) {
	options := sdk.Defaults()
	for _, opt := range opts {
		opt(options)
	}
	encodedArgs := EncodeSpawnArgs(required, pubkeys)
	selector, _ := athcon.FromString("athexp_spawn")
	payload := athcon.Payload{
		Selector: &selector,
		Input:    encodedArgs,
	}
	tx := core.Tx{
		Version:   1,
		Principal: core.ComputePrincipalFromBlob(template, encodedArgs),
		Template:  &template,
		Metadata: core.Metadata{
			Nonce:    nonce,
			GasPrice: options.GasPrice,
		},
		Payload: scale.MustMarshal(payload),
	}
	return codec.Encode(&tx)
}

// Spend creates a raw SPEND transaction, which needs to be signed by the required
// number of signers.
func Spend(principal, to types.Address, amount uint64, nonce types.Nonce, opts ...sdk.Opt) ([]byte, error) {
	options := sdk.Defaults()
	for _, opt := range opts {
		opt(options)
	}

	selector, _ := athcon.FromString("athexp_spend")
	payload := athcon.Payload{
		Selector: &selector,
		Input:    EncodeSpendArgs(to, amount),
	}

	tx := core.Tx{
		Version:   uint8(sdk.TxVersion),
		Principal: principal,
		Metadata: core.Metadata{
			Nonce:    nonce,
			GasPrice: options.GasPrice,
		},
		Payload: scale.MustMarshal(payload),
	}
	return codec.Encode(&tx)
}
