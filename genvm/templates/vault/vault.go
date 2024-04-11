package vault

import (
	"errors"
	"math/big"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/genvm/core"
)

var (
	// ErrNotOwner is raised if Spend is not executed by a principal that matches owner.
	ErrNotOwner = errors.New("vault: not an owner")
	// ErrAmountNotAvailable if Spend overlows available amount (see method with the same name).
	ErrAmountNotAvailable = errors.New("vault: amount not available")
)

const (
	VAULT_STATE_SIZE = core.ACCOUNT_HEADER_SIZE + 56
	DRAINED_SIZE     = 8
)

//go:generate scalegen

type Vault struct {
	Owner               core.Address
	TotalAmount         uint64
	InitialUnlockAmount uint64
	VestingStart        core.LayerID
	VestingEnd          core.LayerID

	DrainedSoFar uint64
}

func (v *Vault) isOwner(address core.Address) bool {
	return v.Owner == address
}

func (v *Vault) Available(lid core.LayerID) uint64 {
	if lid.Before(v.VestingStart) {
		return 0
	}
	if !lid.Before(v.VestingEnd) {
		return v.TotalAmount
	}
	incremental := new(big.Int).SetUint64(v.TotalAmount)
	incremental.Mul(incremental, new(big.Int).SetUint64(uint64(lid.Difference(v.VestingStart))))
	incremental.Div(incremental, new(big.Int).SetUint64(uint64(v.VestingEnd.Difference(v.VestingStart))))
	return incremental.Uint64()
}

// Spend transaction.
func (v *Vault) Spend(host core.Host, to core.Address, amount uint64) error {
	if !v.isOwner(host.Principal()) {
		return ErrNotOwner
	}
	available := v.Available(host.Layer())
	if available > v.TotalAmount {
		panic("wrong math")
	}
	if amount > available-v.DrainedSoFar {
		return ErrAmountNotAvailable
	}
	if err := host.Transfer(to, amount); err != nil {
		return err
	}
	v.DrainedSoFar += amount
	return nil
}

// MaxSpend is noop for this template type, principal of this account type can't submit transactions.
func (v *Vault) MaxSpend(uint8, any) (uint64, error) {
	return 0, nil
}

func (v *Vault) BaseGas(uint8) uint64 {
	return 0
}

func (v *Vault) LoadGas() uint64 {
	return 0
}

func (v *Vault) ExecGas(uint8) uint64 {
	return 0
}

// Verify always returns false.
func (v *Vault) Verify(core.Host, []byte, *scale.Decoder) bool {
	return false
}
