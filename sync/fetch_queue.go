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

const HandleTimeOut = 3 * time.Second

type RequestFactoryV2 func(s *server.MessageServer, peer p2p.Peer, id interface{}) (chan interface{}, error)

func NewTxQueue(s *Syncer) *txQueue {
	//todo buffersize
	q := &txQueue{Syncer: s, queue: make(chan interface{}, 100), dependencies: make(map[types.TransactionId][]chan bool)}
	go q.work()
	return q
}

func NewAtxQueue(s *Syncer) *atxQueue {
	//todo buffersize
	q := &atxQueue{Syncer: s, queue: make(chan interface{}, 100), dependencies: make(map[types.AtxId][]chan bool)}
	go q.work()
	return q
}

type fetchJob struct {
	items interface{}
	ids   interface{}
	jobID int
}

//todo make the queue generic
type txQueue struct {
	*Syncer
	sync.Mutex
	queue        chan interface{} //types.TransactionId //todo make buffered
	dependencies map[types.TransactionId][]chan bool
}

type atxQueue struct {
	*Syncer
	sync.Mutex
	queue        chan interface{} //types.AtxId
	dependencies map[types.AtxId][]chan bool
}

//todo batches
func (tq *txQueue) work() error {
	output := tq.fetchWithFactory(NewFetchWorker(tq.Syncer, 1, TxReqFactoryV2(), tq.queue))
	for out := range output {
		txs, ok := out.(fetchJob)
		tq.Info("next id %v", txs)
		if !ok || txs.items == nil {
			tq.updateDependencie(txs.ids.(types.TransactionId), false) //todo hack add batches
			continue
		}
		tq.updateDependencie(txs.ids.(types.TransactionId), true) //todo hack add batches
		tq.Info("next batch")
	}

	return nil
}

func (tq *txQueue) updateDependencies(txs []types.TransactionId, valid bool) {
	for _, id := range txs {
		tq.updateDependencie(id, valid)
	}
}

func (tq *txQueue) updateDependencie(id types.TransactionId, valid bool) {
	tq.Info("done with %v !!!!!!!!!!!!!!!! %v", hex.EncodeToString(id[:]), valid)
	for _, dep := range tq.dependencies[id] {
		dep <- valid
	}

	delete(tq.dependencies, id)
}

//todo batches
func (aq *atxQueue) work() error {
	output := aq.fetchWithFactory(NewFetchWorker(aq.Syncer, 1, ATxReqFactoryV2(), aq.queue))
	for out := range output {
		txs, ok := out.(fetchJob)
		aq.Info("next id %v", txs)
		if !ok || txs.items == nil {
			aq.updateDependencie(txs.ids.(types.AtxId), false)
			continue
		}

		aq.updateDependencie(txs.ids.(types.AtxId), true)
		aq.Info("next batch")
	}

	return nil
}

func (tq *txQueue) addToPending(ids []types.TransactionId) chan struct{} {

	deps := make([]chan bool, 0, len(ids))
	for _, id := range ids {
		tq.queue <- id
		ch := make(chan bool, 1)
		deps = append(deps, ch)
		tq.dependencies[id] = append(tq.dependencies[id], ch)
	}

	doneChan := make(chan struct{})

	//fan in
	go func() {
		for _, c := range deps {
			<-c
		}
		close(doneChan)
	}()

	return doneChan
}

func (aq *atxQueue) updateDependencies(txs []types.AtxId, valid bool) {
	for _, id := range txs {
		aq.updateDependencie(id, valid)
	}
}

func (aq *atxQueue) updateDependencie(id types.AtxId, valid bool) {
	aq.Info("done with %v !!!!!!!!!!!!!!!! %v", id.ShortId(), valid)
	for _, dep := range aq.dependencies[id] {
		dep <- valid
	}

	delete(aq.dependencies, id)
}

func (aq *atxQueue) addToPending(ids []types.AtxId) chan struct{} {
	deps := make([]chan bool, 0, len(ids))
	for _, id := range ids {
		aq.queue <- id
		ch := make(chan bool, 1)
		deps = append(deps, ch)
		aq.dependencies[id] = append(aq.dependencies[id], ch)
	}

	doneChan := make(chan struct{})

	//fan in
	go func() {
		for _, c := range deps {
			<-c
		}
		close(doneChan)
	}()

	return doneChan
}

//todo minimize code duplication
//returns txs out of txids that are not in the local database
func (tq *txQueue) handle(txids []types.TransactionId) ([]*types.AddressableSignedTransaction, error) {

	//todo need to synchronize this somehow
	_, _, missing := tq.CheckLocalTxs(txids)
	output := tq.addToPending(missing)
	timeout := time.NewTimer(HandleTimeOut)

	select {
	case <-output:
		unprocessedTxs, processedTxs, missing := tq.CheckLocalTxs(txids)
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
				return nil, errors.New(fmt.Sprintf("could not fetch tx %v", hex.EncodeToString(id[:])))
			}
		}
		return txs, nil
	case <-timeout.C:
		return nil, errors.New("something got fudged")
	}
}

func (tq *txQueue) CheckLocalTxs(txids []types.TransactionId) (map[types.TransactionId]*types.AddressableSignedTransaction, map[types.TransactionId]*types.AddressableSignedTransaction, []types.TransactionId) {
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

func (tq *txQueue) invalidateCallbacks(i int) {

}

//todo minimize code duplication
//returns atxs out of atxids that are not in the local database
func (aq *atxQueue) handle(atxIds []types.AtxId) ([]*types.ActivationTx, error) {

	//todo need to synchronize this somehow
	_, _, missing := aq.CheckLocalAtxs(atxIds)
	output := aq.addToPending(missing)
	timeout := time.NewTimer(HandleTimeOut)

	select {
	case <-output:
		unprocessedAtxs, processedAtxs, missing := aq.CheckLocalAtxs(atxIds)
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
				return nil, errors.New(fmt.Sprintf("could not fetch tx %v", id.ShortId()))
			}
		}
		return atxs, nil
	case <-timeout.C:
		return nil, errors.New("something got fudged")
	}
}

func (aq *atxQueue) CheckLocalAtxs(atxIds []types.AtxId) (map[types.AtxId]*types.ActivationTx, map[types.AtxId]*types.ActivationTx, []types.AtxId) {
	//look in pool
	unprocessedAtxs := make(map[types.AtxId]*types.ActivationTx, len(atxIds))
	missingInPool := make([]types.AtxId, 0, len(atxIds))
	for _, t := range atxIds {
		id := t
		if x, err := aq.atxpool.Get(id); err == nil {
			atx := x

			//todo need to get rid of this somehow
			if atx.Nipst == nil {
				aq.Warning("atx %v nipst not found ", id.ShortId())
				missingInPool = append(missingInPool, id)
				continue
			}

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

func (aq *atxQueue) invalidateCallbacks(i int) {

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

		bts, err := types.InterfaceToBytes([]types.TransactionId{id.(types.TransactionId)}) //todo send multiple ids
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
			s.Info("handle atx response ")
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

		bts, err := types.InterfaceToBytes([]types.AtxId{ids.(types.AtxId)}) //todo send multiple ids
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
