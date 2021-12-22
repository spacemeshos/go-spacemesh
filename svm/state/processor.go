package state

import (
	"container/list"
	"fmt"
	"sync"

	"github.com/spacemeshos/ed25519"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mempool"
	"github.com/spacemeshos/go-spacemesh/trie"
)

// IncomingTxProtocol is the protocol identifier for tx received by gossip that is used by the p2p.
const IncomingTxProtocol = "TxGossip"

// PreImages is a struct that contains a root hash and the transactions that are in store of this root hash.
type PreImages struct {
	rootHash  types.Hash32
	preImages []*types.Transaction
}

// Projector interface defines the interface for a struct that can project the state of an account by applying txs from
// mem pool.
type Projector interface {
	GetProjection(addr types.Address, prevNonce, prevBalance uint64) (nonce, balance uint64, err error)
}

// TransactionProcessor is the struct containing state db and is responsible for applying transactions into it.
type TransactionProcessor struct {
	log.Log
	*DB
	pool         *mempool.TxMempool
	processorDb  database.Database
	currentLayer types.LayerID
	rootHash     types.Hash32
	stateQueue   list.List
	projector    Projector
	trie         *trie.Database
	mu           sync.Mutex
	rootMu       sync.RWMutex
}

const newRootKey = "root"

// NewTransactionProcessor returns a new state processor.
func NewTransactionProcessor(allStates, processorDb database.Database, projector Projector, txPool *mempool.TxMempool, logger log.Log) *TransactionProcessor {
	stateDb, err := New(types.Hash32{}, NewDatabase(allStates))
	if err != nil {
		log.With().Panic("cannot load state db", log.Err(err))
	}
	root := stateDb.IntermediateRoot(false)
	logger.With().Info("started processor", log.FieldNamed("state_root", root))
	return &TransactionProcessor{
		Log:         logger,
		DB:          stateDb,
		processorDb: processorDb,
		rootHash:    root,
		stateQueue:  list.List{},
		projector:   projector,
		trie:        stateDb.TrieDB(),
		pool:        txPool,
		mu:          sync.Mutex{}, // sync between reset and apply mesh.Transactions
		rootMu:      sync.RWMutex{},
	}
}

// PublicKeyToAccountAddress converts ed25519 public key to account address.
func PublicKeyToAccountAddress(pub ed25519.PublicKey) types.Address {
	var addr types.Address
	addr.SetBytes(pub)
	return addr
}

// AddressExists checks if an account address exists in this node's global state.
func (tp *TransactionProcessor) AddressExists(addr types.Address) bool {
	return tp.Exist(addr)
}

// GetLayerApplied gets the layer id at which this tx was applied.
func (tp *TransactionProcessor) GetLayerApplied(txID types.TransactionID) *types.LayerID {
	layerIDBytes, err := tp.processorDb.Get(txID.Bytes())
	if err != nil {
		return nil
	}
	layerID := types.BytesToLayerID(layerIDBytes)
	return &layerID
}

// ValidateNonceAndBalance validates that the tx origin account has enough balance to apply the tx,
// also, it checks that nonce in tx is correct, returns error otherwise.
func (tp *TransactionProcessor) ValidateNonceAndBalance(tx *types.Transaction) error {
	origin := tx.Origin()
	nonce, balance, err := tp.projector.GetProjection(origin, tp.GetNonce(origin), tp.GetBalance(origin))
	if err != nil {
		return fmt.Errorf("failed to project state for account %v: %v", origin.Short(), err)
	}
	if tx.AccountNonce != nonce {
		return fmt.Errorf("incorrect account nonce! Expected: %d, Actual: %d", nonce, tx.AccountNonce)
	}
	if (tx.Amount + tx.GetFee()) > balance { // TODO: Fee represents the absolute fee here, as a temporarily hack
		return fmt.Errorf("insufficient balance! Available: %d, Attempting to spend: %d[amount]+%d[fee]=%d",
			balance, tx.Amount, tx.GetFee(), tx.Amount+tx.GetFee())
	}
	return nil
}

// ApplyTransactions receives a batch of transactions to apply to state. Returns the number of transactions that we
// failed to apply.
func (tp *TransactionProcessor) ApplyTransactions(layer types.LayerID, txs []*types.Transaction) ([]*types.Transaction, error) {
	if len(txs) == 0 {
		err := tp.addStateToHistory(layer, tp.GetStateRoot())
		return make([]*types.Transaction, 0), err
	}

	tp.mu.Lock()
	defer tp.mu.Unlock()
	remaining := txs
	remainingCount := len(remaining)

	// loop over the transactions until there's nothing left to process
	for {
		tp.With().Debug("applying transactions", log.Int("count_remaining", remainingCount))
		remaining = tp.Process(remaining, layer)
		if remainingCount == len(remaining) {
			break
		}
		remainingCount = len(remaining)
	}

	newHash, err := tp.Commit()
	if err != nil {
		return remaining, fmt.Errorf("failed to commit global state: %w", err)
	}

	if err = tp.addStateToHistory(layer, newHash); err != nil {
		return remaining, fmt.Errorf("add state to history: %w", err)
	}

	return remaining, nil
}

func (tp *TransactionProcessor) addStateToHistory(layer types.LayerID, newHash types.Hash32) error {
	tp.trie.Reference(newHash, types.Hash32{})
	err := tp.trie.Commit(newHash, false)
	if err != nil {
		return fmt.Errorf("commit trie: %w", err)
	}
	err = tp.saveStateRoot(newHash, layer)
	if err != nil {
		return fmt.Errorf("save state root: %w", err)
	}
	tp.Log.With().Info("new state root", layer, log.FieldNamed("state_root", newHash))
	return nil
}

func getStateRootLayerKey(layer types.LayerID) []byte {
	return append([]byte(newRootKey), layer.Bytes()...)
}

func (tp *TransactionProcessor) saveStateRoot(stateRoot types.Hash32, layer types.LayerID) error {
	if err := tp.processorDb.Put(getStateRootLayerKey(layer), stateRoot.Bytes()); err != nil {
		return fmt.Errorf("put into DB: %w", err)
	}
	tp.rootMu.Lock()
	tp.rootHash = stateRoot
	tp.rootMu.Unlock()
	return nil
}

// GetLayerStateRoot returns the state root at a given layer.
func (tp *TransactionProcessor) GetLayerStateRoot(layer types.LayerID) (types.Hash32, error) {
	bts, err := tp.processorDb.Get(getStateRootLayerKey(layer))
	if err != nil {
		return types.Hash32{}, fmt.Errorf("get from DB: %w", err)
	}
	var x types.Hash32
	x.SetBytes(bts)
	return x, nil
}

// ApplyRewards applies reward reward to miners vector for layer.
func (tp *TransactionProcessor) ApplyRewards(layer types.LayerID, rewards map[types.Address]uint64) {
	for account, reward := range rewards {
		tp.Log.With().Debug("reward applied",
			log.String("account", account.Short()),
			log.Uint64("reward", reward),
			layer,
		)
		tp.AddBalance(account, reward)
		events.ReportAccountUpdate(account)
	}

	newHash, err := tp.Commit()
	if err != nil {
		tp.With().Error("trie write error", log.Err(err))
		return
	}

	if err = tp.addStateToHistory(layer, newHash); err != nil {
		tp.With().Error("failed to add state to history", log.Err(err))
	}
}

// LoadState loads the last state from persistent storage.
func (tp *TransactionProcessor) LoadState(layer types.LayerID) error {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	state, err := tp.GetLayerStateRoot(layer)
	if err != nil {
		return err
	}
	newState, err := New(state, tp.db)
	if err != nil {
		log.With().Panic("cannot revert, improper state", log.Err(err))
	}

	tp.Log.With().Info("reverted",
		log.String("root_hash", newState.IntermediateRoot(false).String()))

	tp.DB = newState
	tp.rootMu.Lock()
	tp.rootHash = state
	tp.rootMu.Unlock()

	return nil
}

// Process applies transaction vector to current state, it returns the remaining transactions that failed.
func (tp *TransactionProcessor) Process(txs []*types.Transaction, layerID types.LayerID) (remaining []*types.Transaction) {
	for _, tx := range txs {
		err := tp.ApplyTransaction(tx, layerID)
		if err != nil {
			tp.With().Warning("failed to apply transaction", tx.ID(), log.Err(err))
			remaining = append(remaining, tx)
		}
		events.ReportValidTx(tx, err == nil)
		events.ReportNewTx(layerID, tx)
		events.ReportAccountUpdate(tx.Origin())
		events.ReportAccountUpdate(tx.GetRecipient())
	}
	return
}

func (tp *TransactionProcessor) checkNonce(trns *types.Transaction) bool {
	return tp.GetNonce(trns.Origin()) == trns.AccountNonce
}

var (
	errOrigin = "origin account doesnt exist"
	errFunds  = "insufficient funds"
	errNonce  = "incorrect nonce"
)

// ApplyTransaction applies provided transaction to the current state, but does not commit it to persistent
// storage. It returns an error if the transaction is invalid, i.e., if there is not enough balance in the source
// account to perform the transaction and pay the fee or if the nonce is incorrect.
func (tp *TransactionProcessor) ApplyTransaction(tx *types.Transaction, layerID types.LayerID) error {
	if !tp.Exist(tx.Origin()) {
		return fmt.Errorf(errOrigin)
	}

	origin := tp.GetOrNewStateObj(tx.Origin())

	amountWithFee := tx.GetFee() + tx.Amount

	// todo: should we allow to spend all accounts balance?
	if origin.Balance() <= amountWithFee {
		tp.Log.With().Error(errFunds,
			log.Uint64("balance_have", origin.Balance()),
			log.Uint64("balance_need", amountWithFee))
		return fmt.Errorf(errFunds)
	}

	if !tp.checkNonce(tx) {
		tp.Log.With().Warning(errNonce,
			log.Uint64("nonce_correct", tp.GetNonce(tx.Origin())),
			log.Uint64("nonce_actual", tx.AccountNonce))
		return fmt.Errorf(errNonce)
	}

	tp.SetNonce(tx.Origin(), tp.GetNonce(tx.Origin())+1) // TODO: Not thread-safe
	transfer(tp, tx.Origin(), tx.GetRecipient(), tx.Amount)

	// subtract fee from account, fee will be sent to miners in layers after
	tp.SubBalance(tx.Origin(), tx.GetFee())
	if err := tp.processorDb.Put(tx.ID().Bytes(), layerID.Bytes()); err != nil {
		return fmt.Errorf("failed to add to applied txs: %v", err)
	}
	tp.With().Info("transaction processed", log.String("transaction", tx.String()))
	return nil
}

// GetStateRoot gets the current state root hash.
func (tp *TransactionProcessor) GetStateRoot() types.Hash32 {
	tp.rootMu.RLock()
	defer tp.rootMu.RUnlock()
	return tp.rootHash
}

func transfer(db *TransactionProcessor, sender, recipient types.Address, amount uint64) {
	db.SubBalance(sender, amount)
	db.AddBalance(recipient, amount)
}

// AddTxToPool exports mempool functionality for adding tx to the pool.
func (tp *TransactionProcessor) AddTxToPool(tx *types.Transaction) {
	tp.pool.Put(tx.ID(), tx)
}
