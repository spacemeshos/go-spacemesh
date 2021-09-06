package state

import (
	"fmt"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/rlp"
	"github.com/spacemeshos/go-spacemesh/trie"
)

// DB is the struct that performs logging of all account states. It consists of a state trie that contains all
// account data in its leaves. It also stores a dirty object list to dump when state is committed into db
type DB struct {
	globalTrie Trie
	db         Database // todo: maybe remove
	// todo: add journal
	lock sync.Mutex

	// This map holds 'live' objects, which will get modified while processing a state transition.
	stateObjects      map[types.Address]*Object
	stateObjectsDirty map[types.Address]struct{}

	// DB error.
	// State objects are used by the consensus core and VM which are
	// unable to deal with database-level errors. Any error that occurs
	// during a database read is memoized here and will eventually be returned
	// by DB.Commit.
	dbErr error // todo: do we need this?
}

// New create a new state from a given trie.
func New(root types.Hash32, db Database) (*DB, error) {
	tr, err := db.OpenTrie(root)
	if err != nil {
		return nil, err
	}
	return &DB{
		db:                db,
		globalTrie:        tr,
		stateObjects:      make(map[types.Address]*Object),
		stateObjectsDirty: make(map[types.Address]struct{}),
	}, nil
}

// setError remembers the first non-nil error it is called with.
func (state *DB) setError(err error) {
	if state.dbErr == nil {
		state.dbErr = err
	}
}

// Error returns db error if it occurred
func (state *DB) Error() error {
	return state.dbErr
}

// GetAllAccounts returns a dump of all accounts in global state
func (state *DB) GetAllAccounts() (*types.MultipleAccountsState, error) {
	// Commit state to store so accounts in memory are included
	if _, err := state.Commit(); err != nil {
		return nil, err
	}

	// We cannot lock before the call to Commit since that also acquires a lock
	// But we need to lock here to ensure consistency between the state root and
	// the account data
	state.lock.Lock()
	defer state.lock.Unlock()
	accounts := state.RawDump()
	return &accounts, nil
}

// Exist reports whether the given account address exists in the state.
// Notably this also returns true for suicided accounts.
func (state *DB) Exist(addr types.Address) bool {
	return state.getStateObj(addr) != nil
}

// Empty returns whether the state object is either non-existent
// or empty according to the EIP161 specification (balance = nonce = code = 0)
func (state *DB) Empty(addr types.Address) bool {
	state.lock.Lock()
	defer state.lock.Unlock()
	so := state.getStateObj(addr)
	return so == nil || so.empty()
}

// GetBalance Retrieve the balance from the given address or 0 if object not found
func (state *DB) GetBalance(addr types.Address) uint64 {
	StateObj := state.getStateObj(addr)
	if StateObj != nil {
		return StateObj.Balance()
	}
	return 0
}

// GetNonce gets the current nonce of the given addr, if the address is not found it returns 0
func (state *DB) GetNonce(addr types.Address) uint64 {
	StateObj := state.getStateObj(addr)
	if StateObj != nil {
		return StateObj.Nonce()
	}

	return 0
}

/*
 * SETTERS
 */

// AddBalance adds amount to the account associated with addr.
func (state *DB) AddBalance(addr types.Address, amount uint64) {
	stateObj := state.GetOrNewStateObj(addr)
	if stateObj != nil {
		stateObj.AddBalance(amount)
	}
}

// SubBalance subtracts amount from the account associated with addr.
func (state *DB) SubBalance(addr types.Address, amount uint64) {
	StateObj := state.GetOrNewStateObj(addr)
	if StateObj != nil {
		StateObj.SubBalance(amount)
	}
}

// SetBalance sets balance to the specific address, it does not return error if address was not found
func (state *DB) SetBalance(addr types.Address, amount uint64) {
	stateObj := state.GetOrNewStateObj(addr)
	if stateObj != nil {
		stateObj.SetBalance(amount)
	}
}

// SetNonce sets nonce to the specific address, it does not return error if address was not found
func (state *DB) SetNonce(addr types.Address, nonce uint64) {
	stateObj := state.GetOrNewStateObj(addr)
	if stateObj != nil {
		stateObj.SetNonce(nonce)
	}
}

//
// Setting, updating & deleting state object methods.
//

// updateStateObj writes the given object to the trie.
func (state *DB) updateStateObj(StateObj *Object) {
	addr := StateObj.Address()
	data, err := rlp.EncodeToBytes(StateObj)
	if err != nil {
		panic(fmt.Errorf("can't encode object at %x: %v", addr[:], err))
	}
	state.setError(state.globalTrie.TryUpdate(addr[:], data))
}

// Retrieve a state object given by the types. Returns nil if not found.
func (state *DB) getStateObj(addr types.Address) (StateObj *Object) {
	state.lock.Lock()
	defer state.lock.Unlock()

	// Prefer 'live' objects.
	if obj := state.stateObjects[addr]; obj != nil {
		/*if obj.deleted {
			return nil
		}*/
		return obj
	}

	// Load the object from the database.
	enc, err := state.globalTrie.TryGet(addr[:])
	if len(enc) == 0 {
		state.setError(err)
		return nil
	}
	var data types.AccountState
	if err := rlp.DecodeBytes(enc, &data); err != nil {
		log.Error("Failed to decode state object", "addr", addr, "err", err)
		return nil
	}

	// Insert into the live set.
	obj := newObject(state, addr, data)
	state.setStateObj(obj)
	return obj
}

func (state *DB) makeDirtyObj(obj *Object) {
	state.stateObjectsDirty[obj.address] = struct{}{}
}

func (state *DB) setStateObj(object *Object) {
	state.stateObjects[object.Address()] = object
}

// GetOrNewStateObj retrieve a state object or create a new state object if nil.
func (state *DB) GetOrNewStateObj(addr types.Address) *Object {
	stateObj := state.getStateObj(addr)
	if stateObj == nil { // || Object.deleted {
		stateObj, _ = state.createObject(addr)
	}
	return stateObj
}

// createObject creates a new state object. If there is an existing account with
// the given address, it is overwritten and returned as the second return value.
func (state *DB) createObject(addr types.Address) (newObj, prev *Object) {
	prev = state.getStateObj(addr)
	newObj = newObject(state, addr, types.AccountState{})
	newObj.setNonce(0)
	/*if prev == nil {
		state.journal.append(createObjectChange{account: &addr})
	} else {
		state.journal.append(resetObjectChange{prev: prev})
	}*/
	state.lock.Lock()
	defer state.lock.Unlock()
	state.setStateObj(newObj)
	state.makeDirtyObj(newObj)
	return newObj, prev
}

// CreateAccount explicitly creates a state object. If a state object with the address
// already exists the balance is carried over to the new account.
//
// CreateAccount is called during the EVM CREATE operation. The situation might arise that
// a contract does the following:
//
//   1. sends funds to sha(account ++ (nonce + 1))
//   2. tx_create(sha(account ++ nonce)) (note that this gets the address of 1)
//
// Carrying over the balance ensures that Ether doesn't disappear.
func (state *DB) CreateAccount(addr types.Address) {
	newObj, prev := state.createObject(addr)
	if prev != nil {
		newObj.setBalance(prev.account.Balance)
	}
}

// Copy creates a deep, independent copy of the state.
// Snapshots of the copied state cannot be applied to the copy.
func (state *DB) Copy() *DB {
	state.lock.Lock()
	defer state.lock.Unlock()

	// Copy all the basic fields, initialize the memory ones
	st := &DB{
		db:                state.db,
		globalTrie:        state.db.CopyTrie(state.globalTrie),
		stateObjects:      make(map[types.Address]*Object),
		stateObjectsDirty: make(map[types.Address]struct{}),
	}

	for addr := range state.stateObjectsDirty {
		if _, exist := st.stateObjects[addr]; !exist {
			st.stateObjects[addr] = state.stateObjects[addr].deepCopy(state)
			st.stateObjectsDirty[addr] = struct{}{}
		}
	}

	return st
}

// Commit writes the state to the underlying in-memory trie database.
func (state *DB) Commit() (root types.Hash32, err error) {
	state.lock.Lock()
	defer state.lock.Unlock()

	// Commit objects to the trie.
	for addr, stateObject := range state.stateObjects {
		_, isDirty := state.stateObjectsDirty[addr]

		if isDirty {
			state.updateStateObj(stateObject)
		}
	}

	// Write trie changes.
	root, err = state.globalTrie.Commit(nil)
	return root, err
}

// IntermediateRoot computes the current root hash of the state trie.
// It is called in between transactions to get the root hash that
// goes into transaction receipts.
func (state *DB) IntermediateRoot(deleteEmptyObjects bool) types.Hash32 {
	return state.globalTrie.Hash()
}

// TrieDB retrieves the low level trie database used for data storage.
func (state *DB) TrieDB() *trie.Database {
	return state.db.TrieDB()
}
