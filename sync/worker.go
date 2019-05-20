package sync

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/types"
	"sync"
	"sync/atomic"
	"time"
)

type RequestFactory func(s *server.MessageServer, peer p2p.Peer) (chan interface{}, error)

type worker struct {
	*sync.Once
	sync.WaitGroup
	output chan interface{}
	count  *int32
	action func()
	log.Log
}

func (w *worker) Work() {
	w.Info("worker work")
	w.action()
	atomic.AddInt32(w.count, -1)
	w.Info("worker done")
	if atomic.LoadInt32(w.count) == 0 { //close once everyone is finished
		w.Info("worker teardown")
		w.Do(func() { close(w.output) })
	}
}

func NewWorker(s *server.MessageServer,
	requestTimeout time.Duration,
	peer p2p.Peer,
	mu *sync.Once,
	count *int32,
	output chan interface{},
	reqFactory RequestFactory) worker {
	workFunc := func() {
		log.Debug("send request Peer: %v", peer)
		ch, _ := reqFactory(s, peer)
		timeout := time.After(requestTimeout)
		select {
		case <-timeout:
			log.Error("layer ids request to %v timed out", peer)
			return
		case v := <-ch:
			if v != nil {
				output <- v
			} else {
				log.Error("peer %v responded with nil", peer)
			}
		}
	}

	return worker{
		Log:    s.Log,
		Once:   mu,
		count:  count,
		output: output,
		action: workFunc,
	}

}

func NewNeighborhoodWorker(s *server.MessageServer,
	requestTimeout time.Duration,
	peers []p2p.Peer,
	mu *sync.Once,
	count *int32,
	output chan interface{},
	reqFactory RequestFactory) worker {
	workFunc := func() {
		for _, peer := range peers {
			log.Debug("send request Peer: %v", peer)
			ch, _ := reqFactory(s, peer)
			timeout := time.After(requestTimeout)
			select {
			case <-timeout:
				log.Error("request to %v timed out", peer)
			case v := <-ch:
				if v != nil {
					output <- v
					return
				} else {
					log.Error("peer %v responded with nil", peer)
				}
			}
		}
	}

	return worker{
		Log:    s.Log,
		Once:   mu,
		count:  count,
		output: output,
		action: workFunc,
	}

}

func LayerIdsReqFactory(lyr types.LayerID) RequestFactory {
	return func(s *server.MessageServer, peer p2p.Peer) (chan interface{}, error) {
		ch, foo := layerBlockIDsRequest()
		if err := s.SendRequest(LAYER_IDS, lyr.ToBytes(), peer, foo); err != nil {
			log.Error("could not get layer ", lyr, " hash from peer ", peer)
			return nil, errors.New("error ")
		}
		return ch, nil
	}
}

func HashReqFactory(lyr types.LayerID) RequestFactory {
	return func(s *server.MessageServer, peer p2p.Peer) (chan interface{}, error) {
		log.Info("send layer hash request Peer: %v layer: %v", peer, lyr)
		ch, foo := layerHashRequest(peer)
		if err := s.SendRequest(LAYER_HASH, lyr.ToBytes(), peer, foo); err != nil {
			s.Error("could not get layer ", lyr, " hash from peer ", peer)
			return nil, errors.New("error ")
		}

		return ch, nil
	}

}

//todo handle blocks in retry queue
func BlocReqFactory(id types.BlockID) RequestFactory {
	return func(s *server.MessageServer, peer p2p.Peer) (chan interface{}, error) {
		log.Info("send block request Peer: %v layer: %v", peer, id)
		ch, foo := miniBlockRequest()
		if err := s.SendRequest(BLOCK, id.ToBytes(), peer, foo); err != nil {
			s.Error("could not get block ", id, " hash from peer ", peer)
			return nil, errors.New("error ")
		}

		return ch, nil
	}
}

//todo batch requests
func TxReqFactory(id types.TransactionId) RequestFactory {
	return func(s *server.MessageServer, peer p2p.Peer) (chan interface{}, error) {
		log.Info("send block request Peer: %v layer: %v", peer, id)
		ch, foo := txRequest()
		if err := s.SendRequest(TX, id[:], peer, foo); err != nil {
			s.Error("could not get transaction ", id, "  from peer ", peer)
			return nil, errors.New("error ")
		}

		return ch, nil
	}
}
