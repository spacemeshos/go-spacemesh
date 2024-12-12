package multisig

import (
	"bytes"
	"fmt"
	"maps"
	"slices"

	"github.com/ChainSafe/gossamer/pkg/scale"
	athcon "github.com/athenavm/athena/ffi/athcon/bindings/go"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/vm/core"
	"github.com/spacemeshos/go-spacemesh/vm/host"
	"github.com/spacemeshos/go-spacemesh/vm/sdk"
)

// SpawnArguments contains a collection with PublicKeys.
type SpawnArguments struct {
	Required   uint8
	PublicKeys []core.PublicKey
}

// Signatures is a collections of parts that must satisfy multisig
// threshold requirement.
type Signatures []Part

// Part contains a reference to public key and signature from private key counterpart.
type Part struct {
	Ref uint8
	Sig core.Signature
}

func NewAggregator(unsigned []byte) *Aggregator {
	return &Aggregator{unsigned: unsigned, parts: map[uint8]Part{}}
}

// Aggregator is a signature accumulator.
type Aggregator struct {
	unsigned []byte
	parts    map[uint8]Part
}

// Add signature parts to the accumulator.
func (tx *Aggregator) Add(ref uint8, sig core.Signature) {
	if _, exists := tx.parts[ref]; exists {
		panic("signature already exists")
	}
	tx.parts[ref] = Part{
		Ref: ref,
		Sig: sig,
	}
}

// Raw returns full raw transaction including payload and signatures.
func (tx *Aggregator) Raw() []byte {
	var buf bytes.Buffer
	enc := scale.NewEncoder(&buf)
	keys := slices.Sorted(maps.Keys(tx.parts))
	for _, ref := range keys {
		if err := enc.Encode(tx.parts[ref]); err != nil {
			panic(err)
		}
	}
	rawTxBuf := bytes.NewBuffer(tx.unsigned)
	enc = scale.NewEncoder(rawTxBuf)
	if err := enc.Encode(buf.Bytes()); err != nil {
		panic(err)
	}
	return rawTxBuf.Bytes()
}

func EncodeSpawnArgs(required uint8, pubkeys []core.PublicKey) ([]byte, error) {
	args := SpawnArguments{
		Required:   required,
		PublicKeys: pubkeys,
	}

	return scale.Marshal(args)
}

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
	encodedArgs, err := EncodeSpawnArgs(required, pubkeys)
	if err != nil {
		return nil, fmt.Errorf("marshalling spawn args: %w", err)
	}
	selector, _ := athcon.FromString("athexp_spawn")
	payload := athcon.Payload{
		Selector: &selector,
		Input:    encodedArgs,
	}
	encodedPayload, err := scale.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encoding TX payload: %w", err)
	}
	accountAddress := core.ComputePrincipalFromBlob(template, encodedArgs)
	fmt.Printf("calculated principal: %s\n", accountAddress.String())
	tx := core.Tx{
		Version:   1,
		Principal: accountAddress,
		Template:  &template,
		Metadata: core.Metadata{
			Nonce:    nonce,
			GasPrice: options.GasPrice,
		},
		Payload: encodedPayload,
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
		Principal: principal,
		Metadata: core.Metadata{
			Nonce:    nonce,
			GasPrice: options.GasPrice,
		},
		Payload: vmlib.EncodeTxSpend(athcon.Address(to), amount),
	}
	return codec.Encode(&tx)
}
