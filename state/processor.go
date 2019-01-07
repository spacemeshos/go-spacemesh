package state

import (
	"container/list"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/crypto/sha3"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/rlp"
	"github.com/spacemeshos/go-spacemesh/trie"
	"math/big"
	"sort"
	"sync"
)


type LayerID uint64



//todo: this object should be splitted into two parts: one is the actual value serialized into trie, and an containig obj with caches
type Transaction struct {
	AccountNonce 	uint64
	Price			*big.Int
	GasLimit		uint64
	Recipient 		*common.Address
	Origin			common.Address //todo: remove this, should be calculated from sig.
	Amount       	*big.Int
	Payload      	[]byte

	//todo: add signatures

	hash *common.Hash
}

func NewTransaction(nonce uint64, origin common.Address, destination common.Address,
					amount *big.Int, gasLimit uint64, gasPrice *big.Int) *Transaction {
	return &Transaction{
		AccountNonce: nonce,
		Origin: origin,
		Recipient:&destination,
		Amount: amount,
		GasLimit:gasLimit,
		Price:gasPrice,
		hash:nil,
		Payload:nil,
	}
}

func rlpHash(x interface{}) (h common.Hash) {
	hw := sha3.NewKeccak256()
	rlp.Encode(hw, x)
	hw.Sum(h[:0])
	return h
}

func (tx *Transaction) Hash() common.Hash{
	if tx.hash == nil {
		hash := rlpHash(tx)
		tx.hash = &hash
	}
	return *tx.hash
}

type Transactions []*Transaction

type PseudoRandomizer interface {
	Uint32() uint32
	Uint64() uint64
}

type StatePreImages struct {
	rootHash common.Hash
	preImages Transactions
}

type TransactionProcessor struct {
	rand PseudoRandomizer
	globalState *StateDB
	prevStates map[LayerID]common.Hash
	currentLayer LayerID
	rootHash common.Hash
	stateQueue list.List
	db *trie.Database
	mu sync.Mutex
}

const maxPastStates = 20

func NewTransactionProcessor(rnd PseudoRandomizer, db *StateDB) *TransactionProcessor{
	return &TransactionProcessor{
		rand: rnd,
		globalState:db,
		prevStates : make(map[LayerID]common.Hash),
		currentLayer: 0,
		rootHash: common.Hash{},
		stateQueue: list.List{},
		db : db.TrieDB(),
		mu : sync.Mutex{}, //sync between reset and apply transactions
	}
}

//should receive sort predicate
func (tp *TransactionProcessor) ApplyTransactions(layer LayerID, transactions Transactions) (uint32, error){
	//todo: need to seed the mersenne twister with random beacon seed

	txs := tp.mergeDoubles(transactions)
	tp.mu.Lock()
	defer tp.mu.Unlock()
	failed := tp.Process(tp.randomSort(txs), tp.coalesceTransactionsBySender(txs))
	newHash , err := tp.globalState.Commit(false)
	log.Info("new state root for layer %v is %x", layer, newHash)
	if err != nil {
		log.Error("db write error %v", err)
		return failed,err
	}

	tp.stateQueue.PushBack(newHash)
	if tp.stateQueue.Len() > maxPastStates {
		hash := tp.stateQueue.Remove(tp.stateQueue.Back())
		tp.db.Commit(hash.(common.Hash),false)
	}
	tp.prevStates[layer] = newHash
	tp.db.Reference(newHash, common.Hash{})

	return failed, nil
}

func (tp *TransactionProcessor) Reset(layer LayerID){
	tp.mu.Lock()
	defer tp.mu.Unlock()
	if state, ok := tp.prevStates[layer]; ok {
		newState, err := New(state, tp.globalState.db)
		log.Info("reverted, new root %x", newState.IntermediateRoot(false))
		if err != nil {
			panic("cannot revert- improper state")
		}

		tp.globalState = newState
		tp.pruneAfterRevert(layer)
	}
}



func (tp *TransactionProcessor) mergeDoubles(transactions Transactions) Transactions{
	transactionSet := make(map[common.Hash]struct{})
	merged := make(Transactions, 0, len(transactions))
	for _,trns := range transactions {
		if _,ok := transactionSet[trns.Hash()]; !ok {
			transactionSet[trns.Hash()] = struct{}{}
			merged = append(merged, trns)
		}
	}
	return merged
}

func (tp *TransactionProcessor) randomSort(transactions Transactions) Transactions{
	vecLen := len(transactions)
	for i := range transactions {
		swp := int(tp.rand.Uint32()) % vecLen
		transactions[i], transactions[swp] = transactions[swp], transactions[i]
	}
	return transactions
}

func (tp *TransactionProcessor) coalesceTransactionsBySender(transactions Transactions) map[common.Address][]*Transaction {
	trnsBySender := make(map[common.Address][]*Transaction)
	for _, trns := range transactions {
		trnsBySender[trns.Origin] = append(trnsBySender[trns.Origin], trns)
	}

	for key := range trnsBySender{
		sort.Slice(trnsBySender[key], func(i, j int) bool {
			//todo: add fix here:
			// if trnsBySender[key][i].AccountNonce == trnsBySender[key][j].AccountNonce { return trnsBySender[key][i].Hash() < trnsBySender[key][j].AccountNonce.Hash() }
			return trnsBySender[key][i].AccountNonce < trnsBySender[key][j].AccountNonce
		})
	}

	return trnsBySender
}

func (tp *TransactionProcessor) Process(transactions Transactions, trnsBySender map[common.Address][]*Transaction) (errors uint32){
	senderPut := make(map[common.Address]struct{})
	sortedOriginByTransactions := make([]common.Address, 0,10)
	errors = 0
	// The order of the transactions determines the order addresses by which we take transactions
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


func (tp *TransactionProcessor) pruneAfterRevert(targetLayerID LayerID){
	//needs to be called under mutex lock
	for i:= tp.currentLayer; i >= targetLayerID; i-- {
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

func (tp *TransactionProcessor) checkNonce(trns *Transaction) bool{
	return tp.globalState.GetNonce(trns.Origin) == trns.AccountNonce
}

var(
	ErrOrigin = "origin account doesnt exist"
	ErrFunds = "insufficient funds"
	ErrNonce = "incorrect nonce"
)
//todo: mining fees...
func (tp *TransactionProcessor) ApplyTransaction(trans *Transaction) error{
	if !tp.globalState.Exist(trans.Origin) {
		return  fmt.Errorf(ErrOrigin)
	}

	origin := tp.globalState.GetOrNewStateObj(trans.Origin)

	//todo: should we allow to spend all accounts data?
	if origin.Balance().Cmp(trans.Amount) <= 0 {
		log.Error(ErrFunds + " have: %v need: %v", origin.Balance(), trans.Amount)
		return  fmt.Errorf(ErrFunds)
	}

	if !tp.checkNonce(trans) {
		log.Error(ErrNonce + " should be %v actual %v", tp.globalState.GetNonce(trans.Origin), trans.AccountNonce)
		return  fmt.Errorf(ErrNonce)
	}

	tp.globalState.SetNonce(trans.Origin, tp.globalState.GetNonce(trans.Origin) + 1)
	transfer(tp.globalState,trans.Origin, *trans.Recipient, trans.Amount)

	return nil
}

func transfer(db GlobalStateDB, sender, recipient common.Address, amount *big.Int) {
	db.SubBalance(sender, amount)
	db.AddBalance(recipient, amount)
}
