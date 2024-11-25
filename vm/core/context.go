package core

import (
	"fmt"
	"maps"
	"math"

	"github.com/spacemeshos/go-scale"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type StorageStatus int

const (
	StorageStatusAdded StorageStatus = iota
	StorageStatusModified
	StorageStatusError
)

// Context serves 2 purposes:
// - maintains changes to the system state, that will be applied only after successful execution
// - accumulates set of reusable objects and data.
type Context struct {
	Registry HandlerRegistry
	Loader   AccountLoader

	// LayerID of the block.
	LayerID   LayerID
	GenesisID types.Hash20

	PrincipalAddress   Address
	PrincipalHandler   Handler
	PrincipalTemplate  Template
	PrincipalNextNonce uint64

	ParseOutput ParseOutput
	Gas         struct {
		BaseGas  uint64
		FixedGas uint64
	}
	Header  Header
	Args    scale.Encodable
	SpawnTx bool

	Logger *zap.Logger

	// consumed is in gas units and is actually subtracted from the principal account balance,
	// i.e., it's a "hold" on the account funds. this includes a "hold" on maxspend.
	consumed uint64
	// gas spent is the number of gas units actually spent on computation.
	gasSpent uint64
	// fee is in coins units
	fee uint64
	// an amount transferred to other accounts
	transferred uint64

	touched []Address
	changed map[Address]*Account
}

// Clone returns a shallow copy of the context.
func (c *Context) Clone() Host {
	clone := *c
	clone.changed = maps.Clone(c.changed)
	return &clone
}

// PrincipalAccount returns the current state of the principal account.
func (c *Context) PrincipalAccount() (*Account, error) {
	return c.load(c.PrincipalAddress)
}

// Principal returns address of the account that signed the transaction and pays for the gas.
func (c *Context) Principal() Address {
	return c.PrincipalAddress
}

// NextNonce returns the next nonce of the principal account.
func (c *Context) NextNonce() uint64 {
	return c.PrincipalNextNonce
}

// Nonce returns the transaction nonce.
func (c *Context) Nonce() uint64 {
	return c.ParseOutput.Nonce
}

// Nonce returns the transaction nonce.
func (c *Context) Payload() Payload {
	return c.ParseOutput.Payload
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
func (c *Context) Balance() (uint64, error) {
	acct, err := c.PrincipalAccount()
	if err != nil {
		return 0, err
	}
	return acct.Balance, nil
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
func (c *Context) Spawn(template Address, blob []byte) (Address, error) {
	// calculate new principal address
	principalAddress := ComputePrincipalFromBlob(template, blob)

	// check if the account is already spawned
	account, err := c.load(principalAddress)
	if err != nil {
		return Address{}, err
	}
	// the account is already spawned and contains different code. this should not happen.
	// if len(account.State) > 0 && !bytes.Equal(account.State, blob) {

	// do not allow an already-spawned account to be spawned again
	if account.TemplateAddress != nil {
		return Address{}, ErrSpawned
	}

	account.State = blob
	account.TemplateAddress = &template
	c.change(account)
	c.Logger.Debug(
		"spawn",
		zap.Stringer("address", principalAddress),
		zap.Stringer("template", template),
		zap.Int("state_size", len(blob)),
	)
	return principalAddress, nil
}

// SetStorage sets the storage value for the account.
func (c *Context) SetStorage(address Address, key, value [32]byte) (StorageStatus, error) {
	account, err := c.load(address)
	if err != nil {
		return StorageStatusError, err
	}

	defer c.change(account)

	// TODO(lane): make this more efficient
	// right now this is an array rather than a map to make serialization easier
	for i, item := range account.Storage {
		if item.Key == key {
			account.Storage[i].Value = value
			return StorageStatusModified, nil
		}
	}
	account.Storage = append(account.Storage, types.StorageItem{Key: key, Value: value})
	return StorageStatusAdded, nil
}

// IsSpawn returns whether the transaction is a spawn transaction.
func (c *Context) IsSpawn() bool {
	return c.SpawnTx
}

// Transfer amount to the address after validation passes.
func (c *Context) Transfer(to Address, amount uint64) error {
	acct, err := c.PrincipalAccount()
	if err != nil {
		return err
	}
	// no-op
	if amount == 0 {
		c.Logger.Debug("ignoring zero-value transfer")
		return nil
	}
	return c.transfer(acct, to, amount, c.Header.MaxSpend)
}

func safeAdd(a, b uint64) (uint64, error) {
	if a > math.MaxUint64-b {
		return 0, ErrOverflow
	}
	return a + b, nil
}

func (c *Context) transfer(from *Account, to Address, amount, max uint64) error {
	account, err := c.load(to)
	if err != nil {
		return err
	}
	if amount > from.Balance {
		return ErrNoBalance
	}
	if totalTransfer, err := safeAdd(c.transferred, amount); err != nil {
		return err
	} else if totalTransfer > max {
		return fmt.Errorf("%w: %d", ErrMaxSpend, max)
	}

	// noop. only gas is consumed
	if from.Address == to {
		return nil
	}

	if newBalance, err := safeAdd(account.Balance, amount); err != nil {
		return err
	} else {
		account.Balance = newBalance
	}
	c.transferred += amount
	from.Balance -= amount
	c.change(from)
	c.change(account)
	c.Logger.Debug(
		"transfer",
		zap.Uint64("amount", amount),
		zap.Stringer("from", from.Address),
		zap.Stringer("to", to),
		zap.Uint64("from_new_balance", from.Balance),
		zap.Uint64("to_new_balance", account.Balance),
	)
	return nil
}

// SpendGas marks gas as consumed.
func (c *Context) SpendGas(gas uint64) {
	c.gasSpent += gas
}

// GasSpent returns the amount of gas spent.
func (c *Context) GasSpent() uint64 {
	return c.gasSpent
}

// Consume gas from the account after validation passes.
func (c *Context) Consume(gas uint64) (err error) {
	principalAccount, err := c.PrincipalAccount()
	if err != nil {
		return err
	}
	amount := gas * c.Header.GasPrice
	if amount > principalAccount.Balance {
		amount = principalAccount.Balance
		err = ErrOutOfGas
	} else if total := c.consumed + gas; total > c.Header.MaxGas {
		gas = c.Header.MaxGas - c.consumed
		amount = gas * c.Header.GasPrice
		err = ErrMaxGas
	}
	c.consumed += gas
	c.fee += amount
	principalAccount.Balance -= amount
	c.change(principalAccount)

	c.Logger.Debug(
		"consume",
		zap.String("principal", c.PrincipalAddress.String()),
		zap.Uint64("gas", gas),
		zap.Uint64("fee", amount),
		zap.Uint64("consumed", c.consumed),
	)

	return err
}

// Refund refunds gas remaining after execution.
func (c *Context) Refund() (err error) {
	// TODO(lane): safe math
	unspent := c.consumed - c.gasSpent
	principalAccount, err := c.PrincipalAccount()
	if err != nil {
		return err
	}
	amount := unspent * c.Header.GasPrice
	c.consumed -= unspent
	c.fee -= amount
	principalAccount.Balance += amount
	c.change(principalAccount)

	c.Logger.Debug(
		"refund",
		zap.String("principal", c.PrincipalAddress.String()),
		zap.Uint64("consumed", c.consumed),
		zap.Uint64("gas spent", c.gasSpent),
		zap.Uint64("unspent gas", unspent),
		zap.Uint64("fee", amount),
	)

	return nil
}

// Apply is executed if transaction was consumed.
func (c *Context) Apply(updater AccountUpdater) error {
	acct, err := c.PrincipalAccount()
	if err != nil {
		return err
	}
	acct.NextNonce = c.Header.Nonce + 1
	if err := updater.Update(*acct); err != nil {
		return fmt.Errorf("%w: %w", ErrInternal, err)
	}
	for _, address := range c.touched {
		if address == c.PrincipalAddress {
			continue
		}
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
	rst = append(rst, c.touched...)
	return rst
}

func (c *Context) Has(address types.Address) (bool, error) {
	_, err := c.load(address)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (c *Context) Get(address types.Address) (Account, error) {
	addr, err := c.load(address)
	return *addr, err
}

func (c *Context) load(address types.Address) (*Account, error) {
	if c.changed == nil {
		c.changed = map[Address]*Account{}
	}
	account, exist := c.changed[address]
	if !exist {
		loaded, err := c.Loader.Get(address)
		if err != nil {
			return nil, fmt.Errorf("%w: error loading account: %w", ErrInternal, err)
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
