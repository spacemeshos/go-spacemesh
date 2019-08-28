package sync

import (
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"sync"
)

type FetchPoetProofFunc func(poetProofRef []byte) error
type GetValidAddressableTxFunc func(tx *types.SerializableSignedTransaction) (*types.AddressableSignedTransaction, error)
type SValidateAtxFunc func(atx *types.ActivationTx) error
type CheckLocalFunc func(atxIds []types.Hash32) (map[types.Hash32]Item, map[types.Hash32]Item, []types.Hash32)

type Item interface {
	Hash32() types.Hash32
	ShortString() string
}

type fetchJob struct {
	items []Item
	ids   []types.Hash32
}

//todo make the queue generic
type fetchQueue struct {
	log.Log
	FetchRequestFactory
	*sync.Mutex
	*mesh.Mesh
	workerInfra        WorkerInfra
	pending            map[types.Hash32][]chan bool
	updateDependencies func(fj fetchJob)
	checkLocal         CheckLocalFunc
	queue              chan []types.Hash32 //types.TransactionId //todo make buffered
}

//todo batches
func (fq *fetchQueue) work() error {
	output := fetchWithFactory(NewFetchWorker(fq.workerInfra, 1, fq.FetchRequestFactory, fq.queue))
	for out := range output {
		if out == nil {
			fq.Info("close queue")
			return nil
		}

		fq.updateDependencies(out.(fetchJob))
		fq.Debug("next batch")
	}
	return nil
}

func (fq *fetchQueue) addToQueue(ids []types.Hash32) chan bool {
	return getDoneChan(fq.addToPending(ids))
}

func (fq *fetchQueue) addToPending(ids []types.Hash32) []chan bool {
	fq.Lock()
	deps := make([]chan bool, 0, len(ids))
	var idsToAdd []types.Hash32
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

func (fq *fetchQueue) invalidate(id types.Hash32, valid bool) {
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
func (tq *fetchQueue) Handle(itemIds []types.Hash32) ([]Item, error) {
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

func NewTxQueue(s *Syncer, txValidator TxValidator) *txQueue {
	//todo buffersize
	q := &txQueue{
		fetchQueue: fetchQueue{
			Log:                 s.Log.WithName("txFetchQueue"),
			Mesh:                s.Mesh,
			workerInfra:         s.workerInfra,
			Mutex:               &sync.Mutex{},
			FetchRequestFactory: TxReqFactory(),
			checkLocal:          s.txCheckLocalFactory,
			pending:             make(map[types.Hash32][]chan bool),
			queue:               make(chan []types.Hash32, 1000)},
	}

	q.updateDependencies = updateTxDependencies(q.invalidate, s.txpool, txValidator.GetValidAddressableTx)
	go q.work()
	return q
}

//we could get rid of this if we had a unified id type
func (tx txQueue) HandleTxs(txids []types.TransactionId) ([]*types.AddressableSignedTransaction, error) {
	txItems := make([]types.Hash32, 0, len(txids))
	for _, i := range txids {
		txItems = append(txItems, i.Hash32())
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

func updateTxDependencies(invalidate func(id types.Hash32, valid bool), txpool TxMemPool, getValidAddrTx GetValidAddressableTxFunc) func(fj fetchJob) {
	return func(fj fetchJob) {
		if len(fj.items) == 0 {
			return
		}

		mp := map[types.Hash32]types.SerializableSignedTransaction{}

		for _, item := range fj.items {
			mp[item.Hash32()] = item.(types.SerializableSignedTransaction)
		}

		for _, id := range fj.ids {
			if item, ok := mp[id]; ok {
				tx, err := getValidAddrTx(&item)
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

func NewAtxQueue(s *Syncer, fetchPoetProof FetchPoetProofFunc) *atxQueue {
	//todo buffersize
	q := &atxQueue{
		fetchQueue: fetchQueue{
			Log:                 s.Log.WithName("atxFetchQueue"),
			Mesh:                s.Mesh,
			workerInfra:         s.workerInfra,
			FetchRequestFactory: ATxReqFactory(),
			Mutex:               &sync.Mutex{},
			checkLocal:          s.atxCheckLocalFactory,
			pending:             make(map[types.Hash32][]chan bool),
			queue:               make(chan []types.Hash32, 1000),
		},
	}

	q.updateDependencies = updateAtxDependencies(q.invalidate, s.SyntacticallyValidateAtx, s.atxpool, fetchPoetProof)
	go q.work()
	return q
}

//we could get rid of this if we had a unified id type
func (atx atxQueue) HandleAtxs(atxids []types.AtxId) ([]*types.ActivationTx, error) {
	atxItems := make([]types.Hash32, 0, len(atxids))
	for _, i := range atxids {
		atxItems = append(atxItems, i.Hash32())
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

func updateAtxDependencies(invalidate func(id types.Hash32, valid bool), sValidateAtx SValidateAtxFunc, atxpool AtxMemPool, fetchProof FetchPoetProofFunc) func(fj fetchJob) {
	return func(fj fetchJob) {
		if len(fj.items) == 0 {
			return
		}

		fetchProofCalcId(fetchProof, fj)

		mp := map[types.Hash32]types.ActivationTx{}
		for _, item := range fj.items {
			tmp := item.(types.ActivationTx)
			mp[item.Hash32()] = tmp
		}

		for _, id := range fj.ids {
			if atx, ok := mp[id]; ok {
				err := sValidateAtx(&atx)
				if err == nil {
					atxpool.Put(atx.Id(), &atx)
					invalidate(id, true)
					continue
				}
			}
			invalidate(id, false)
		}
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
	itemsWithProofs := make([]Item, 0, len(fj.items))
	for _, item := range fj.items {
		atx := item.(types.ActivationTx)
		atx.CalcAndSetId() //todo put it somewhere that will cause less confusion
		if err := fetchPoetProof(atx.GetPoetProofRef()); err != nil {
			log.Error("received atx (%v) with syntactically invalid or missing PoET proof (%x): %v",
				atx.ShortString(), atx.GetShortPoetProofRef(), err)
			continue
		}
		itemsWithProofs = append(itemsWithProofs, atx)
	}

	fj.items = itemsWithProofs
}
