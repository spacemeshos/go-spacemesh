package wallet

import (
	"fmt"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"

	"github.com/spacemeshos/go-spacemesh/genvm/core"
)

func New(args *SpawnArguments) *Wallet {
	return &Wallet{PublicKey: args.PublicKey}
}

//go:generate scalegen -pkg wallet -file wallet_scale.go -types Wallet -imports github.com/spacemeshos/go-spacemesh/genvm/wallet

type Wallet struct {
	PublicKey core.PublicKey
}

func (s *Wallet) MaxSpend(method uint8, args any) (uint64, error) {
	switch method {
	case 0:
		return 0, nil
	case 1:
		return args.(*SpendArguments).Amount, nil
	default:
		return 0, fmt.Errorf("%w: unknown method %d", core.ErrMalformed, method)
	}
}

func (s *Wallet) Verify(ctx *core.Context, tx []byte) bool {
	if len(tx) < 64 {
		return false
	}
	if len(s.PublicKey) != ed25519.PublicKeySize {
		return false
	}
	hash := core.Hash(tx[:len(tx)-64])
	return ed25519.Verify(ed25519.PublicKey(s.PublicKey[:]), hash[:], tx[len(tx)-64:])
}

func (s *Wallet) Spend(ctx *core.Context, args *SpendArguments) error {
	return ctx.Transfer(args.Destination, args.Amount)
}
