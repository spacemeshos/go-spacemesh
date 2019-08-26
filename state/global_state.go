package state

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/rlp"
	"github.com/spacemeshos/go-spacemesh/trie"
	"math/big"
	"sync"
)

type GlobalStateDB interface {
	Exist(addr types.Address) bool
	Empty(addr types.Address) bool
	GetBalance(addr types.Address) uint64
	GetNonce(addr types.Address) uint64
	AddBalance(addr types.Address, amount *big.Int)
	SubBalance(addr types.Address, amount *big.Int)
	SetNonce(addr types.Address, nonce uint64)
	GetOrNewStateObj(addr types.Address) *StateObj
	CreateAccount(addr types.Address)
	Commit(deleteEmptyObjects bool) (root types.Hash32, err error)
	//Copy() *GlobalStateDB
	IntermediateRoot(deleteEmptyObjects bool) types.Hash32
	TrieDB() *trie.Database
}

type StateDB struct {
	globalTrie Trie
	db         Database //todo: maybe remove
	//todo: add journal
	lock sync.Mutex

	// This map holds 'live' objects, which will get modified while processing a state transition.
	stateObjects      map[types.Address]*StateObj
	stateObjectsDirty map[types.Address]struct{}

	// DB error.
	// State objects are used by the consensus core and VM which are
	// unable to deal with database-level errors. Any error that occurs
	// during a database read is memoized here and will eventually be returned
	// by StateDB.Commit.
	dbErr error //todo: do we need this?
}

// Create a new state from a given trie.
func New(root types.Hash32, db Database) (*StateDB, error) {
	tr, err := db.OpenTrie(root)
	if err != nil {
		return nil, err
	}
	return &StateDB{
		db:                db,
		globalTrie:        tr,
		stateObjects:      make(map[types.Address]*StateObj),
		stateObjectsDirty: make(map[types.Address]struct{}),
	}, nil
}

// setError remembers the first non-nil error it is called with.
func (self *StateDB) setError(err error) {
	if self.dbErr == nil {
		self.dbErr = err
	}
}

func (self *StateDB) Error() error {
	return self.dbErr
}

// Exist reports whether the given account address exists in the state.
// Notably this also returns true for suicided accounts.
func (self *StateDB) Exist(addr types.Address) bool {
	return self.getStateObj(addr) != nil
}

// Empty returns whether the state object is either non-existent
// or empty according to the EIP161 specification (balance = nonce = code = 0)
func (self *StateDB) Empty(addr types.Address) bool {
	self.lock.Lock()
	defer self.lock.Unlock()
	so := self.getStateObj(addr)
	return so == nil || so.empty()
}

// Retrieve the balance from the given address or 0 if object not found
func (self *StateDB) GetBalance(addr types.Address) uint64 {
	StateObj := self.getStateObj(addr)
	if StateObj != nil {
		return StateObj.Balance().Uint64()
	}
	return 0
}

func (self *StateDB) GetNonce(addr types.Address) uint64 {
	StateObj := self.getStateObj(addr)
	if StateObj != nil {
		return StateObj.Nonce()
	}

	return 0
}

/*
 * SETTERS
 */

// AddBalance adds amount to the account associated with addr.
func (self *StateDB) AddBalance(addr types.Address, amount *big.Int) {
	stateObj := self.GetOrNewStateObj(addr)
	if stateObj != nil {
		stateObj.AddBalance(amount)
	}
}

// SubBalance subtracts amount from the account associated with addr.
func (self *StateDB) SubBalance(addr types.Address, amount *big.Int) {
	StateObj := self.GetOrNewStateObj(addr)
	if StateObj != nil {
		StateObj.SubBalance(amount)
	}
}

func (self *StateDB) SetBalance(addr types.Address, amount *big.Int) {
	stateObj := self.GetOrNewStateObj(addr)
	if stateObj != nil {
		stateObj.SetBalance(amount)
	}
}

func (self *StateDB) SetNonce(addr types.Address, nonce uint64) {
	stateObj := self.GetOrNewStateObj(addr)
	if stateObj != nil {
		stateObj.SetNonce(nonce)
	}
}

//
// Setting, updating & deleting state object methods.
//

// updateStateObj writes the given object to the trie.
func (self *StateDB) updateStateObj(StateObj *StateObj) {
	addr := StateObj.Address()
	data, err := rlp.EncodeToBytes(StateObj)
	if err != nil {
		panic(fmt.Errorf("can't encode object at %x: %v", addr[:], err))
	}
	self.setError(self.globalTrie.TryUpdate(addr[:], data))
}

// Retrieve a state object given by the types. Returns nil if not found.
func (self *StateDB) getStateObj(addr types.Address) (StateObj *StateObj) {
	self.lock.Lock()
	defer self.lock.Unlock()
	// Prefer 'live' objects.
	if obj := self.stateObjects[addr]; obj != nil {
		/*if obj.deleted {
			return nil
		}*/
		return obj
	}

	// Load the object from the database.
	enc, err := self.globalTrie.TryGet(addr[:])
	if len(enc) == 0 {
		self.setError(err)
		return nil
	}
	var data Account
	if err := rlp.DecodeBytes(enc, &data); err != nil {
		log.Error("Failed to decode state object", "addr", addr, "err", err)
		return nil
	}
	// Insert into the live set.
	obj := newObject(self, addr, data)
	self.setStateObj(obj)
	return obj
}

func (self *StateDB) makeDirtyObj(obj *StateObj) {
	self.stateObjectsDirty[obj.address] = struct{}{}
}

func (self *StateDB) setStateObj(object *StateObj) {
	self.stateObjects[object.Address()] = object
}

// Retrieve a state object or create a new state object if nil.
func (self *StateDB) GetOrNewStateObj(addr types.Address) *StateObj {
	stateObj := self.getStateObj(addr)
	if stateObj == nil { //|| StateObj.deleted {
		stateObj, _ = self.createObject(addr)
	}
	return stateObj
}

// createObject creates a new state object. If there is an existing account with
// the given address, it is overwritten and returned as the second return value.
func (self *StateDB) createObject(addr types.Address) (newobj, prev *StateObj) {
	prev = self.getStateObj(addr)
	newobj = newObject(self, addr, Account{})
	newobj.setNonce(0)
	/*if prev == nil {
		self.journal.append(createObjectChange{account: &addr})
	} else {
		self.journal.append(resetObjectChange{prev: prev})
	}*/
	self.lock.Lock()
	defer self.lock.Unlock()
	self.setStateObj(newobj)
	self.makeDirtyObj(newobj)
	return newobj, prev
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
func (self *StateDB) CreateAccount(addr types.Address) {
	new, prev := self.createObject(addr)
	if prev != nil {
		new.setBalance(prev.account.Balance)
	}
}

// Copy creates a deep, independent copy of the state.
// Snapshots of the copied state cannot be applied to the copy.
func (self *StateDB) Copy() *StateDB {
	self.lock.Lock()
	defer self.lock.Unlock()

	// Copy all the basic fields, initialize the memory ones
	state := &StateDB{
		db:                self.db,
		globalTrie:        self.db.CopyTrie(self.globalTrie),
		stateObjects:      make(map[types.Address]*StateObj),
		stateObjectsDirty: make(map[types.Address]struct{}),
	}

	for addr := range self.stateObjectsDirty {
		if _, exist := state.stateObjects[addr]; !exist {
			state.stateObjects[addr] = self.stateObjects[addr].deepCopy(state)
			state.stateObjectsDirty[addr] = struct{}{}
		}
	}

	return state
}

// Commit writes the state to the underlying in-memory trie database.
func (s *StateDB) Commit(deleteEmptyObjects bool) (root types.Hash32, err error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	// Commit objects to the trie.
	for addr, stateObject := range s.stateObjects {
		_, isDirty := s.stateObjectsDirty[addr]

		if isDirty {
			s.updateStateObj(stateObject)
		}
	}
	// Write trie changes.
	root, err = s.globalTrie.Commit(nil)
	return root, err
}

// Finalise finalises the state by removing the self destructed objects
// and clears the journal as well as the refunds.
func (s *StateDB) Finalise(deleteEmptyObjects bool) {
	//todo: do we need this?
}

// IntermediateRoot computes the current root hash of the state trie.
// It is called in between transactions to get the root hash that
// goes into transaction receipts.
func (s *StateDB) IntermediateRoot(deleteEmptyObjects bool) types.Hash32 {
	return s.globalTrie.Hash()
}

// TrieDB retrieves the low level trie database used for data storage.
func (s *StateDB) TrieDB() *trie.Database {
	return s.db.TrieDB()
}
