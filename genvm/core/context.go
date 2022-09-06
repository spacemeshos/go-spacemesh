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
	Registry HandlerRegistry
	Loader   AccountLoader
	Handler  Handler
	Template Template

	Account Account

	ParseOutput ParseOutput
	Header      Header
	Args        scale.Encodable

	// consumed is in gas units and will be used
	consumed uint64
	// fee is in coins units
	fee uint64
	// an amount transfrered to other accounts
	transferred uint64

	// TODO all templates for genesis will support transfers to only one account.
	// i keep it for the purposes of testing and validation (e.g we can implement more complex templates)
	// but it can be simplified down to one variable
	touched []Address
	changed map[Address]*Account
}

// Spawn account.
func (c *Context) Spawn(args scale.Encodable) error {
	spawned := ComputePrincipal(c.Header.TemplateAddress, args)
	var account *types.Account
	if spawned == c.Account.Address {
		account = &c.Account
	} else {
		var err error
		account, err = c.load(spawned)
		if err != nil {
			return err
		}
	}
	if account.TemplateAddress != nil {
		return ErrSpawned
	}
	handler := c.Registry.Get(c.Header.TemplateAddress)
	if handler == nil {
		return fmt.Errorf("%w: spawn is called with unknown handler", ErrInternal)
	}
	buf := bytes.NewBuffer(nil)
	instance, err := handler.New(args)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrMalformed, err.Error())
	}
	_, err = instance.EncodeScale(scale.NewEncoder(buf))
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInternal, err)
	}
	account.State = buf.Bytes()
	account.TemplateAddress = &c.Header.TemplateAddress
	if account.Address != c.Account.Address {
		c.change(account)
	}
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
	account, err := c.load(to)
	if err != nil {
		return err
	}
	c.Account.Balance -= amount
	account.Balance += amount
	c.change(account)
	return nil
}

// Consume gas from the account after validation passes.
func (c *Context) Consume(gas uint64) (err error) {
	amount := gas * c.Header.GasPrice
	if amount > c.Account.Balance {
		amount = c.Account.Balance
		err = ErrNoBalance
	} else if total := c.consumed + gas; total > c.Header.MaxGas {
		gas = c.Header.MaxGas - c.consumed
		amount = gas * c.Header.GasPrice
		err = ErrMaxGas
	}
	c.consumed += gas
	c.fee += amount
	c.Account.Balance -= amount
	return err
}

// Apply is executed if transaction was consumed.
func (c *Context) Apply(updater AccountUpdater) error {
	c.Account.NextNonce = c.Header.Nonce.Counter + 1
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
	return c.fee
}

// Updated list of addresses.
func (c *Context) Updated() []types.Address {
	rst := make([]types.Address, 0, len(c.touched)+1)
	rst = append(rst, c.Account.Address)
	rst = append(rst, c.touched...)
	return rst
}

func (c *Context) load(address types.Address) (*Account, error) {
	if c.changed == nil {
		c.changed = map[Address]*Account{}
	}
	account, exist := c.changed[address]
	if !exist {
		loaded, err := c.Loader.Get(address)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrInternal, err.Error())
		}
		account = &loaded
	}
	return account, nil
}

func (c *Context) change(account *Account) {
	_, exist := c.changed[account.Address]
	if !exist {
		c.touched = append(c.touched, account.Address)
	}
	c.changed[account.Address] = account
}
