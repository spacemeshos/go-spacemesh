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
	"sort"
	"sync"
)

type PseudoRandomizer interface {
	Uint32() uint32
	Uint64() uint64
}

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
	rand         PseudoRandomizer
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

func NewTransactionProcessor(rnd PseudoRandomizer, db *StateDB, projector Projector, gasParams GasConfig, logger log.Log) *TransactionProcessor {
	return &TransactionProcessor{
		Log:          logger,
		rand:         rnd,
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
func (tp *TransactionProcessor) ValidateSignature(s types.Signed) (types.Address, error) {
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

// Validate the tx's signature by extracting the source account and validating its existence.
// Return the src acount address and error in case of failure
func (tp *TransactionProcessor) ValidateTransactionSignature(tx *types.Transaction) (types.Address, error) {
	addr := tx.Origin()
	if !tp.globalState.Exist(addr) {
		return types.Address{}, fmt.Errorf("failed to validate tx signature, unknown src account %x", addr)
	}

	return addr, nil
}

func (tp *TransactionProcessor) ValidateNonceAndBalance(tx *types.Transaction) error {
	stateObj := &StateObj{}
	if tp.globalState.Exist(tx.Origin()) {
		stateObj = tp.globalState.GetOrNewStateObj(tx.Origin())
	}
	nonce, balance, err := tp.projector.GetProjection(stateObj.Address(), stateObj.Nonce(), stateObj.Balance().Uint64())
	if err != nil {
		return fmt.Errorf("failed to project state for account %v: %v", tx.Origin().Short(), err)
	}
	if tx.AccountNonce != nonce {
		return fmt.Errorf("incorrect account nonce! Expected: %d, Actual: %d", nonce, tx.AccountNonce)
	}
	if (tx.Amount + tx.GasPrice) > balance { // TODO: GasPrice represents the absolute fee here, as a temporarily hack
		return fmt.Errorf("insufficient balance! Available: %d, Attempting to spend: %d+%d=%d",
			balance, tx.Amount, tx.GasPrice, tx.Amount+tx.GasPrice)
	}
	return nil
}

//should receive sort predicate
// ApplyTransaction receives a batch of transaction to apply on state. Returns the number of transaction that failed to apply.
func (tp *TransactionProcessor) ApplyTransactions(layer types.LayerID, txs []*types.Transaction) (uint32, error) {
	//todo: need to seed the mersenne twister with random beacon seed
	if len(txs) == 0 {
		return 0, nil
	}

	//txs := MergeDoubles(mesh.Transactions)
	tp.mu.Lock()
	defer tp.mu.Unlock()
	failed := tp.Process(tp.randomSort(txs), tp.coalescTransactionsBySender(txs))
	newHash, err := tp.globalState.Commit(false)

	if err != nil {
		tp.Log.Error("db write error %v", err)
		return failed, err
	}

	tp.Log.Info("new state root for layer %v is %x", layer, newHash)
	tp.Log.With().Info("new state", log.Uint64("mesh.LayerID", uint64(layer)), log.String("root_hash", newHash.String()))

	tp.addStateToHistory(layer, newHash)

	return failed, nil
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

func (tp *TransactionProcessor) randomSort(transactions []*types.Transaction) []*types.Transaction {
	vecLen := len(transactions)
	for i := range transactions {
		swp := int(tp.rand.Uint32()) % vecLen
		transactions[i], transactions[swp] = transactions[swp], transactions[i]
	}
	return transactions
}

func (tp *TransactionProcessor) coalescTransactionsBySender(transactions []*types.Transaction) map[types.Address][]*types.Transaction {
	trnsBySender := make(map[types.Address][]*types.Transaction)
	for _, trns := range transactions {
		trnsBySender[trns.Origin()] = append(trnsBySender[trns.Origin()], trns)
	}

	for key := range trnsBySender {
		sort.Slice(trnsBySender[key], func(i, j int) bool {
			//todo: add fix here:
			if trnsBySender[key][i].AccountNonce == trnsBySender[key][j].AccountNonce {
				return bytes.Compare(trnsBySender[key][i].Id().Bytes(), trnsBySender[key][j].Id().Bytes()) > 1
			}
			return trnsBySender[key][i].AccountNonce < trnsBySender[key][j].AccountNonce
		})
	}

	return trnsBySender
}

func (tp *TransactionProcessor) Process(transactions []*types.Transaction, trnsBySender map[types.Address][]*types.Transaction) (errors uint32) {
	senderPut := make(map[types.Address]struct{})
	sortedOriginByTransactions := make([]types.Address, 0, 10)
	errors = 0
	// The order of the mesh.Transactions determines the order addresses by which we take mesh.Transactions
	// Maybe refactor this
	for _, trans := range transactions {
		if _, ok := senderPut[trans.Origin()]; !ok {
			sortedOriginByTransactions = append(sortedOriginByTransactions, trans.Origin())
			senderPut[trans.Origin()] = struct{}{}
		}
	}

	for _, origin := range sortedOriginByTransactions {
		for _, trns := range trnsBySender[origin] {
			//todo: should we abort all transaction processing if we failed this one?
			err := tp.ApplyTransaction(trns)
			//todo: think maybe moving these to another validation process before palying transactions.
			events.Publish(events.NewTx{Id: trns.Id().String(),
				Origin:      trns.Origin().String(),
				Destination: trns.Recipient.String(),
				Amount:      trns.Amount,
				Gas:         trns.GasPrice})
			if err != nil {
				errors++
				tp.Log.Error("transaction aborted: %v", err)
			}
			events.Publish(events.ValidTx{Id: trns.Id().String(), Valid: err == nil})
		}
	}
	return errors
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
	tp.Log.Info("transaction processed, s_account: %s d_account: %s, amount: %v shmekels tx nonce: %v, gas limit: %v gas price: %v",
		trans.Origin().Short(), trans.Recipient.Short(), trans.Amount, trans.AccountNonce, trans.GasLimit, trans.GasPrice)
	return nil
}

func transfer(db GlobalStateDB, sender, recipient types.Address, amount *big.Int) {
	db.SubBalance(sender, amount)
	db.AddBalance(recipient, amount)
}
