package multisig

import (
	"fmt"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/genvm/core"
)

//go:generate scalegen

// MultiSig K/N template.
type MultiSig struct {
	k          uint8
	PublicKeys []core.PublicKey
}

// MaxSpend returns amount specified in the SpendArguments.
func (ms *MultiSig) MaxSpend(method uint8, args any) (uint64, error) {
	switch method {
	case methodSpawn:
		return 0, nil
	case methodSpend:
		return args.(*SpendArguments).Amount, nil
	default:
		return 0, fmt.Errorf("%w: unknown method %d", core.ErrMalformed, method)
	}
}

// Verify that transaction is signed has k valid signatures.
func (ms *MultiSig) Verify(host core.Host, raw []byte, dec *scale.Decoder) bool {
	sig := make(Signatures, ms.k)
	n, err := scale.DecodeStructArray(dec, sig)
	if err != nil {
		return false
	}
	hash := core.Hash(raw[:len(raw)-n])
	batch := ed25519.NewBatchVerifierWithCapacity(int(ms.k))
	last := uint8(0)
	for i, part := range sig {
		if part.Ref >= uint8(len(ms.PublicKeys)) {
			return false
		}
		if i != 0 && part.Ref <= last {
			return false
		}
		last = part.Ref
		batch.Add(ms.PublicKeys[part.Ref][:], hash[:], part.Sig[:])
	}
	verified, _ := batch.Verify(nil)
	return verified
}

// Spend transfers an amount to the address specified in SpendArguments.
func (ms *MultiSig) Spend(host core.Host, args *SpendArguments) error {
	return host.Transfer(args.Destination, args.Amount)
}
