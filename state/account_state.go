package state

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/rlp"
	"io"
	"math/big"
)

type AccountState interface {
	GetBalance() *big.Int
	GetNonce() uint64
	SetNonce(newNonce uint64)
	AddBalance(amount *big.Int)
	SubBalance(amount *big.Int)
	SetBalance(amount *big.Int)
	GetAddress() types.Address
}

type StateObj struct {
	address  types.Address
	addrHash types.Hash32
	account  Account
	db       *StateDB
}

type Account struct {
	Nonce   uint64
	Balance *big.Int
}

// newObject creates a state object.
func newObject(db *StateDB, address types.Address, data Account) *StateObj {
	if data.Balance == nil {
		data.Balance = new(big.Int)
	}
	return &StateObj{
		db:       db,
		address:  address,
		addrHash: crypto.Keccak256Hash(address[:]),
		account:  data,
	}
}

// EncodeRLP implements rlp.Encoder.
func (c *StateObj) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, c.account)
}

// AddBalance removes amount from c's balance.
// It is used to add funds to the destination account of a transfer.
func (c *StateObj) AddBalance(amount *big.Int) {
	// EIP158: We must check emptiness for the objects such that the account
	// clearing (0,0,0 objects) can take effect.
	if amount.Sign() == 0 {
		if c.empty() {
			c.touch()
		}

		return
	}
	c.SetBalance(new(big.Int).Add(c.Balance(), amount))
}

func (c *StateObj) touch() {
	c.db.makeDirtyObj(c)
}

// empty returns whether the account is considered empty.
func (s *StateObj) empty() bool {
	return s.account.Nonce == 0 && s.account.Balance.Sign() == 0
}

// SubBalance removes amount from c's balance.
// It is used to remove funds from the origin account of a transfer.
func (c *StateObj) SubBalance(amount *big.Int) {
	if amount.Sign() == 0 {
		return
	}
	c.SetBalance(new(big.Int).Sub(c.Balance(), amount))
}

func (self *StateObj) SetBalance(amount *big.Int) {
	self.setBalance(amount)
	self.db.makeDirtyObj(self)
}

func (self *StateObj) setBalance(amount *big.Int) {
	self.account.Balance = amount
}

// Return the gas back to the origin. Used by the Virtual machine or Closures
func (c *StateObj) ReturnGas(gas *big.Int) {}

func (self *StateObj) deepCopy(db *StateDB) *StateObj {
	StateObj := newObject(db, self.address, self.account)

	return StateObj
}

//
// Attribute accessors
//

// Returns the address of the contract/account
func (c *StateObj) Address() types.Address {
	return c.address
}

func (self *StateObj) SetNonce(nonce uint64) {
	self.setNonce(nonce)
	self.db.makeDirtyObj(self)
}

func (self *StateObj) setNonce(nonce uint64) {
	self.account.Nonce = nonce
}

func (self *StateObj) Balance() *big.Int {
	return self.account.Balance
}

func (self *StateObj) Nonce() uint64 {
	return self.account.Nonce
}

// Never called, but must be present to allow StateObj to be used
// as a vm.Account interface that also satisfies the vm.ContractRef
// interface. Interfaces are awesome.
func (self *StateObj) Value() *big.Int {
	panic("Value on StateObj should never be called")
}
