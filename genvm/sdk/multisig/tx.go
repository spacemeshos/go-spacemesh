package multisig

import (
	"bytes"
	"sort"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/genvm/core"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk"
	"github.com/spacemeshos/go-spacemesh/genvm/templates/multisig"
	"github.com/spacemeshos/go-spacemesh/hash"
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

// Aggregator is a signature accumulator.
type Aggregator struct {
	payload []byte
	parts   map[uint8]multisig.Part
}

// Add signature parts to the accumulator.
func (tx *Aggregator) Add(parts ...multisig.Part) {
	for _, part := range parts {
		tx.parts[part.Ref] = part
	}
}

// Part returns signature part from ref public key.
func (tx *Aggregator) Part(ref uint8) *multisig.Part {
	part, exists := tx.parts[ref]
	if !exists {
		return nil
	}
	return &part
}

// Raw returns full raw transaction including payload and signature.
func (tx *Aggregator) Raw() []byte {
	buf := bytes.NewBuffer(nil)
	enc := scale.NewEncoder(buf)
	sig := make(multisig.Signatures, 0, len(tx.parts))
	for _, part := range tx.parts {
		sig = append(sig, part)
	}
	sort.Slice(sig, func(i, j int) bool {
		return sig[i].Ref < sig[j].Ref
	})
	_, err := scale.EncodeStructArray(enc, sig)
	if err != nil {
		panic(err) // buf.Write is not expected to fail
	}
	return append(tx.payload, buf.Bytes()...)
}

// SelfSpawn returns accumulator for self-spawn transaction.
func SelfSpawn(ref uint8, pk ed25519.PrivateKey, template types.Address, pubs []ed25519.PublicKey, nonce core.Nonce, opts ...sdk.Opt) *Aggregator {
	options := sdk.Defaults()
	for _, opt := range opts {
		opt(options)
	}

	payload := multisig.SpawnPayload{}
	payload.Nonce = nonce
	payload.GasPrice = options.GasPrice
	payload.Arguments.PublicKeys = make([]core.PublicKey, len(pubs))
	for i := range pubs {
		copy(payload.Arguments.PublicKeys[i][:], pubs[i])
	}
	principal := core.ComputePrincipal(template, &payload.Arguments)

	tx := encode(&sdk.TxVersion, &principal, &sdk.MethodSpawn, &template, &payload)
	hh := hash.Sum(tx)
	sig := ed25519.Sign(ed25519.PrivateKey(pk), hh[:])
	aggregator := &Aggregator{payload: tx, parts: map[uint8]multisig.Part{}}
	part := multisig.Part{Ref: ref}
	copy(part.Sig[:], sig)
	aggregator.Add(part)
	return aggregator
}

// Spend creates spend transaction.
func Spend(ref uint8, pk ed25519.PrivateKey, principal, to types.Address, amount uint64, nonce types.Nonce, opts ...sdk.Opt) *Aggregator {
	options := sdk.Defaults()
	for _, opt := range opts {
		opt(options)
	}

	payload := multisig.SpendPayload{}
	payload.GasPrice = options.GasPrice
	payload.Arguments.Destination = to
	payload.Arguments.Amount = amount
	payload.Nonce = nonce

	tx := encode(&sdk.TxVersion, &principal, &sdk.MethodSpend, &payload)
	hh := hash.Sum(tx)
	sig := ed25519.Sign(ed25519.PrivateKey(pk), hh[:])
	aggregator := &Aggregator{payload: tx, parts: map[uint8]multisig.Part{}}
	part := multisig.Part{Ref: ref}
	copy(part.Sig[:], sig)
	aggregator.Add(part)
	return aggregator
}
