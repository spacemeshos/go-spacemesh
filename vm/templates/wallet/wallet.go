package wallet

import (
	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/vm/core"
)

// New returns Wallet instance with SpawnArguments.
func New(args *SpawnArguments) *Wallet {
	return &Wallet{}
}

//go:generate scalegen

// Wallet is a single-key wallet.
type Wallet struct {
}

// MaxSpend returns amount specified in the SpendArguments for Spend method.
func (s *Wallet) MaxSpend(args any) (uint64, error) {
	// TODO(lane): depends on https://github.com/athenavm/athena/issues/128
	// mock for now
	return 1000000, nil
}

// Verify that transaction is signed by the owner of the PublicKey using ed25519.
func (s *Wallet) Verify(host core.Host, raw []byte, dec *scale.Decoder) bool {
	// TODO(lane): depends on https://github.com/athenavm/athena/issues/129
	// mock for now
	return true
}

func (s *Wallet) BaseGas() uint64 {
	return BaseGas()
}

func (s *Wallet) LoadGas() uint64 {
	return LoadGas()
}

func (s *Wallet) ExecGas() uint64 {
	return ExecGas()
}
