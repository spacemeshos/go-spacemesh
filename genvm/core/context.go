package core

import (
	"bytes"
	"fmt"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// Context serves 2 purposes:
// - maintains changes to the system state, that will be applied only after succeful execution
// - accumulates set of reusable objects and data.
type Context struct {
	Loader   AccountLoader
	Handler  Handler
	Template Template

	Account Account

	Header Header
	Args   scale.Encodable

	// consumed and transferred is for MaxGas/MaxSpend validation
	consumed    uint64
	transferred uint64

	// TODO all templates for genesis will support transfers to only one account.
	// i keep it for the purposes of testing and validation (e.g we can implement more complex templates)
	// but it can be simplified down to one variable
	touched    []Address
	changed    map[Address]*Account
	genesis_id [20]byte
}

// Spawn account.
func (c *Context) Spawn(template Address, args scale.Encodable) error {
	principal := ComputePrincipal(template, args)
	// TODO(dshulyak) only self-spawn is supported
	if principal != c.Header.Principal {
		return ErrSpawn
	}

	c.Account.Template = &template
	return nil
}

// Transfer amount to the address after validation passes.
func (c *Context) Transfer(to Address, amount uint64) error {
	if amount > c.Account.Balance {
		return ErrNoBalance
	}
	c.transferred += amount
	if c.transferred > c.Header.MaxSpend {
		return fmt.Errorf("%w: %d", ErrMaxSpend, c.Header.MaxSpend)
	}
	// noop. only gas is consumed
	if c.Account.Address == to {
		return nil
	}

	if c.changed == nil {
		c.changed = map[Address]*Account{}
	}
	account, exist := c.changed[to]
	if !exist {
		loaded, err := c.Loader.Get(to)
		if err != nil {
			return fmt.Errorf("%w: %s", ErrInternal, err.Error())
		}
		c.touched = append(c.touched, to)
		c.changed[to] = &loaded
		account = &loaded
	}

	c.Account.Balance -= amount
	account.Balance += amount
	return nil
}

// Consume gas from the account after validation passes.
func (c *Context) Consume(gas uint64) (err error) {
	amount := gas * c.Header.GasPrice
	if amount > c.Account.Balance {
		return ErrNoBalance
	}
	if total := c.consumed + gas; total > c.Header.MaxGas {
		gas = c.Header.MaxGas - c.consumed
		amount = gas * c.Header.GasPrice
		err = ErrMaxGas
	}
	c.consumed += gas
	c.Account.Balance -= amount
	return err
}

// Apply is executed if transaction was consumed.
func (c *Context) Apply(updater AccountUpdater) error {
	buf := bytes.NewBuffer(nil)
	encoder := scale.NewEncoder(buf)
	c.Template.EncodeScale(encoder)

	c.Account.Nonce = c.Header.Nonce.Counter
	c.Account.State = buf.Bytes()
	if err := updater.Update(c.Account); err != nil {
		return fmt.Errorf("%w: %s", ErrInternal, err.Error())
	}
	for _, address := range c.touched {
		account := c.changed[address]
		if err := updater.Update(*account); err != nil {
			return fmt.Errorf("%w: %s", ErrInternal, err.Error())
		}
	}
	return nil
}

// Consumed gas.
func (c *Context) Consumed() uint64 {
	return c.consumed
}

// Fee computed from consumed gas.
func (c *Context) Fee() uint64 {
	return c.consumed * c.Header.GasPrice
}

// Updated list of addresses.
func (c *Context) Updated() []types.Address {
	rst := make([]types.Address, len(c.touched)+1)
	rst = append(rst, c.Account.Address)
	copy(rst[1:], c.touched)
	return rst
}
