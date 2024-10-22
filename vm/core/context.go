package core

import (
	"bytes"
	"fmt"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// Context serves 2 purposes:
// - maintains changes to the system state, that will be applied only after succesful execution
// - accumulates set of reusable objects and data.
type Context struct {
	Registry HandlerRegistry
	Loader   AccountLoader

	// LayerID of the block.
	LayerID   LayerID
	GenesisID types.Hash20

	PrincipalHandler  Handler
	PrincipalTemplate Template
	PrincipalAccount  Account

	ParseOutput ParseOutput
	Gas         struct {
		BaseGas  uint64
		FixedGas uint64
	}
	Header Header
	Args   scale.Encodable

	// consumed is in gas units and will be used
	consumed uint64
	// fee is in coins units
	fee uint64
	// an amount transfrered to other accounts
	transferred uint64

	touched []Address
	changed map[Address]*Account
}

// Principal returns address of the account that signed the transaction and pays for the gas.
func (c *Context) Principal() Address {
	return c.PrincipalAccount.Address
}

// Nonce returns the transaction nonce.
func (c *Context) Nonce() uint64 {
	return c.ParseOutput.Nonce
}

// TemplateAddress returns the address of the principal account template.
func (c *Context) TemplateAddress() Address {
	return c.Header.TemplateAddress
}

// MaxGas returns the maximum amount of gas that can be consumed by the transaction.
func (c *Context) MaxGas() uint64 {
	return c.Header.MaxGas
}

// Layer returns block layer id.
func (c *Context) Layer() LayerID {
	return c.LayerID
}

// GetGenesisID returns genesis id.
func (c *Context) GetGenesisID() Hash20 {
	return c.GenesisID
}

// Balance returns the principal account balance.
func (c *Context) Balance() uint64 { return c.PrincipalAccount.Balance }

// Template of the principal account.
func (c *Context) Template() Template {
	return c.PrincipalTemplate
}

// Handler of the principal account.
func (c *Context) Handler() Handler {
	return c.PrincipalHandler
}

// Spawn account.
func (c *Context) Spawn(args scale.Encodable) error {
	account, err := c.load(ComputePrincipal(c.Header.TemplateAddress, args))
	if err != nil {
		return err
	}
	if account.TemplateAddress != nil {
		return ErrSpawned
	}
	handler := c.Registry.Get(c.Header.TemplateAddress)
	if handler == nil {
		return fmt.Errorf("%w: spawn is called with unknown handler", ErrInternal)
	}
	buf := bytes.NewBuffer(nil)
	// instance, err := handler.New(args)
	// if err != nil {
	// 	return fmt.Errorf("%w: %w", ErrMalformed, err)
	// }
	// _, err = instance.EncodeScale(scale.NewEncoder(buf))
	// if err != nil {
	// 	return fmt.Errorf("%w: %w", ErrInternal, err)
	// }
	account.State = buf.Bytes()
	account.TemplateAddress = &c.Header.TemplateAddress
	c.change(account)
	return nil
}

// Transfer amount to the address after validation passes.
func (c *Context) Transfer(to Address, amount uint64) error {
	return c.transfer(&c.PrincipalAccount, to, amount, c.Header.MaxSpend)
}

func (c *Context) transfer(from *Account, to Address, amount, max uint64) error {
	account, err := c.load(to)
	if err != nil {
		return err
	}
	if amount > from.Balance {
		return ErrNoBalance
	}
	if c.transferred+amount > max {
		return fmt.Errorf("%w: %d", ErrMaxSpend, max)
	}
	// noop. only gas is consumed
	if from.Address == to {
		return nil
	}

	c.transferred += amount
	from.Balance -= amount
	account.Balance += amount
	c.change(account)
	return nil
}

// Consume gas from the account after validation passes.
func (c *Context) Consume(gas uint64) (err error) {
	amount := gas * c.Header.GasPrice
	if amount > c.PrincipalAccount.Balance {
		amount = c.PrincipalAccount.Balance
		err = ErrOutOfGas
	} else if total := c.consumed + gas; total > c.Header.MaxGas {
		gas = c.Header.MaxGas - c.consumed
		amount = gas * c.Header.GasPrice
		err = ErrMaxGas
	}
	c.consumed += gas
	c.fee += amount
	c.PrincipalAccount.Balance -= amount
	return err
}

// Apply is executed if transaction was consumed.
func (c *Context) Apply(updater AccountUpdater) error {
	c.PrincipalAccount.NextNonce = c.Header.Nonce + 1
	if err := updater.Update(c.PrincipalAccount); err != nil {
		return fmt.Errorf("%w: %w", ErrInternal, err)
	}
	for _, address := range c.touched {
		account := c.changed[address]
		if err := updater.Update(*account); err != nil {
			return fmt.Errorf("%w: %w", ErrInternal, err)
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
	rst = append(rst, c.PrincipalAccount.Address)
	rst = append(rst, c.touched...)
	return rst
}

func (c *Context) load(address types.Address) (*Account, error) {
	if address == c.Principal() {
		return &c.PrincipalAccount, nil
	}
	if c.changed == nil {
		c.changed = map[Address]*Account{}
	}
	account, exist := c.changed[address]
	if !exist {
		loaded, err := c.Loader.Get(address)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInternal, err)
		}
		account = &loaded
	}
	return account, nil
}

func (c *Context) change(account *Account) {
	if account.Address == c.Principal() {
		return
	}
	_, exist := c.changed[account.Address]
	if !exist {
		c.touched = append(c.touched, account.Address)
	}
	c.changed[account.Address] = account
}
