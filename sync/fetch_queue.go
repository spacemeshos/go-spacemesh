package sync

import (
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/types"
	"sync"
	"time"
)

type RequestFactoryV2 func(s *server.MessageServer, peer p2p.Peer, id interface{}) (chan interface{}, error)

func NewTxQueue(s *Syncer) *txQueue {
	//todo buffersize
	q := &txQueue{Syncer: s, queue: make(chan interface{}, 100), pending: make(map[types.TransactionId][]chan bool), txlocks: map[types.TransactionId]*sync.Mutex{}}
	go q.work()
	return q
}

func NewAtxQueue(s *Syncer) *atxQueue {
	//todo buffersize
	q := &atxQueue{Syncer: s, queue: make(chan interface{}, 100), pending: make(map[types.AtxId][]chan bool), atxlocks: map[types.AtxId]*sync.Mutex{}}
	go q.work()
	return q
}

type fetchJob struct {
	items interface{}
	ids   interface{}
}

//todo make the queue generic
type txQueue struct {
	*Syncer
	sync.Mutex
	txlocks map[types.TransactionId]*sync.Mutex
	queue   chan interface{} //types.TransactionId //todo make buffered
	pending map[types.TransactionId][]chan bool
}

type atxQueue struct {
	*Syncer
	sync.Mutex
	atxlocks map[types.AtxId]*sync.Mutex
	queue    chan interface{} //types.AtxId
	pending  map[types.AtxId][]chan bool
}

//todo batches
func (tq *txQueue) work() error {
	output := tq.fetchWithFactory(NewFetchWorker(tq.Syncer, tq.Concurrency, TxReqFactoryV2(), tq.queue))
	for out := range output {
		if out == nil {
			tq.Info("close queue")
			return nil
		}
		txs := out.(fetchJob)
		if txs.items == nil {
			tq.Info("could not fetch any txs for ids %v", txs.ids)
			continue
		}
		tq.Info("next id %v", txs)
		tq.updateDependencies(txs) //todo hack add batches
		tq.Info("next batch")
	}

	return nil
}

func (tq *txQueue) updateDependencies(fj fetchJob) {
	mp := map[types.TransactionId]types.SerializableSignedTransaction{}
	for _, item := range fj.items.([]types.SerializableSignedTransaction) {
		mp[types.GetTransactionId(&item)] = item
	}

	for _, id := range fj.ids.([]types.TransactionId) {
		if item, ok := mp[id]; ok {
			tq.invalidate(id, &item, true)
			continue
		}
		tq.invalidate(id, nil, false)
	}
}

func (tq *txQueue) invalidate(id types.TransactionId, tx *types.SerializableSignedTransaction, valid bool) {
	tq.Lock()
	tq.Info("done with %v !!!!!!!!!!!!!!!! %v", hex.EncodeToString(id[:]), valid)
	if valid && tx != nil {
		tmp, err := tq.validateAndBuildTx(tx)
		if err == nil {
			tq.txpool.Put(id, tmp)
		}
	}
	for _, dep := range tq.pending[id] {
		dep <- valid
	}
	delete(tq.pending, id)
	tq.Unlock()
}

//todo batches
func (aq *atxQueue) work() error {
	output := aq.fetchWithFactory(NewFetchWorker(aq.Syncer, aq.Concurrency, ATxReqFactoryV2(), aq.queue))
	for out := range output {
		if out == nil {
			aq.Info("close queue")
			return nil
		}
		txs := out.(fetchJob)
		if txs.items == nil {
			aq.Info("could not fetch any txs for ids %v", txs.ids)
			continue
		}

		aq.Info("next id %v", txs)
		aq.updateDependencies(txs) //todo hack add batches
		aq.Info("next batch")
	}

	return nil
}

func (tq *txQueue) addToQueue(ids []types.TransactionId) chan bool {

	deps := tq.addToPending(ids)
	tq.queue <- ids

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
	for _, id := range ids {
		ch := make(chan bool, 1)
		deps = append(deps, ch)
		tq.pending[id] = append(tq.pending[id], ch)
	}
	tq.Unlock()
	return deps
}

func (aq *atxQueue) updateDependencies(fj fetchJob) {
	mp := map[types.AtxId]types.ActivationTx{}
	for _, item := range fj.items.([]types.ActivationTx) {
		item.CalcAndSetId() //todo put it somewhere that will cause less confusion
		mp[item.Id()] = item
	}
	for _, id := range fj.ids.([]types.AtxId) {
		if item, ok := mp[id]; ok {
			aq.invalidate(id, &item, true)
			continue
		}
		aq.invalidate(id, nil, false)
	}
}

func (aq *atxQueue) invalidate(id types.AtxId, atx *types.ActivationTx, valid bool) {
	aq.Lock()
	aq.Info("done with %v !!!!!!!!!!!!!!!! %v", id.ShortId(), valid)

	if valid && atx != nil {
		aq.atxpool.Put(id, atx)
	}

	for _, dep := range aq.pending[id] {
		dep <- valid
	}

	delete(aq.pending, id)
	aq.Unlock()
}

func (aq *atxQueue) addToQueue(ids []types.AtxId) chan bool {
	deps := aq.addToPending(ids)
	aq.queue <- ids

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
	for _, id := range ids {
		ch := make(chan bool, 1)
		deps = append(deps, ch)
		aq.pending[id] = append(aq.pending[id], ch)
	}
	aq.Unlock()
	return deps
}

//todo minimize code duplication
//returns txs out of txids that are not in the local database
func (tq *txQueue) Handle(txids []types.TransactionId) ([]*types.AddressableSignedTransaction, error) {

	tq.lockTxs(txids)
	_, _, missing := tq.checkLocalTxs(txids)
	output := tq.addToQueue(missing)
	tq.unlockTxs(txids)

	if success := <-output; !success {
		return nil, errors.New(fmt.Sprintf("could not fetch all txs"))
	}

	unprocessedTxs, processedTxs, missing := tq.checkLocalTxs(txids)
	if len(missing) > 0 {
		return nil, errors.New("something got fudged2")
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

func (tq *txQueue) unlockTxs(txids []types.TransactionId) {
	tq.Lock()

	var locks []*sync.Mutex
	for _, id := range txids {
		locks = append(locks, tq.txlocks[id])
		delete(tq.txlocks, id)
	}
	tq.Unlock()

	for _, lock := range locks {
		lock.Unlock()
	}
}

func (tq *txQueue) lockTxs(txids []types.TransactionId) {
	tq.Lock()
	var locks []*sync.Mutex
	for _, id := range txids {
		lock, ok := tq.txlocks[id]
		if !ok {
			lock = &sync.Mutex{}
			lock.Lock()
			tq.txlocks[id] = lock
		} else {
			locks = append(locks, lock)
		}

	}
	tq.Unlock()

	for _, lk := range locks {
		lk.Lock()
	}

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
			missingInPool = append(missingInPool, t)
		}
	}
	//look in db
	dbTxs, missing := tq.GetTransactions(missingInPool)
	return unprocessedTxs, dbTxs, missing
}

//todo minimize code duplication
//returns atxs out of atxids that are not in the local database
func (aq *atxQueue) Handle(atxIds []types.AtxId) ([]*types.ActivationTx, error) {

	aq.lockAtxs(atxIds)
	_, _, missing := aq.checkLocalAtxs(atxIds)
	output := aq.addToQueue(missing)
	aq.unlockAtxs(atxIds)

	if success := <-output; !success {
		return nil, errors.New(fmt.Sprintf("could not fetch all atxs"))
	}

	unprocessedAtxs, processedAtxs, missing := aq.checkLocalAtxs(atxIds)
	if len(missing) > 0 {
		return nil, errors.New("something got fudged2")
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

func (aq *atxQueue) unlockAtxs(atxIds []types.AtxId) {
	aq.Lock()
	var locks []*sync.Mutex
	for _, id := range atxIds {
		locks = append(locks, aq.atxlocks[id])
		delete(aq.atxlocks, id)
	}
	aq.Unlock()

	for _, lock := range locks {
		lock.Unlock()
	}

}

func (aq *atxQueue) lockAtxs(atxIds []types.AtxId) {
	aq.Lock()
	var locks []*sync.Mutex
	for _, id := range atxIds {
		lock, ok := aq.atxlocks[id]
		if !ok {
			lock = &sync.Mutex{}
			lock.Lock()
			aq.atxlocks[id] = lock
		} else {
			locks = append(locks, lock)
		}
	}
	aq.Unlock()

	for _, lock := range locks {
		lock.Unlock()
	}
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
			aq.Warning("atx %v not in atx pool", id.ShortId())
			missingInPool = append(missingInPool, id)
		}
	}
	//look in db
	dbAtxs, missing := aq.GetATXs(missingInPool)

	return unprocessedAtxs, dbAtxs, missing
}

func TxReqFactoryV2() RequestFactoryV2 {
	return func(s *server.MessageServer, peer p2p.Peer, id interface{}) (chan interface{}, error) {
		ch := make(chan interface{}, 1)
		foo := func(msg []byte) {
			defer close(ch)
			if len(msg) == 0 {
				s.Warning("peer responded with nil to txs request ", peer)
				return
			}
			var tx []types.SerializableSignedTransaction
			err := types.BytesToInterface(msg, &tx)
			if err != nil {
				s.Error("could not unmarshal tx data %v", err)
				return
			}
			ch <- tx
		}

		bts, err := types.InterfaceToBytes(id) //todo send multiple ids
		if err != nil {
			return nil, err
		}

		if err := s.SendRequest(TX, bts, peer, foo); err != nil {
			return nil, err
		}
		return ch, nil
	}
}

func ATxReqFactoryV2() RequestFactoryV2 {
	return func(s *server.MessageServer, peer p2p.Peer, ids interface{}) (chan interface{}, error) {
		ch := make(chan interface{}, 1)
		foo := func(msg []byte) {
			s.Info("Handle atx response ")
			defer close(ch)
			if len(msg) == 0 {
				s.Warning("peer responded with nil to atxs request ", peer)
				return
			}
			var tx []types.ActivationTx
			err := types.BytesToInterface(msg, &tx)
			if err != nil {
				s.Error("could not unmarshal tx data %v", err)
				return
			}

			ch <- tx
		}

		bts, err := types.InterfaceToBytes(ids) //todo send multiple ids
		if err != nil {
			return nil, err
		}
		if err := s.SendRequest(ATX, bts, peer, foo); err != nil {
			return nil, err
		}

		return ch, nil
	}
}

func NewFetchWorker(s *Syncer, count int, reqFactory RequestFactoryV2, idsChan chan interface{}) worker {
	output := make(chan interface{}, count)
	acount := int32(count)
	mu := &sync.Once{}
	workFunc := func() {
		for ids := range idsChan {
			retrived := false
		next:
			for _, p := range s.GetPeers() {
				peer := p
				s.Info("send fetch request to Peer: %v", peer.String())
				ch, _ := reqFactory(s.MessageServer, peer, ids)
				timeout := time.After(s.RequestTimeout)
				select {
				case <-timeout:
					s.Error("fetch request to %v timed out", peer.String())
				case v := <-ch:
					//chan not closed
					if v != nil {
						retrived = true
						s.Info("Peer: %v responded to  fetch request ", peer.String())
						output <- fetchJob{ids: ids, items: v}
						s.Info("done")
						break next
					}
				}
			}
			if !retrived {
				output <- fetchJob{ids: ids, items: nil}
			}
		}
	}

	return worker{Log: s.Log, Once: mu, workCount: &acount, output: output, work: workFunc}
}
