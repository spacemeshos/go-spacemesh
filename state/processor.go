package state

import (
	"container/list"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
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
	rootHash  common.Hash
	preImages mesh.Transactions
}

type GasConfig struct {
	BasicTxCost *big.Int
}

func DefaultConfig() GasConfig {
	return GasConfig{
		big.NewInt(3),
	}
}

type TransactionProcessor struct {
	log.Log
	rand         PseudoRandomizer
	globalState  *StateDB
	prevStates   map[mesh.LayerID]common.Hash
	currentLayer mesh.LayerID
	rootHash     common.Hash
	stateQueue   list.List
	gasCost      GasConfig
	db           *trie.Database
	mu           sync.Mutex
}

const maxPastStates = 20

func NewTransactionProcessor(rnd PseudoRandomizer, db *StateDB, gasParams GasConfig, logger log.Log) *TransactionProcessor {
	return &TransactionProcessor{
		Log:          logger,
		rand:         rnd,
		globalState:  db,
		prevStates:   make(map[mesh.LayerID]common.Hash),
		currentLayer: 0,
		rootHash:     common.Hash{},
		stateQueue:   list.List{},
		gasCost:      gasParams,
		db:           db.TrieDB(),
		mu:           sync.Mutex{}, //sync between reset and apply mesh.Transactions
	}
}

//should receive sort predicate
func (tp *TransactionProcessor) ApplyTransactions(layer mesh.LayerID, txs mesh.Transactions) (uint32, error) {
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

func (tp *TransactionProcessor) addStateToHistory(layer mesh.LayerID, newHash common.Hash) {
	tp.stateQueue.PushBack(newHash)
	if tp.stateQueue.Len() > maxPastStates {
		hash := tp.stateQueue.Remove(tp.stateQueue.Back())
		tp.db.Commit(hash.(common.Hash), false)
	}
	tp.prevStates[layer] = newHash
	tp.db.Reference(newHash, common.Hash{})

}

func (tp *TransactionProcessor) ApplyRewards(layer mesh.LayerID, miners map[string]struct{}, underQuota map[string]struct{}, bonusReward, diminishedReward *big.Int) {
	for minerId, _ := range miners {
		if _, ok := underQuota[minerId]; ok {
			tp.globalState.AddBalance(address.HexToAddress(minerId), diminishedReward)
		} else {
			tp.globalState.AddBalance(address.HexToAddress(minerId), bonusReward)
		}
	}
	tp.globalState.Commit(false)
	newHash, err := tp.globalState.Commit(false)

	if err != nil {
		tp.Log.Error("db write error %v", err)
		return
	}

	tp.addStateToHistory(layer, newHash)

}

func (tp *TransactionProcessor) Reset(layer mesh.LayerID) {
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

func (tp *TransactionProcessor) randomSort(transactions mesh.Transactions) mesh.Transactions {
	vecLen := len(transactions)
	for i := range transactions {
		swp := int(tp.rand.Uint32()) % vecLen
		transactions[i], transactions[swp] = transactions[swp], transactions[i]
	}
	return transactions
}

func (tp *TransactionProcessor) coalescTransactionsBySender(transactions mesh.Transactions) map[address.Address][]*mesh.Transaction {
	trnsBySender := make(map[address.Address][]*mesh.Transaction)
	for _, trns := range transactions {
		trnsBySender[trns.Origin] = append(trnsBySender[trns.Origin], trns)
	}

	for key := range trnsBySender {
		sort.Slice(trnsBySender[key], func(i, j int) bool {
			//todo: add fix here:
			// if trnsBySender[key][i].AccountNonce == trnsBySender[key][j].AccountNonce { return trnsBySender[key][i].Hash() < trnsBySender[key][j].AccountNonce.Hash() }
			return trnsBySender[key][i].AccountNonce < trnsBySender[key][j].AccountNonce
		})
	}

	return trnsBySender
}

func (tp *TransactionProcessor) Process(transactions mesh.Transactions, trnsBySender map[address.Address][]*mesh.Transaction) (errors uint32) {
	senderPut := make(map[address.Address]struct{})
	sortedOriginByTransactions := make([]address.Address, 0, 10)
	errors = 0
	// The order of the mesh.Transactions determines the order addresses by which we take mesh.Transactions
	// Maybe refactor this
	for _, trans := range transactions {
		if _, ok := senderPut[trans.Origin]; !ok {
			sortedOriginByTransactions = append(sortedOriginByTransactions, trans.Origin)
			senderPut[trans.Origin] = struct{}{}
		}
	}

	for _, origin := range sortedOriginByTransactions {
		for _, trns := range trnsBySender[origin] {
			//todo: should we abort all transaction processing if we failed this one?
			err := tp.ApplyTransaction(trns)
			if err != nil {
				errors++
				log.Error("transaction aborted: %v", err)
			}

		}
	}
	return errors
}

func (tp *TransactionProcessor) pruneAfterRevert(targetLayerID mesh.LayerID) {
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

func (tp *TransactionProcessor) checkNonce(trns *mesh.Transaction) bool {
	return tp.globalState.GetNonce(trns.Origin) == trns.AccountNonce
}

var (
	ErrOrigin = "origin account doesnt exist"
	ErrFunds  = "insufficient funds"
	ErrNonce  = "incorrect nonce"
)

func (tp *TransactionProcessor) ApplyTransaction(trans *mesh.Transaction) error {
	if !tp.globalState.Exist(trans.Origin) {
		return fmt.Errorf(ErrOrigin)
	}

	origin := tp.globalState.GetOrNewStateObj(trans.Origin)

	gas := new(big.Int).Mul(trans.Price, tp.gasCost.BasicTxCost)

	/*if gas < trans.GasLimit {

	}*/

	amountWithGas := new(big.Int).Add(gas, trans.Amount)

	//todo: should we allow to spend all accounts balance?
	if origin.Balance().Cmp(amountWithGas) <= 0 {
		tp.Log.Error(ErrFunds+" have: %v need: %v", origin.Balance(), amountWithGas)
		return fmt.Errorf(ErrFunds)
	}

	if !tp.checkNonce(trans) {
		tp.Log.Error(ErrNonce+" should be %v actual %v", tp.globalState.GetNonce(trans.Origin), trans.AccountNonce)
		return fmt.Errorf(ErrNonce)
	}

	tp.globalState.SetNonce(trans.Origin, tp.globalState.GetNonce(trans.Origin)+1)
	transfer(tp.globalState, trans.Origin, *trans.Recipient, trans.Amount)

	//subtract gas from account, gas will be sent to miners in layers after
	tp.globalState.SubBalance(trans.Origin, gas)

	return nil
}

func transfer(db GlobalStateDB, sender, recipient address.Address, amount *big.Int) {
	db.SubBalance(sender, amount)
	db.AddBalance(recipient, amount)
}
