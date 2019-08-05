package sync

import (
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/types"
	"sync"
	"time"
)

const HandleTimeOut = 3 * time.Second

type fetchJob struct {
	items interface{}
	ids   interface{}
	jobID int
}

type RequestFactoryV2 func(s *server.MessageServer, peer p2p.Peer, id interface{}) (chan interface{}, error)

//todo make the queue generic
type txQueue struct {
	log.Log
	Syncer
	sync.Mutex
	queue        chan []types.TransactionId
	dependencies map[types.TransactionId][]func(res bool) error
	jobToIds     map[types.TransactionId][]func(res bool) error
}

type atxQueue struct {
	log.Log
	Syncer
	sync.Mutex
	queue        chan []types.AtxId
	dependencies map[types.AtxId][]func(res bool) error
}

func (tq *txQueue) work(s *Syncer) error {
	output := s.fetchWithFactory(NewNeighborhoodWorker(s, tq.Concurrency, TxReqFactoryV2(tq.queue)))
	for out := range output {
		txs, ok := out.(fetchJob)
		if !ok || txs.items == nil {
			tq.updateDependencies(txs.ids.([]types.TransactionId), false)
			tq.Error(fmt.Sprintf("could not retrieve a block in view "))
			continue
		}

		//todo invalidate and callbacks

		tq.Info("next batch")
	}

	return nil
}

func (vq *txQueue) updateDependencies(txs []types.TransactionId, valid bool) {
	//todo update dependencies
}

func (aq *atxQueue) work(s *Syncer) error {

	output := s.fetchWithFactory(NewNeighborhoodWorker(s, aq.Concurrency, ATxReqFactoryV2(aq.queue)))
	for out := range output {
		txs, ok := out.(fetchJob)
		if !ok || txs.items == nil {
			aq.updateDependencies(txs.ids.([]types.AtxId), false)
			aq.Error(fmt.Sprintf("could not retrieve a block in view "))
			continue
		}

		//todo invalidate and callbacks
		aq.Info("next batch")
	}

	return nil
}

func (vq *atxQueue) updateDependencies(txs []types.AtxId, valid bool) {
	//todo update dependencies
}

//todo chan or callback ?
func (tq *txQueue) addToPending(id []types.TransactionId) chan []types.SerializableSignedTransaction {
	//todo implement
	//todo invalidate already pending

	return nil
}

//todo chan or callback ?
func (atxQ *atxQueue) addToPending(id []types.AtxId) chan []types.ActivationTx {
	//todo implement
	//todo invalidate already pending

	return nil
}

//todo minimize code duplication
//returns txs out of txids that are not in the local database
func (tq *txQueue) handle(txids []types.TransactionId) ([]*types.AddressableSignedTransaction, error) {

	//todo need to synchronize this somehow
	unprocessedTxs, dbTxs, missing := tq.CheckLocalTxs(txids)

	output := tq.addToPending(missing)

	timeout := time.NewTimer(HandleTimeOut)

	select {
	case a := <-output:
		for _, tx := range a {
			txid := types.GetTransactionId(&tx)
			ast, err := tq.validateAndBuildTx(&tx)
			if err != nil {
				tq.Warning("tx %v not valid %v", hex.EncodeToString(txid[:]), err)
				continue
			}
			unprocessedTxs[txid] = ast

		}
	case <-timeout.C:
		return nil, errors.New("something got fudged")
	}

	txs := make([]*types.AddressableSignedTransaction, 0, len(txids))
	for _, id := range txids {
		if tx, ok := unprocessedTxs[id]; ok {
			txs = append(txs, tx)
		} else if _, ok := dbTxs[id]; ok {
			continue
		} else {
			return nil, errors.New(fmt.Sprintf("could not fetch tx %v", hex.EncodeToString(id[:])))
		}
	}

	return txs, nil
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

//todo minimize code duplication
//returns atxs out of atxids that are not in the local database
func (atxQ *atxQueue) handle(atxIds []types.AtxId) ([]*types.ActivationTx, error) {

	//todo need to synchronize this somehow
	unprocessedAtxs, dbAtxs, missing := atxQ.CheckLocalAtxs(atxIds)

	//todo return callback
	output := atxQ.addToPending(missing)

	timeout := time.NewTimer(HandleTimeOut)

	select {
	case a := <-output:
		for _, atx := range a {
			//todo get rid of this, dont use pointers here ?
			tmp := &atx
			unprocessedAtxs[atx.Id()] = tmp
		}
	case <-timeout.C:
		return nil, errors.New("something got fudged")
	}

	atxs := make([]*types.ActivationTx, 0, len(atxIds))
	for _, id := range atxIds {
		if tx, ok := unprocessedAtxs[id]; ok {
			atxs = append(atxs, tx)
		} else if _, ok := dbAtxs[id]; ok {
			continue
		} else {
			return nil, errors.New(fmt.Sprintf("could not fetch atx %v", id.ShortId()))
		}
	}

	return atxs, nil
}

func (atxQ *atxQueue) CheckLocalAtxs(atxIds []types.AtxId) (map[types.AtxId]*types.ActivationTx, map[types.AtxId]*types.ActivationTx, []types.AtxId) {
	//look in pool
	unprocessedAtxs := make(map[types.AtxId]*types.ActivationTx, len(atxIds))
	missingInPool := make([]types.AtxId, 0, len(atxIds))
	for _, t := range atxIds {
		id := t
		if x, err := atxQ.atxpool.Get(id); err == nil {
			atx := x

			//todo need to get rid of this somehow
			if atx.Nipst == nil {
				atxQ.Warning("atx %v nipst not found ", id.ShortId())
				missingInPool = append(missingInPool, id)
				continue
			}

			atxQ.Debug("found atx, %v in atx pool", id.ShortId())
			unprocessedAtxs[id] = &atx
		} else {
			atxQ.Warning("atx %v not in atx pool", id.ShortId())
			missingInPool = append(missingInPool, id)
		}
	}
	//look in db
	dbAtxs, missing := atxQ.GetATXs(missingInPool)

	return unprocessedAtxs, dbAtxs, missing
}

func TxReqFactoryV2(ids chan []types.TransactionId) RequestFactory {
	return func(s *server.MessageServer, peer p2p.Peer) (chan interface{}, error) {
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

		bts, err := types.InterfaceToBytes(ids)
		if err != nil {
			return nil, err
		}

		if err := s.SendRequest(TX, bts, peer, foo); err != nil {
			return nil, err
		}
		return ch, nil
	}
}

func ATxReqFactoryV2(ids chan []types.AtxId) RequestFactory {
	return func(s *server.MessageServer, peer p2p.Peer) (chan interface{}, error) {
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

		bts, err := types.InterfaceToBytes(ids)
		if err != nil {
			return nil, err
		}
		if err := s.SendRequest(ATX, bts, peer, foo); err != nil {
			return nil, err
		}

		return ch, nil
	}
}

func NewFetchWorker(s *Syncer, count int, reqFactory RequestFactoryV2, idsChan chan []interface{}) worker {
	output := make(chan interface{}, count)
	acount := int32(count)
	mu := &sync.Once{}
	workFunc := func() {
		for ids := range idsChan {
			retrived := false
		next:
			for _, p := range s.GetPeers() {
				peer := p
				s.Info("send %v block request to Peer: %v", ids, peer)
				ch, _ := reqFactory(s.MessageServer, peer, ids)
				timeout := time.After(s.RequestTimeout)
				select {
				case <-timeout:
					s.Error("block %v request to %v timed out", ids, peer)
				case v := <-ch:
					//chan not closed
					if v != nil {
						retrived = true
						s.Info("Peer: %v responded to %v block request ", peer, ids)
						output <- fetchJob{ids: ids, items: v}
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
