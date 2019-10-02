package state

import (
	"bytes"
	"container/list"
	"fmt"
	xdr "github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/common/types"
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

type GasConfig struct {
	BasicTxCost *big.Int
}

func DefaultConfig() GasConfig {
	return GasConfig{
		big.NewInt(3),
	}
}

type Projector interface {
	GetProjection(addr types.Address, prevNonce, prevBalance uint64) (nonce, balance uint64, err error)
}

type TransactionProcessor struct {
	log.Log
	globalState  *StateDB
	prevStates   map[types.LayerID]types.Hash32
	currentLayer types.LayerID
	rootHash     types.Hash32
	stateQueue   list.List
	projector    Projector
	gasCost      GasConfig
	db           *trie.Database
	mu           sync.Mutex
}

const maxPastStates = 20

func NewTransactionProcessor(db *StateDB, projector Projector, gasParams GasConfig, logger log.Log) *TransactionProcessor {
	return &TransactionProcessor{
		Log:          logger,
		globalState:  db,
		prevStates:   make(map[types.LayerID]types.Hash32),
		currentLayer: 0,
		rootHash:     types.Hash32{},
		stateQueue:   list.List{},
		projector:    projector,
		gasCost:      gasParams,
		db:           db.TrieDB(),
		mu:           sync.Mutex{}, //sync between reset and apply mesh.Transactions
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
	if !tp.globalState.Exist(addr) {
		return types.Address{}, fmt.Errorf("failed to validate tx signature, unknown src account %v", addr)
	}

	return addr, nil
}

// AddressExists checks if an account address exists in this node's global state
func (tp *TransactionProcessor) AddressExists(addr types.Address) bool {
	return tp.globalState.Exist(addr)
}

func (tp *TransactionProcessor) ValidateNonceAndBalance(tx *types.Transaction) error {
	origin := tx.Origin()
	nonce, balance, err := tp.projector.GetProjection(origin, tp.globalState.GetNonce(origin), tp.globalState.GetBalance(origin))
	if err != nil {
		return fmt.Errorf("failed to project state for account %v: %v", origin.Short(), err)
	}
	if tx.AccountNonce != nonce {
		return fmt.Errorf("incorrect account nonce! Expected: %d, Actual: %d", nonce, tx.AccountNonce)
	}
	if (tx.Amount + tx.GasPrice) > balance { // TODO: GasPrice represents the absolute fee here, as a temporarily hack
		return fmt.Errorf("insufficient balance! Available: %d, Attempting to spend: %d[amount]+%d[fee]=%d",
			balance, tx.Amount, tx.GasPrice, tx.Amount+tx.GasPrice)
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
	for i := 0; i < 100; i++ { // Capped to 100 iterations TODO: Should this be capped? Extract to config / const
		remaining = tp.Process(remaining)
		if remainingCount == len(remaining) {
			break
		}
		remainingCount = len(remaining)
	}

	newHash, err := tp.globalState.Commit(false)

	if err != nil {
		tp.Log.Error("db write error %v", err)
		return remainingCount, err
	}

	tp.Log.Info("new state root for layer %v is %x", layer, newHash)
	tp.Log.With().Info("new state", log.Uint64("mesh.LayerID", uint64(layer)), log.String("root_hash", newHash.String()))

	tp.addStateToHistory(layer, newHash)

	return remainingCount, nil
}

func (tp *TransactionProcessor) addStateToHistory(layer types.LayerID, newHash types.Hash32) {
	tp.stateQueue.PushBack(newHash)
	if tp.stateQueue.Len() > maxPastStates {
		hash := tp.stateQueue.Remove(tp.stateQueue.Back())
		tp.db.Commit(hash.(types.Hash32), false)
	}
	tp.prevStates[layer] = newHash
	tp.db.Reference(newHash, types.Hash32{})

}

func (tp *TransactionProcessor) ApplyRewards(layer types.LayerID, minersAccounts []types.Address, underQuota map[types.Address]int, bonusReward, diminishedReward *big.Int) {
	for _, account := range minersAccounts {
		reward := bonusReward
		//if we have 2 blocks in same layer, one of them can receive a diminished reward and the other cannot
		if val, ok := underQuota[account]; ok {
			if val > 0 {
				reward = diminishedReward
				underQuota[account] = underQuota[account] - 1
				if underQuota[account] == 0 {
					delete(underQuota, account)
				}
			}
		}
		tp.Log.Info("reward applied for account: %s reward: %s is diminished: %v layer: %v", account.Short(),
			reward.String(),
			reward == diminishedReward,
			layer)

		tp.globalState.AddBalance(account, reward)
		events.Publish(events.RewardReceived{Coinbase: account.String(), Amount: reward.Uint64()})
	}
	newHash, err := tp.globalState.Commit(false)

	if err != nil {
		tp.Log.Error("db write error %v", err)
		return
	}

	tp.addStateToHistory(layer, newHash)

}

func (tp *TransactionProcessor) Reset(layer types.LayerID) {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	if state, ok := tp.prevStates[layer]; ok {
		newState, err := New(state, tp.globalState.db)

		if err != nil {
			log.Panic("cannot revert- improper state")
		}
		tp.Log.Info("reverted, new root %x", newState.IntermediateRoot(false))
		tp.Log.With().Info("reverted", log.String("root_hash", newState.IntermediateRoot(false).String()))

		tp.globalState = newState
		tp.pruneAfterRevert(layer)
	}
}

func (tp *TransactionProcessor) Process(txs []*types.Transaction) (remaining []*types.Transaction) {
	for _, tx := range txs {
		err := tp.ApplyTransaction(tx)
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
			Gas:         tx.GasPrice})
	}
	return
}

func (tp *TransactionProcessor) pruneAfterRevert(targetLayerID types.LayerID) {
	//needs to be called under mutex lock
	for i := tp.currentLayer; i >= targetLayerID; i-- {
		if hash, ok := tp.prevStates[i]; ok {
			if tp.stateQueue.Front().Value != hash {
				panic("old state wasn't found")
			}
			tp.stateQueue.Remove(tp.stateQueue.Front())
			tp.db.Dereference(hash)
			delete(tp.prevStates, i)
		}
	}
}

func (tp *TransactionProcessor) checkNonce(trns *types.Transaction) bool {
	return tp.globalState.GetNonce(trns.Origin()) == trns.AccountNonce
}

var (
	ErrOrigin = "origin account doesnt exist"
	ErrFunds  = "insufficient funds"
	ErrNonce  = "incorrect nonce"
)

func (tp *TransactionProcessor) ApplyTransaction(trans *types.Transaction) error {
	if !tp.globalState.Exist(trans.Origin()) {
		return fmt.Errorf(ErrOrigin)
	}

	origin := tp.globalState.GetOrNewStateObj(trans.Origin())

	gas := trans.GasPrice * tp.gasCost.BasicTxCost.Uint64()

	/*if gas < trans.GasLimit {

	}*/

	amountWithGas := gas + trans.Amount

	//todo: should we allow to spend all accounts balance?
	if origin.Balance().Uint64() <= amountWithGas {
		tp.Log.Error(ErrFunds+" have: %v need: %v", origin.Balance(), amountWithGas)
		return fmt.Errorf(ErrFunds)
	}

	if !tp.checkNonce(trans) {
		tp.Log.Error(ErrNonce+" should be %v actual %v", tp.globalState.GetNonce(trans.Origin()), trans.AccountNonce)
		return fmt.Errorf(ErrNonce)
	}

	tp.globalState.SetNonce(trans.Origin(), tp.globalState.GetNonce(trans.Origin())+1) // TODO: Not thread-safe
	transfer(tp.globalState, trans.Origin(), trans.Recipient, new(big.Int).SetUint64(trans.Amount))

	//subtract gas from account, gas will be sent to miners in layers after
	tp.globalState.SubBalance(trans.Origin(), new(big.Int).SetUint64(gas))
	tp.With().Info("transaction processed", log.String("transaction", trans.String()))
	return nil
}

func transfer(db GlobalStateDB, sender, recipient types.Address, amount *big.Int) {
	db.SubBalance(sender, amount)
	db.AddBalance(recipient, amount)
}
