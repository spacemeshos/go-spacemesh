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
	Required   uint8
	PublicKeys []core.PublicKey `scale:"max=10"`
}

func (ms *MultiSig) BaseGas(method uint8) uint64 {
	return BaseGas * uint64(ms.Required)
}

func (ms *MultiSig) FixedGas(method uint8) uint64 {
	return FixedGas * uint64(ms.Required)
}

// MaxSpend returns amount specified in the SpendArguments.
func (ms *MultiSig) MaxSpend(method uint8, args any) (uint64, error) {
	switch method {
	case core.MethodSpawn:
		return 0, nil
	case core.MethodSpend:
		return args.(*SpendArguments).Amount, nil
	default:
		return 0, fmt.Errorf("%w: unknown method %d", core.ErrMalformed, method)
	}
}

// Verify that transaction is signed has k valid signatures.
func (ms *MultiSig) Verify(host core.Host, raw []byte, dec *scale.Decoder) bool {
	sig := make(Signatures, ms.Required)
	n, err := scale.DecodeStructArray(dec, sig)
	if err != nil {
		return false
	}
	body := core.SigningBody(host.GetGenesisID().Bytes(), raw[:len(raw)-n])
	batch := ed25519.NewBatchVerifierWithCapacity(int(ms.Required))
	last := uint8(0)
	for i, part := range sig {
		if part.Ref >= uint8(len(ms.PublicKeys)) {
			return false
		}
		if i != 0 && part.Ref <= last {
			return false
		}
		last = part.Ref
		batch.Add(ms.PublicKeys[part.Ref][:], body, part.Sig[:])
	}
	verified, _ := batch.Verify(nil)
	return verified
}

// Spend transfers an amount to the address specified in SpendArguments.
func (ms *MultiSig) Spend(host core.Host, args *SpendArguments) error {
	return host.Transfer(args.Destination, args.Amount)
}
