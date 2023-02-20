package multisig

import (
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
func (ms *MultiSig) MaxSpend(method core.Method, args any) (uint64, error) {
	if method == core.MethodSpend {
		return args.(*SpendArguments).Amount, nil
	}
	return 0, nil
}

// Verify that transaction is signed has k valid signatures.
func (ms *MultiSig) Verify(host core.Host, raw []byte, dec *scale.Decoder) bool {
	sig := make(Signatures, ms.k)
	n, err := scale.DecodeStructArray(dec, sig)
	if err != nil {
		return false
	}
	hash := core.Hash(host.GetGenesisID().Bytes(), raw[:len(raw)-n])
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

func (ms *MultiSig) Authorize(core.Host) bool {
	return false
}
