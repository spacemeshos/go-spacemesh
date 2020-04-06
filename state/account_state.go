package state

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/rlp"
	"io"
	"math/big"
)

// AccountState is the interface defined to query a single account state
type AccountState interface {
	GetBalance() *big.Int
	GetNonce() uint64
	SetNonce(newNonce uint64)
	AddBalance(amount *big.Int)
	SubBalance(amount *big.Int)
	SetBalance(amount *big.Int)
	GetAddress() types.Address
}

// Object is the struct in which account information is stored. It contains account info such as nonce and balance
// and also the accounts address, address hash and a reference to this structs containing database
type Object struct {
	address  types.Address
	addrHash types.Hash32
	account  Account
	db       *DB
}

// Account struct represents basic account info: nonce and balance
type Account struct {
	Nonce   uint64
	Balance *big.Int
}

// newObject creates a state object.
func newObject(db *DB, address types.Address, data Account) *Object {
	if data.Balance == nil {
		data.Balance = new(big.Int)
	}
	return &Object{
		db:       db,
		address:  address,
		addrHash: crypto.Keccak256Hash(address[:]),
		account:  data,
	}
}

// EncodeRLP implements rlp.Encoder.
func (state *Object) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, state.account)
}

// AddBalance removes amount from c's balance.
// It is used to add funds to the destination account of a transfer.
func (state *Object) AddBalance(amount *big.Int) {
	// EIP158: We must check emptiness for the objects such that the account
	// clearing (0,0,0 objects) can take effect.
	if amount.Sign() == 0 {
		if state.empty() {
			state.touch()
		}

		return
	}
	state.SetBalance(new(big.Int).Add(state.Balance(), amount))
}

func (state *Object) touch() {
	state.db.makeDirtyObj(state)
}

// empty returns whether the account is considered empty.
func (state *Object) empty() bool {
	return state.account.Nonce == 0 && state.account.Balance.Sign() == 0
}

// SubBalance removes amount from c's balance.
// It is used to remove funds from the origin account of a transfer.
func (state *Object) SubBalance(amount *big.Int) {
	if amount.Sign() == 0 {
		return
	}
	state.SetBalance(new(big.Int).Sub(state.Balance(), amount))
}

// SetBalance sets the balance for current account
func (state *Object) SetBalance(amount *big.Int) {
	state.setBalance(amount)
	state.db.makeDirtyObj(state)
}

func (state *Object) setBalance(amount *big.Int) {
	state.account.Balance = amount
}

// ReturnGas Return the gas back to the origin. Used by the Virtual machine or Closures
func (state *Object) ReturnGas(gas *big.Int) {}

func (state *Object) deepCopy(db *DB) *Object {
	StateObj := newObject(db, state.address, state.account)

	return StateObj
}

//
// Attribute accessors
//

// Address returns the address of the contract/account
func (state *Object) Address() types.Address {
	return state.address
}

// SetNonce sets the nonce to be nonce for this Object
func (state *Object) SetNonce(nonce uint64) {
	state.setNonce(nonce)
	state.db.makeDirtyObj(state)
}

func (state *Object) setNonce(nonce uint64) {
	state.account.Nonce = nonce
}

// Balance returns the account current balance
func (state *Object) Balance() *big.Int {
	return state.account.Balance
}

// Nonce returns the accounts current nonce
func (state *Object) Nonce() uint64 {
	return state.account.Nonce
}

// Value Never called, but must be present to allow Object to be used
// as a vm.Account interface that also satisfies the vm.ContractRef
// interface. Interfaces are awesome.
func (state *Object) Value() *big.Int {
	panic("Value on Object should never be called")
}
