package state

import (
	"container/list"
	"context"
	"fmt"
	"math/big"
	"sync"

	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/trie"
)

// IncomingTxProtocol is the protocol identifier for tx received by gossip that is used by the p2p
const IncomingTxProtocol = "TxGossip"

// PreImages is a struct that contains a root hash and the transactions that are in store of this root hash
type PreImages struct {
	rootHash  types.Hash32
	preImages []*types.Transaction
}

// Projector interface defines the interface for a struct that can project the state of an account by applying txs from
// mem pool
type Projector interface {
	GetProjection(addr types.Address, prevNonce, prevBalance uint64) (nonce, balance uint64, err error)
}

// TransactionProcessor is the struct containing state db and is responsible for applying transactions into it
type TransactionProcessor struct {
	log.Log
	*DB
	pool         *TxMempool
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

// NewTransactionProcessor returns a new state processor
func NewTransactionProcessor(allStates, processorDb database.Database, projector Projector, txPool *TxMempool, logger log.Log) *TransactionProcessor {
	stateDb, err := New(types.Hash32{}, NewDatabase(allStates))
	if err != nil {
		log.With().Panic("cannot load state db", log.Err(err))
	}
	root := stateDb.IntermediateRoot(false)
	log.With().Info("started processor", log.FieldNamed("state_root", root))
	return &TransactionProcessor{
		Log:          logger,
		DB:           stateDb,
		processorDb:  processorDb,
		currentLayer: 0,
		rootHash:     root,
		stateQueue:   list.List{},
		projector:    projector,
		trie:         stateDb.TrieDB(),
		pool:         txPool,
		mu:           sync.Mutex{}, // sync between reset and apply mesh.Transactions
		rootMu:       sync.RWMutex{},
	}
}

// PublicKeyToAccountAddress converts ed25519 public key to account address
func PublicKeyToAccountAddress(pub ed25519.PublicKey) types.Address {
	var addr types.Address
	addr.SetBytes(pub)
	return addr
}

// AddressExists checks if an account address exists in this node's global state
func (tp *TransactionProcessor) AddressExists(addr types.Address) bool {
	return tp.Exist(addr)
}

// GetLayerApplied gets the layer id at which this tx was applied
func (tp *TransactionProcessor) GetLayerApplied(txID types.TransactionID) *types.LayerID {
	layerIDBytes, err := tp.processorDb.Get(txID.Bytes())
	if err != nil {
		return nil
	}
	layerID := types.LayerID(util.BytesToUint64(layerIDBytes))
	return &layerID
}

// ValidateNonceAndBalance validates that the tx origin account has enough balance to apply the tx,
// also, it checks that nonce in tx is correct, returns error otherwise
func (tp *TransactionProcessor) ValidateNonceAndBalance(tx *types.Transaction) error {
	origin := tx.Origin()
	nonce, balance, err := tp.projector.GetProjection(origin, tp.GetNonce(origin), tp.GetBalance(origin))
	if err != nil {
		return fmt.Errorf("failed to project state for account %v: %v", origin.Short(), err)
	}
	if tx.AccountNonce != nonce {
		return fmt.Errorf("incorrect account nonce! Expected: %d, Actual: %d", nonce, tx.AccountNonce)
	}
	if (tx.Amount + tx.Fee) > balance { // TODO: Fee represents the absolute fee here, as a temporarily hack
		return fmt.Errorf("insufficient balance! Available: %d, Attempting to spend: %d[amount]+%d[fee]=%d",
			balance, tx.Amount, tx.Fee, tx.Amount+tx.Fee)
	}
	return nil
}

// ApplyTransactions receives a batch of transaction to apply on state. Returns the number of transaction that failed to apply.
func (tp *TransactionProcessor) ApplyTransactions(layer types.LayerID, txs []*types.Transaction) (int, error) {
	if len(txs) == 0 {
		err := tp.addStateToHistory(layer, tp.GetStateRoot())
		return 0, err
	}

	tp.mu.Lock()
	defer tp.mu.Unlock()
	remaining := txs
	remainingCount := len(remaining)
	for { // Loop until there's nothing left to process
		remaining = tp.Process(remaining, layer)
		if remainingCount == len(remaining) {
			break
		}
		remainingCount = len(remaining)
	}

	newHash, err := tp.Commit()

	if err != nil {
		return remainingCount, fmt.Errorf("failed to commit global state: %v", err)
	}

	err = tp.addStateToHistory(layer, newHash)

	return remainingCount, err
}

func (tp *TransactionProcessor) addStateToHistory(layer types.LayerID, newHash types.Hash32) error {
	tp.trie.Reference(newHash, types.Hash32{})
	err := tp.trie.Commit(newHash, false)
	if err != nil {
		return err
	}
	err = tp.addState(newHash, layer)
	if err != nil {
		return err
	}
	tp.Log.With().Info("new state root", layer, log.FieldNamed("state_root", newHash))
	return nil
}

func getStateRootLayerKey(layer types.LayerID) []byte {
	return append([]byte(newRootKey), layer.Bytes()...)
}

func (tp *TransactionProcessor) addState(stateRoot types.Hash32, layer types.LayerID) error {
	if err := tp.processorDb.Put(getStateRootLayerKey(layer), stateRoot.Bytes()); err != nil {
		return err
	}
	tp.rootMu.Lock()
	tp.rootHash = stateRoot
	tp.rootMu.Unlock()
	return nil
}

// GetLayerStateRoot returns the state root at a given layer
func (tp *TransactionProcessor) GetLayerStateRoot(layer types.LayerID) (types.Hash32, error) {
	bts, err := tp.processorDb.Get(getStateRootLayerKey(layer))
	if err != nil {
		return types.Hash32{}, err
	}
	var x types.Hash32
	x.SetBytes(bts)
	return x, nil
}

// ApplyRewards applies reward reward to miners vector for layer
// TODO: convert rewards to uint64 (see https://github.com/spacemeshos/go-spacemesh/issues/2069)
func (tp *TransactionProcessor) ApplyRewards(layer types.LayerID, miners []types.Address, reward *big.Int) {
	rewardConverted := reward.Uint64()
	for _, account := range miners {
		tp.Log.With().Info("reward applied",
			log.String("account", account.Short()),
			log.Uint64("reward", rewardConverted),
			layer,
		)
		tp.AddBalance(account, rewardConverted)
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

// LoadState loads the last state from persistent storage
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
		log.FieldNamed("root_hash", newState.IntermediateRoot(false)))

	tp.DB = newState
	tp.rootMu.Lock()
	tp.rootHash = state
	tp.rootMu.Unlock()

	return nil
}

// Process applies transaction vector to current state, it returns the remaining transactions that failed
func (tp *TransactionProcessor) Process(txs []*types.Transaction, layerID types.LayerID) (remaining []*types.Transaction) {
	for _, tx := range txs {
		err := tp.ApplyTransaction(tx, layerID)
		if err != nil {
			tp.With().Warning("failed to apply transaction", tx.ID(), log.Err(err))
			remaining = append(remaining, tx)
		}
		events.ReportValidTx(tx, err == nil)
		events.ReportNewTx(tx)
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

// ApplyTransaction applies provided transaction trans to the current state, but does not commit it to persistent
// storage. it returns error if there is not enough balance in src account to perform the transaction and pay
// fee or if the nonce is invalid
func (tp *TransactionProcessor) ApplyTransaction(trans *types.Transaction, layerID types.LayerID) error {
	if !tp.Exist(trans.Origin()) {
		return fmt.Errorf(errOrigin)
	}

	origin := tp.GetOrNewStateObj(trans.Origin())

	amountWithFee := trans.Fee + trans.Amount

	// todo: should we allow to spend all accounts balance?
	if origin.Balance() <= amountWithFee {
		tp.Log.With().Error(errFunds,
			log.Uint64("balance_have", origin.Balance()),
			log.Uint64("balance_need", amountWithFee))
		return fmt.Errorf(errFunds)
	}

	if !tp.checkNonce(trans) {
		tp.Log.With().Error(errNonce,
			log.Uint64("nonce_correct", tp.GetNonce(trans.Origin())),
			log.Uint64("nonce_actual", trans.AccountNonce))
		return fmt.Errorf(errNonce)
	}

	tp.SetNonce(trans.Origin(), tp.GetNonce(trans.Origin())+1) // TODO: Not thread-safe
	transfer(tp, trans.Origin(), trans.Recipient, trans.Amount)

	// subtract fee from account, fee will be sent to miners in layers after
	tp.SubBalance(trans.Origin(), trans.Fee)
	if err := tp.processorDb.Put(trans.ID().Bytes(), layerID.Bytes()); err != nil {
		return fmt.Errorf("failed to add to applied txs: %v", err)
	}
	tp.With().Info("transaction processed", log.String("transaction", trans.String()))
	return nil
}

// GetStateRoot gets the current state root hash
func (tp *TransactionProcessor) GetStateRoot() types.Hash32 {
	tp.rootMu.RLock()
	defer tp.rootMu.RUnlock()
	return tp.rootHash
}

func transfer(db *TransactionProcessor, sender, recipient types.Address, amount uint64) {
	db.SubBalance(sender, amount)
	db.AddBalance(recipient, amount)
}

// HandleTxGossipData handles data sent from gossip
func (tp *TransactionProcessor) HandleTxGossipData(ctx context.Context, data service.GossipMessage, syncer service.Fetcher) {
	err := tp.HandleTxData(data.Bytes())
	if err != nil {
		tp.With().Error("invalid tx", log.Err(err))
		return
	}
	data.ReportValidation(ctx, IncomingTxProtocol)
}

// HandleTxData handles data received on TX gossip channel
func (tp *TransactionProcessor) HandleTxData(data []byte) error {
	tx, err := types.BytesToTransaction(data)
	if err != nil {
		tp.With().Error("cannot parse incoming transaction", log.Err(err))
		return err
	}
	return tp.handleTransaction(tx)
}

// HandleTxSyncData handles data received on TX sync
func (tp *TransactionProcessor) HandleTxSyncData(data []byte) error {
	var tx mesh.DbTransaction
	err := types.BytesToInterface(data, &tx)
	if err != nil {
		tp.With().Error("cannot parse incoming transaction", log.Err(err))
		return err
	}
	if err = tx.CalcAndSetOrigin(); err != nil {
		return err
	}
	// we don't validate the tx, todo: this is copied from old sync, unless I am wrong i think some validation is needed
	tp.pool.Put(tx.Transaction.ID(), tx.Transaction)
	return nil
}

func (tp *TransactionProcessor) handleTransaction(tx *types.Transaction) error {
	if err := tx.CalcAndSetOrigin(); err != nil {
		tp.With().Error("failed to calculate transaction origin", tx.ID(), log.Err(err))
		return err
	}

	tp.Log.With().Info("got new tx",
		tx.ID(),
		log.Uint64("nonce", tx.AccountNonce),
		log.Uint64("amount", tx.Amount),
		log.Uint64("fee", tx.Fee),
		log.Uint64("gas", tx.GasLimit),
		log.String("recipient", tx.Recipient.String()),
		log.String("origin", tx.Origin().String()))

	if !tp.AddressExists(tx.Origin()) {
		tp.With().Error("transaction origin does not exist",
			log.String("transaction", tx.String()),
			tx.ID(),
			log.String("origin", tx.Origin().Short()))
		return fmt.Errorf("transaction origin does not exist")
	}
	if err := tp.ValidateNonceAndBalance(tx); err != nil {
		tp.With().Error("nonce and balance validation failed", tx.ID(), log.Err(err))
		return fmt.Errorf("nonce and balance validation failed")
	}

	tp.pool.Put(tx.ID(), tx)
	return nil
}

// ValidateAndAddTxToPool validates the provided tx nonce and balance with projector and puts it in the transaction pool
// it returns an error if the provided tx is not valid
func (tp *TransactionProcessor) ValidateAndAddTxToPool(tx *types.Transaction) error {
	err := tp.ValidateNonceAndBalance(tx)
	if err != nil {
		return err
	}
	tp.pool.Put(tx.ID(), tx)
	return nil
}
