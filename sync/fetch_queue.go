package sync

import (
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/types"
	"sync"
)

type RequestFactoryV2 func(s *MessageServer, peer p2p.Peer, id interface{}) (chan interface{}, error)
type FetchPoetProof func(poetProofRef []byte) error

func NewTxQueue(msh *mesh.Mesh, srv *MessageServer, txpool TxMemPool, txValidator TxValidator, lg log.Log) *txQueue {
	//todo buffersize

	q := &txQueue{
		Log:           lg,
		Mesh:          msh,
		MessageServer: srv,
		TxValidator:   txValidator,
		txpool:        txpool,
		queue:         make(chan *fetchJob, 100),
		pending:       make(map[types.TransactionId][]chan bool),
	}
	go q.work()
	return q
}

func NewAtxQueue(msh *mesh.Mesh, srv *MessageServer, atxpool AtxMemPool, fetchPoetProof FetchPoetProof, lg log.Log) *atxQueue {
	//todo buffersize

	q := &atxQueue{
		Log:            lg,
		Mesh:           msh,
		FetchPoetProof: fetchPoetProof,
		MessageServer:  srv,
		atxpool:        atxpool,
		queue:          make(chan *fetchJob, 100),
		pending:        make(map[types.AtxId][]chan bool),
	}
	go q.work()
	return q
}

type fetchJob struct {
	items interface{}
	ids   interface{}
}

//todo make the queue generic
type txQueue struct {
	log.Log
	sync.Mutex
	*mesh.Mesh
	*MessageServer
	TxValidator
	txpool  TxMemPool
	queue   chan *fetchJob //types.TransactionId //todo make buffered
	pending map[types.TransactionId][]chan bool
}

//todo batches
func (tq *txQueue) work() error {
	output := fetchWithFactory(NewFetchWorker(tq.MessageServer, tq.Log, 1, TxReqFactory(), tq.queue))
	for out := range output {

		if out == nil {
			tq.Info("close queue")
			return nil
		}

		txs := out.(fetchJob)
		tq.updateDependencies(txs) //todo hack add batches
		tq.Debug("next batch")
	}

	return nil
}

func (tq *txQueue) updateDependencies(fj fetchJob) {
	items, ok := fj.items.([]*types.SerializableSignedTransaction)
	if !ok {
		tq.Warning("could not fetch any")
		return
	}
	mp := map[types.TransactionId]*types.SerializableSignedTransaction{}

	for _, item := range items {
		mp[types.GetTransactionId(item)] = item
	}

	for _, id := range fj.ids.([]types.TransactionId) {
		if item, ok := mp[id]; ok {
			tx, err := tq.GetValidAddressableTx(item)
			if err == nil {
				tq.invalidate(id, tx, true)
				continue
			}
		}
		tq.invalidate(id, nil, false)
	}
}

func (tq *txQueue) invalidate(id types.TransactionId, tx *types.AddressableSignedTransaction, valid bool) {
	tq.Debug("done with %v !!!!!!!!!!!!!!!! %v", id, valid)
	if valid {
		tq.txpool.Put(id, tx)
	}

	tq.Lock()
	deps := tq.pending[id]
	delete(tq.pending, id)
	tq.Unlock()
	for _, dep := range deps {
		dep <- valid
	}
}

func (tq *txQueue) addToQueue(ids []types.TransactionId) chan bool {

	deps := tq.addToPending(ids)
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

func (tq *txQueue) addToPending(ids []types.TransactionId) []chan bool {
	tq.Lock()
	deps := make([]chan bool, 0, len(ids))
	var idsToAdd []types.TransactionId
	for _, id := range ids {
		ch := make(chan bool, 1)
		deps = append(deps, ch)
		if _, ok := tq.pending[id]; !ok {
			idsToAdd = append(idsToAdd, id)
		}
		tq.pending[id] = append(tq.pending[id], ch)
	}
	tq.Unlock()
	tq.queue <- &fetchJob{ids: idsToAdd}
	return deps
}

//todo minimize code duplication
//returns txs out of txids that are not in the local database
func (tq *txQueue) Handle(txids []types.TransactionId) ([]*types.AddressableSignedTransaction, error) {
	if len(txids) == 0 {
		tq.Debug("handle empty tx slice")
		return nil, nil
	}

	unprocessedTxs, processedTxs, missing := tq.checkLocalTxs(txids)
	if len(missing) > 0 {

		output := tq.addToQueue(missing)

		if success := <-output; !success {
			return nil, errors.New(fmt.Sprintf("could not fetch all txs"))
		}

		unprocessedTxs, processedTxs, missing = tq.checkLocalTxs(txids)
		if len(missing) > 0 {
			return nil, errors.New("something got fudged2")
		}
	}

	txs := make([]*types.AddressableSignedTransaction, 0, len(txids))
	for _, id := range txids {
		if tx, ok := unprocessedTxs[id]; ok {
			txs = append(txs, tx)
		} else if _, ok := processedTxs[id]; ok {
			continue
		} else {
			return nil, errors.New(fmt.Sprintf("atx %v was not found after fetch was done", hex.EncodeToString(id[:])))
		}
	}
	return txs, nil
}

func (tq *txQueue) checkLocalTxs(txids []types.TransactionId) (map[types.TransactionId]*types.AddressableSignedTransaction, map[types.TransactionId]*types.AddressableSignedTransaction, []types.TransactionId) {
	//look in pool
	unprocessedTxs := make(map[types.TransactionId]*types.AddressableSignedTransaction)
	missingInPool := make([]types.TransactionId, 0)
	for _, t := range txids {
		if tx, err := tq.txpool.Get(t); err == nil {
			tq.Debug("found tx, %v in tx pool", hex.EncodeToString(t[:]))
			unprocessedTxs[t] = &tx
		} else {
			tq.Debug("atx %v not in atx pool", hex.EncodeToString(t[:]))
			missingInPool = append(missingInPool, t)
		}
	}
	//look in db
	dbTxs, missing := tq.GetTransactions(missingInPool)
	return unprocessedTxs, dbTxs, missing
}

type atxQueue struct {
	log.Log
	sync.Mutex
	*mesh.Mesh
	FetchPoetProof
	*MessageServer
	atxpool AtxMemPool
	queue   chan *fetchJob //types.AtxId
	pending map[types.AtxId][]chan bool
}

//todo batches
func (aq *atxQueue) work() error {
	output := fetchWithFactory(NewFetchWorker(aq.MessageServer, aq.Log, 1, ATxReqFactory(), aq.queue))
	for out := range output {
		if out == nil {
			aq.Info("close queue")
			return nil
		}
		txs := out.(fetchJob)
		aq.fetchPoetProofs(txs)    //removes atxs with proofs we could not fetch
		aq.updateDependencies(txs) //todo hack add batches
		aq.Debug("next batch")
	}

	return nil
}

//todo get rid of this, send proofs with atxs
func (aq *atxQueue) fetchPoetProofs(fj fetchJob) {
	items, ok := fj.items.([]*types.ActivationTx)
	if !ok {
		aq.Warning("could not fetch any")
		return
	}

	itemsWithProofs := make([]*types.ActivationTx, len(items))
	for _, item := range items {
		item.CalcAndSetId() //todo put it somewhere that will cause less confusion
		if err := aq.FetchPoetProof(item.GetPoetProofRef()); err != nil {
			aq.Error("received atx (%v) with syntactically invalid or missing PoET proof (%x): %v",
				item.ShortId(), item.GetShortPoetProofRef(), err)
			continue
		}
		itemsWithProofs = append(itemsWithProofs, item)
	}

	fj.items = itemsWithProofs
}

func (aq *atxQueue) updateDependencies(fj fetchJob) {
	items, ok := fj.items.([]*types.ActivationTx)
	if !ok {
		aq.Warning("could not fetch any")
		return
	}

	mp := map[types.AtxId]*types.ActivationTx{}
	for _, item := range items {
		mp[item.Id()] = item
	}

	for _, id := range fj.ids.([]types.AtxId) {
		if atx, ok := mp[id]; ok {
			err := aq.SyntacticallyValidateAtx(atx)
			if err == nil {
				aq.invalidate(id, atx, true)
				continue
			}
		}
		aq.invalidate(id, nil, false)
	}

}

func (aq *atxQueue) invalidate(id types.AtxId, atx *types.ActivationTx, valid bool) {
	aq.Debug("done with %v !!!!!!!!!!!!!!!! %v", id.ShortId(), valid)

	if valid && atx != nil {
		aq.atxpool.Put(id, atx)
	}

	aq.Lock()
	deps := aq.pending[id]
	delete(aq.pending, id)
	aq.Unlock()

	for _, dep := range deps {
		dep <- valid
	}
}

func (aq *atxQueue) addToQueue(ids []types.AtxId) chan bool {
	deps := aq.addToPending(ids)
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

func (aq *atxQueue) addToPending(ids []types.AtxId) []chan bool {
	aq.Lock()
	deps := make([]chan bool, 0, len(ids))
	var idsToAdd []types.AtxId
	for _, id := range ids {
		ch := make(chan bool, 1)
		deps = append(deps, ch)
		if _, ok := aq.pending[id]; !ok {
			idsToAdd = append(idsToAdd, id)
		}
		aq.pending[id] = append(aq.pending[id], ch)
	}
	aq.Unlock()
	aq.queue <- &fetchJob{ids: idsToAdd}
	return deps
}

//todo minimize code duplication
//returns atxs out of atxids that are not in the local database
func (aq *atxQueue) Handle(atxIds []types.AtxId) ([]*types.ActivationTx, error) {
	if len(atxIds) == 0 {
		aq.Debug("handle empty tx slice")
		return nil, nil
	}

	unprocessedAtxs, processedAtxs, missing := aq.checkLocalAtxs(atxIds)
	if len(missing) > 0 {
		output := aq.addToQueue(missing)

		if success := <-output; !success {
			return nil, errors.New(fmt.Sprintf("could not fetch all atxs"))
		}

		unprocessedAtxs, processedAtxs, missing = aq.checkLocalAtxs(atxIds)
		if len(missing) > 0 {
			return nil, errors.New("something got fudged2")
		}
	}

	atxs := make([]*types.ActivationTx, 0, len(atxIds))
	for _, id := range atxIds {
		if tx, ok := unprocessedAtxs[id]; ok {
			atxs = append(atxs, tx)
		} else if _, ok := processedAtxs[id]; ok {
			continue
		} else {
			return nil, errors.New(fmt.Sprintf("atx %v was not found after fetch was done", id.ShortId()))
		}
	}
	return atxs, nil
}

func (aq *atxQueue) checkLocalAtxs(atxIds []types.AtxId) (map[types.AtxId]*types.ActivationTx, map[types.AtxId]*types.ActivationTx, []types.AtxId) {
	//look in pool
	unprocessedAtxs := make(map[types.AtxId]*types.ActivationTx, len(atxIds))
	missingInPool := make([]types.AtxId, 0, len(atxIds))
	for _, t := range atxIds {
		id := t
		if x, err := aq.atxpool.Get(id); err == nil {
			atx := x
			aq.Debug("found atx, %v in atx pool", id.ShortId())
			unprocessedAtxs[id] = &atx
		} else {
			aq.Debug("atx %v not in atx pool", id.ShortId())
			missingInPool = append(missingInPool, id)
		}
	}
	//look in db
	dbAtxs, missing := aq.GetATXs(missingInPool)

	return unprocessedAtxs, dbAtxs, missing
}

func (tq *txQueue) validateAndBuildTx(x *types.SerializableSignedTransaction) (*types.AddressableSignedTransaction, error) {
	addr, err := tq.ValidateTransactionSignature(x)
	if err != nil {
		return nil, err
	}

	return &types.AddressableSignedTransaction{SerializableSignedTransaction: x, Address: addr}, nil
}
