package state

import (
	"bytes"
	"container/list"
	"fmt"
	xdr "github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/trie"
	"math/big"
	"sync"
)

type StatePreImages struct {
	rootHash  types.Hash32
	preImages []*types.Transaction
}

type Projector interface {
	GetProjection(addr types.Address, prevNonce, prevBalance uint64) (nonce, balance uint64, err error)
}

type TransactionProcessor struct {
	log.Log
	*StateDB
	processorDb  database.Database
	currentLayer types.LayerID
	rootHash     types.Hash32
	stateQueue   list.List
	projector    Projector
	trie         *trie.Database
	mu           sync.Mutex
	rootMu       sync.RWMutex
}

const NewRootKey = "root"

func NewTransactionProcessor(allStates, processorDb database.Database, projector Projector, logger log.Log) *TransactionProcessor {
	stateDb, err := New(types.Hash32{}, NewDatabase(allStates))
	if err != nil {
		log.Panic("cannot load state db, %v", err)
	}
	root := stateDb.IntermediateRoot(false)
	log.Info("started processor with state root %v", root)
	return &TransactionProcessor{
		Log:          logger,
		StateDB:      stateDb,
		processorDb:  processorDb,
		currentLayer: 0,
		rootHash:     root,
		stateQueue:   list.List{},
		projector:    projector,
		trie:         stateDb.TrieDB(),
		mu:           sync.Mutex{}, //sync between reset and apply mesh.Transactions
		rootMu:       sync.RWMutex{},
	}
}

func PublicKeyToAccountAddress(pub ed25519.PublicKey) types.Address {
	var addr types.Address
	addr.SetBytes(pub)
	return addr
}

// Validate the signature by extracting the source account and validating its existence.
// Return the src acount address and error in case of failure
func (tp *TransactionProcessor) ValidateSignature(s types.Signed) (types.Address, error) { // TODO: never used
	var w bytes.Buffer
	_, err := xdr.Marshal(&w, s.Data())
	if err != nil {
		return types.Address{}, err
	}

	pubKey, err := ed25519.ExtractPublicKey(w.Bytes(), s.Sig())
	if err != nil {
		return types.Address{}, err
	}

	addr := PublicKeyToAccountAddress(pubKey)
	if !tp.Exist(addr) {
		return types.Address{}, fmt.Errorf("failed to validate tx signature, unknown src account %v", addr)
	}

	return addr, nil
}

// AddressExists checks if an account address exists in this node's global state
func (tp *TransactionProcessor) AddressExists(addr types.Address) bool {
	return tp.Exist(addr)
}

func (tp *TransactionProcessor) GetLayerApplied(txId types.TransactionId) *types.LayerID {
	layerIdBytes, err := tp.processorDb.Get(txId.Bytes())
	if err != nil {
		return nil
	}
	layerId := types.LayerID(util.BytesToUint64(layerIdBytes))
	return &layerId
}

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

// ApplyTransaction receives a batch of transaction to apply on state. Returns the number of transaction that failed to apply.
func (tp *TransactionProcessor) ApplyTransactions(layer types.LayerID, txs []*types.Transaction) (int, error) {
	if len(txs) == 0 {
		return 0, nil
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

	newHash, err := tp.Commit(false)

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
	tp.Log.With().Info("new state root", log.LayerId(uint64(layer)), log.String("state_root", newHash.String()))
	return nil
}

func getStateRootLayerKey(layer types.LayerID) []byte {
	return append([]byte(NewRootKey), layer.ToBytes()...)
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

func (tp *TransactionProcessor) getLayerStateRoot(layer types.LayerID) (types.Hash32, error) {
	bts, err := tp.processorDb.Get(getStateRootLayerKey(layer))
	if err != nil {
		return types.Hash32{}, err
	}
	var x types.Hash32
	x.SetBytes(bts)
	return x, nil
}

func (tp *TransactionProcessor) ApplyRewards(layer types.LayerID, miners []types.Address, reward *big.Int) {
	for _, account := range miners {
		tp.Log.With().Info("Reward applied",
			log.String("account", account.Short()),
			log.Uint64("reward", reward.Uint64()),
			log.LayerId(uint64(layer)),
		)
		tp.AddBalance(account, reward)
		events.Publish(events.RewardReceived{Coinbase: account.String(), Amount: reward.Uint64()})
	}
	newHash, err := tp.Commit(false)

	if err != nil {
		tp.Log.Error("trie write error %v", err)
		return
	}

	err = tp.addStateToHistory(layer, newHash)
	if err != nil {
		tp.Log.Error("failed to add state to history: %v", err)
	}
}

func (tp *TransactionProcessor) LoadState(layer types.LayerID) error {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	state, err := tp.getLayerStateRoot(layer)
	if err != nil {
		return err
	}
	newState, err := New(state, tp.db)
	if err != nil {
		log.Panic("cannot revert- improper state: %v", err)
	}

	tp.Log.Info("reverted, new root %x", newState.IntermediateRoot(false))
	tp.Log.With().Info("reverted", log.String("root_hash", newState.IntermediateRoot(false).String()))

	tp.StateDB = newState
	tp.rootMu.Lock()
	tp.rootHash = state
	tp.rootMu.Unlock()

	return nil
}

func (tp *TransactionProcessor) Process(txs []*types.Transaction, layerId types.LayerID) (remaining []*types.Transaction) {
	for _, tx := range txs {
		err := tp.ApplyTransaction(tx, layerId)
		if err != nil {
			tp.With().Warning("failed to apply transaction", log.TxId(tx.Id().ShortString()), log.Err(err))
			remaining = append(remaining, tx)
		}
		events.Publish(events.ValidTx{Id: tx.Id().String(), Valid: err == nil})
		events.Publish(events.NewTx{
			Id:          tx.Id().String(),
			Origin:      tx.Origin().String(),
			Destination: tx.Recipient.String(),
			Amount:      tx.Amount,
			Fee:         tx.Fee})
	}
	return
}

func (tp *TransactionProcessor) checkNonce(trns *types.Transaction) bool {
	return tp.GetNonce(trns.Origin()) == trns.AccountNonce
}

var (
	ErrOrigin = "origin account doesnt exist"
	ErrFunds  = "insufficient funds"
	ErrNonce  = "incorrect nonce"
)

func (tp *TransactionProcessor) ApplyTransaction(trans *types.Transaction, layerId types.LayerID) error {
	if !tp.Exist(trans.Origin()) {
		return fmt.Errorf(ErrOrigin)
	}

	origin := tp.GetOrNewStateObj(trans.Origin())

	amountWithFee := trans.Fee + trans.Amount

	//todo: should we allow to spend all accounts balance?
	if origin.Balance().Uint64() <= amountWithFee {
		tp.Log.Error(ErrFunds+" have: %v need: %v", origin.Balance(), amountWithFee)
		return fmt.Errorf(ErrFunds)
	}

	if !tp.checkNonce(trans) {
		tp.Log.Error(ErrNonce+" should be %v actual %v", tp.GetNonce(trans.Origin()), trans.AccountNonce)
		return fmt.Errorf(ErrNonce)
	}

	tp.SetNonce(trans.Origin(), tp.GetNonce(trans.Origin())+1) // TODO: Not thread-safe
	transfer(tp, trans.Origin(), trans.Recipient, new(big.Int).SetUint64(trans.Amount))

	//subtract fee from account, fee will be sent to miners in layers after
	tp.SubBalance(trans.Origin(), new(big.Int).SetUint64(trans.Fee))
	if err := tp.processorDb.Put(trans.Id().Bytes(), layerId.ToBytes()); err != nil {
		return fmt.Errorf("failed to add to applied txs: %v", err)
	}
	tp.With().Info("transaction processed", log.String("transaction", trans.String()))
	return nil
}

func (tp *TransactionProcessor) GetStateRoot() types.Hash32 {
	tp.rootMu.RLock()
	defer tp.rootMu.RUnlock()
	return tp.rootHash
}

func transfer(db GlobalStateDB, sender, recipient types.Address, amount *big.Int) {
	db.SubBalance(sender, amount)
	db.AddBalance(recipient, amount)
}
