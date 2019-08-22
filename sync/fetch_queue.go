package sync

import (
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/types"
	"sync"
)

type FetchPoetProofFunc func(poetProofRef []byte) error
type GetValidAddressableTxFunc func(tx *types.SerializableSignedTransaction) (*types.AddressableSignedTransaction, error)
type SValidateAtxFunc func(atx *types.ActivationTx) error
type CheckLocalFunc func(atxIds []common.Hash) (map[common.Hash]Item, map[common.Hash]Item, []common.Hash)

type Item interface {
	ItemId() common.Hash
}

type fetchJob struct {
	items []Item
	ids   []common.Hash
}

//todo make the queue generic
type fetchQueue struct {
	log.Log
	FetchRequestFactory
	*sync.Mutex
	*mesh.Mesh
	workerInfra        WorkerInfra
	pending            map[common.Hash][]chan bool
	updateDependencies func(fj fetchJob)
	checkLocal         CheckLocalFunc
	queue              chan []common.Hash //types.TransactionId //todo make buffered
}

//todo batches
func (fq *fetchQueue) work() error {
	output := fetchWithFactory(NewFetchWorker(fq.workerInfra, 1, fq.FetchRequestFactory, fq.queue))
	for out := range output {

		if out == nil {
			fq.Info("close queue")
			return nil
		}

		txs := out.(fetchJob)
		fq.updateDependencies(txs) //todo hack add batches
		fq.Debug("next batch")
	}
	return nil
}

func (fq *fetchQueue) addToQueue(ids []common.Hash) chan bool {
	return getDoneChan(fq.addToPending(ids))
}

func (fq *fetchQueue) addToPending(ids []common.Hash) []chan bool {
	fq.Lock()
	deps := make([]chan bool, 0, len(ids))
	var idsToAdd []common.Hash
	for _, id := range ids {
		ch := make(chan bool, 1)
		deps = append(deps, ch)
		if _, ok := fq.pending[id]; !ok {
			idsToAdd = append(idsToAdd, id)
		}
		fq.pending[id] = append(fq.pending[id], ch)
	}
	fq.Unlock()
	fq.queue <- idsToAdd
	return deps
}

func (fq *fetchQueue) invalidate(id common.Hash, valid bool) {
	fq.Debug("done with %v !!!!!!!!!!!!!!!! %v", id.ShortString(), valid)
	fq.Lock()
	deps := fq.pending[id]
	delete(fq.pending, id)
	fq.Unlock()

	for _, dep := range deps {
		dep <- valid
	}
}

//returns txs out of txids that are not in the local database
func (tq *fetchQueue) Handle(itemIds []common.Hash) ([]Item, error) {
	if len(itemIds) == 0 {
		tq.Debug("handle empty item ids slice")
		return nil, nil
	}

	unprocessedItems, processedItems, missing := tq.checkLocal(itemIds)
	if len(missing) > 0 {

		output := tq.addToQueue(missing)

		if success := <-output; !success {
			return nil, errors.New(fmt.Sprintf("could not fetch all items"))
		}

		unprocessedItems, processedItems, missing = tq.checkLocal(itemIds)
		if len(missing) > 0 {
			return nil, errors.New("could not find all items even though fetch was successful")
		}
	}

	txs := make([]Item, 0, len(itemIds))
	for _, id := range itemIds {
		if tx, ok := unprocessedItems[id]; ok {
			txs = append(txs, tx)
		} else if _, ok := processedItems[id]; ok {
			continue
		} else {
			return nil, errors.New(fmt.Sprintf("item %v was not found after fetch was done", hex.EncodeToString(id[:])))
		}
	}
	return txs, nil
}

//todo make the queue generic
type txQueue struct {
	fetchQueue
}

func NewTxQueue(msh *mesh.Mesh, srv WorkerInfra, txpool TxMemPool, txValidator TxValidator, lg log.Log) *txQueue {
	//todo buffersize
	q := &txQueue{
		fetchQueue: fetchQueue{
			Log:                 lg,
			Mesh:                msh,
			workerInfra:         srv,
			Mutex:               &sync.Mutex{},
			FetchRequestFactory: TxReqFactory(),
			checkLocal:          txCheckLocalFactory(msh, lg, txpool),
			pending:             make(map[common.Hash][]chan bool),
			queue:               make(chan []common.Hash, 1000)},
	}

	q.updateDependencies = updateTxDependencies(q.invalidate, txpool, txValidator.GetValidAddressableTx)
	go q.work()
	return q
}

//we could get rid of this if we had a unified id type
func (tx txQueue) HandleTxs(txids []types.TransactionId) ([]*types.AddressableSignedTransaction, error) {
	txItems := make([]common.Hash, 0, len(txids))
	for _, i := range txids {
		txItems = append(txItems, i.ItemId())
	}

	txres, err := tx.Handle(txItems)
	if err != nil {
		return nil, err
	}

	txs := make([]*types.AddressableSignedTransaction, 0, len(txres))
	for _, i := range txres {
		txs = append(txs, i.(*types.AddressableSignedTransaction))
	}

	return txs, nil
}

func updateTxDependencies(invalidate func(id common.Hash, valid bool), txpool TxMemPool, getValidAddrTx GetValidAddressableTxFunc) func(fj fetchJob) {
	return func(fj fetchJob) {
		if len(fj.items) == 0 {
			return
		}

		mp := map[common.Hash]*types.SerializableSignedTransaction{}

		for _, item := range fj.items {
			mp[item.ItemId()] = item.(*types.SerializableSignedTransaction)
		}

		for _, id := range fj.ids {
			if item, ok := mp[id]; ok {
				tx, err := getValidAddrTx(item)
				if err == nil {
					txpool.Put(types.TransactionId(id), tx)
					invalidate(id, true)
					continue
				}
			}
			invalidate(id, false)
		}
	}
}

type atxQueue struct {
	fetchQueue
}

func NewAtxQueue(msh *mesh.Mesh, srv WorkerInfra, atxpool AtxMemPool, fetchPoetProof FetchPoetProofFunc, lg log.Log) *atxQueue {
	//todo buffersize
	q := &atxQueue{
		fetchQueue: fetchQueue{
			Log:                 lg,
			Mesh:                msh,
			workerInfra:         srv,
			FetchRequestFactory: ATxReqFactory(),
			Mutex:               &sync.Mutex{},
			checkLocal:          atxCheckLocalFactory(msh, lg, atxpool),
			pending:             make(map[common.Hash][]chan bool),
			queue:               make(chan []common.Hash, 1000),
		},
	}

	q.updateDependencies = updateAtxDependencies(q.invalidate, msh.SyntacticallyValidateAtx, atxpool, fetchPoetProof)
	go q.work()
	return q
}

//we could get rid of this if we had a unified id type
func (atx atxQueue) HandleAtxs(atxids []types.AtxId) ([]*types.ActivationTx, error) {
	atxItems := make([]common.Hash, 0, len(atxids))
	for _, i := range atxids {
		atxItems = append(atxItems, i.ItemId())
	}

	atxres, err := atx.Handle(atxItems)
	if err != nil {
		return nil, err
	}

	atxs := make([]*types.ActivationTx, 0, len(atxres))
	for _, i := range atxres {
		atxs = append(atxs, i.(*types.ActivationTx))
	}

	return atxs, nil
}

func updateAtxDependencies(invalidate func(id common.Hash, valid bool), sValidateAtx SValidateAtxFunc, atxpool AtxMemPool, fetchProof FetchPoetProofFunc) func(fj fetchJob) {
	return func(fj fetchJob) {
		if len(fj.items) == 0 {
			return
		}

		fetchProofCalcId(fetchProof, fj)

		mp := map[common.Hash]*types.ActivationTx{}
		for _, item := range fj.items {
			mp[item.ItemId()] = item.(*types.ActivationTx)
		}

		for _, id := range fj.ids {
			if atx, ok := mp[id]; ok {
				err := sValidateAtx(atx)
				if err == nil {
					atxpool.Put(atx.Id(), atx)
					invalidate(id, true)
					continue
				}
			}
			invalidate(id, false)
		}
	}

}

func atxCheckLocalFactory(msh *mesh.Mesh, lg log.Log, atxpool AtxMemPool) func(atxIds []common.Hash) (map[common.Hash]Item, map[common.Hash]Item, []common.Hash) {
	//look in pool
	return func(atxIds []common.Hash) (map[common.Hash]Item, map[common.Hash]Item, []common.Hash) {
		unprocessedItems := make(map[common.Hash]Item, len(atxIds))
		missingInPool := make([]types.AtxId, 0, len(atxIds))
		for _, t := range atxIds {
			id := types.AtxId{Hash: t}
			if x, err := atxpool.Get(id); err == nil {
				atx := x
				lg.Debug("found atx, %v in atx pool", id.ShortString())
				unprocessedItems[id.ItemId()] = &atx
			} else {
				lg.Debug("atx %v not in atx pool", id.ShortString())
				missingInPool = append(missingInPool, id)
			}
		}
		//look in db
		dbAtxs, missing := msh.GetATXs(missingInPool)

		dbItems := make(map[common.Hash]Item, len(dbAtxs))
		for i := range dbAtxs {
			dbItems[i.Hash] = i
		}

		missingItems := make([]common.Hash, 0, len(missing))
		for _, i := range missing {
			missingItems = append(missingItems, i.Hash)
		}

		return unprocessedItems, dbItems, missingItems
	}
}

func txCheckLocalFactory(msh *mesh.Mesh, lg log.Log, txpool TxMemPool) func(txids []common.Hash) (map[common.Hash]Item, map[common.Hash]Item, []common.Hash) {
	//look in pool
	return func(txIds []common.Hash) (map[common.Hash]Item, map[common.Hash]Item, []common.Hash) {
		unprocessedItems := make(map[common.Hash]Item)
		missingInPool := make([]types.TransactionId, 0)
		for _, t := range txIds {
			id := types.TransactionId(t)
			if tx, err := txpool.Get(id); err == nil {
				lg.Debug("found tx, %v in tx pool", hex.EncodeToString(t[:]))
				unprocessedItems[id.ItemId()] = &tx
			} else {
				lg.Debug("tx %v not in atx pool", hex.EncodeToString(t[:]))
				missingInPool = append(missingInPool, id)
			}
		}
		//look in db
		dbTxs, missing := msh.GetTransactions(missingInPool)

		dbItems := make(map[common.Hash]Item, len(dbTxs))
		for i := range dbTxs {
			dbItems[i.ItemId()] = i
		}

		missingItems := make([]common.Hash, 0, len(missing))
		for _, i := range missing {
			missingItems = append(missingItems, i.ItemId())
		}

		return unprocessedItems, dbItems, missingItems
	}
}

func getDoneChan(deps []chan bool) chan bool {
	doneChan := make(chan bool)
	//fan in
	go func() {
		alldone := true
		for _, c := range deps {
			if done := <-c; !done {
				alldone = false
				break
			}
		}
		doneChan <- alldone
		close(doneChan)
	}()
	return doneChan
}

//todo get rid of this, send proofs with atxs
func fetchProofCalcId(fetchPoetProof FetchPoetProofFunc, fj fetchJob) {
	itemsWithProofs := make([]Item, len(fj.items))
	for _, item := range fj.items {
		atx := item.(*types.ActivationTx)
		atx.CalcAndSetId() //todo put it somewhere that will cause less confusion
		if err := fetchPoetProof(atx.GetPoetProofRef()); err != nil {
			log.Error("received atx (%v) with syntactically invalid or missing PoET proof (%x): %v",
				atx.ShortId(), atx.GetShortPoetProofRef(), err)
			continue
		}
		itemsWithProofs = append(itemsWithProofs, atx)
	}

	fj.items = itemsWithProofs
}
