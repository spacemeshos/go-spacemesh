package state

import (
	"io"
	"math/big"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/rlp"
)

// AccountState is the interface defined to query a single account state
type AccountState interface {
	GetBalance() uint64
	GetNonce() uint64
	SetNonce(newNonce uint64)
	AddBalance(amount uint64)
	SubBalance(amount uint64)
	SetBalance(amount uint64)
	GetAddress() types.Address
}

// Object is the struct in which account information is stored. It contains account info such as nonce and balance
// and also the accounts address, address hash and a reference to this structs containing database
type Object struct {
	address  types.Address
	addrHash types.Hash32
	account  types.AccountState
	db       *DB
}

// newObject creates a state object.
func newObject(db *DB, address types.Address, data types.AccountState) *Object {
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
func (state *Object) AddBalance(amount uint64) {
	// EIP158: We must check emptiness for the objects such that the account
	// clearing (0,0,0 objects) can take effect.
	if amount == 0 {
		if state.empty() {
			state.touch()
		}
		return
	}
	state.SetBalance(state.Balance() + amount)
}

func (state *Object) touch() {
	state.db.makeDirtyObj(state)
}

// empty returns whether the account is considered empty.
func (state *Object) empty() bool {
	return state.account.Nonce == 0 && state.account.Balance == 0
}

// SubBalance removes amount from c's balance.
// It is used to remove funds from the origin account of a transfer.
func (state *Object) SubBalance(amount uint64) {
	if amount == 0 {
		return
	}
	state.SetBalance(state.Balance() - amount)
}

// SetBalance sets the balance for current account
func (state *Object) SetBalance(amount uint64) {
	state.setBalance(amount)
	state.db.makeDirtyObj(state)
}

func (state *Object) setBalance(amount uint64) {
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
func (state *Object) Balance() uint64 {
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
