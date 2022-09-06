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

	LayerID LayerID

	PrincipalHandler  Handler
	PrincipalTemplate Template
	PrincipalAccount  Account

	ParseOutput ParseOutput
	Header      Header
	Args        scale.Encodable

	// consumed is in gas units and will be used
	consumed uint64
	// fee is in coins units
	fee uint64
	// an amount transfrered to other accounts
	transferred uint64

	touched []Address
	changed map[Address]*Account
}

// Principal returns address of the account that signed transaction.
func (c *Context) Principal() Address {
	return c.PrincipalAccount.Address
}

// Layer returns block layer id.
func (c *Context) Layer() LayerID {
	return c.LayerID
}

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

// Relay call to the remote account.
func (c *Context) Relay(remoteTemplate, address Address, call func(Host) error) error {
	account, err := c.load(address)
	if err != nil {
		return err
	}
	if account.TemplateAddress == nil {
		return ErrNotSpawned
	}
	if *account.TemplateAddress != remoteTemplate {
		return fmt.Errorf("%w: %s != %s", ErrTemplateMismatch, remoteTemplate.String(), account.TemplateAddress.String())
	}
	handler := c.Registry.Get(remoteTemplate)
	if handler == nil {
		panic("template of the spawned account should exist in the registry")
	}
	template, err := handler.Load(account.State)
	if err != nil {
		return err
	}

	remote := &RemoteContext{
		Context:  c,
		remote:   account,
		handler:  handler,
		template: template,
	}
	if err := call(remote); err != nil {
		return err
	}
	// ideally such changes would be serialized once for the whole block execution
	// but it requires more changes in the cache, so can be done as an optimization
	// if it proves meaningful (most likely wont)
	buf := bytes.NewBuffer(nil)
	encoder := scale.NewEncoder(buf)
	if _, err := template.EncodeScale(encoder); err != nil {
		return fmt.Errorf("%w: %s", ErrInternal, err.Error())
	}
	account.State = buf.Bytes()
	c.change(account)
	return nil
}

// Consume gas from the account after validation passes.
func (c *Context) Consume(gas uint64) (err error) {
	amount := gas * c.Header.GasPrice
	if amount > c.PrincipalAccount.Balance {
		amount = c.PrincipalAccount.Balance
		err = ErrNoBalance
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
	c.PrincipalAccount.NextNonce = c.Header.Nonce.Counter + 1
	if err := updater.Update(c.PrincipalAccount); err != nil {
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
			return nil, fmt.Errorf("%w: %s", ErrInternal, err.Error())
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

// RemoteContext ...
type RemoteContext struct {
	*Context
	remote   *Account
	handler  Handler
	template Template
}

// Template ...
func (r *RemoteContext) Template() Template {
	return r.template
}

// Handler ...
func (r *RemoteContext) Handler() Handler {
	return r.handler
}

// Transfer ...
func (r *RemoteContext) Transfer(to Address, amount uint64) error {
	if err := r.transfer(r.remote, to, amount, r.remote.Balance); err != nil {
		return err
	}
	return nil
}
