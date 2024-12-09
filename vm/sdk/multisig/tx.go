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
	buf := bytes.NewBuffer(tx.unsigned)
	enc := scale.NewEncoder(buf)
	keys := slices.Sorted(maps.Keys(tx.parts))
	for _, ref := range keys {
		if err := enc.Encode(tx.parts[ref].Ref); err != nil {
			panic(err)
		}
		if err := enc.Encode(tx.parts[ref].Sig); err != nil {
			panic(err)
		}
	}
	return buf.Bytes()
}

func encodeSpawnArgs(required uint8, pubkeys []core.PublicKey) ([]byte, error) {
	args := SpawnArguments{
		Required:   required,
		PublicKeys: pubkeys,
	}

	return scale.Marshal(args)
}

func Spawn(template types.Address, required uint8, pubkeys []core.PublicKey, nonce core.Nonce) ([]byte, error) {
	encodedArgs, err := encodeSpawnArgs(required, pubkeys)
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
	tx := core.Tx{
		Version:   1,
		Principal: accountAddress,
		Template:  &template,
		Metadata: core.Metadata{
			Nonce:    nonce,
			GasPrice: 1,
		},
		Payload: encodedPayload,
	}
	return codec.Encode(&tx)
}
