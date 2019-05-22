package sync

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"sync"
	"sync/atomic"
	"time"
)

type RequestFactory func(s *server.MessageServer, peer p2p.Peer) (chan interface{}, error)

type worker struct {
	*sync.Once
	sync.WaitGroup
	work      func()
	workCount *int32
	output    chan interface{}
	log.Log
}

func (w *worker) Work() {
	w.Info("worker work")
	w.work()
	atomic.AddInt32(w.workCount, -1)
	w.Info("worker done")
	if atomic.LoadInt32(w.workCount) == 0 { //close once everyone is finished
		w.Info("worker teardown")
		w.Do(func() { close(w.output) })
	}
}

func NewPeerWorker(s *Syncer, reqFactory RequestFactory) (worker, chan interface{}) {
	count := int32(1)
	peers := s.GetPeers()
	mu := &sync.Once{}
	numOfpeers := len(peers)
	output := make(chan interface{}, numOfpeers)
	peerfuncs := []func(){}
	wg := sync.WaitGroup{}
	wg.Add(len(peers))

	for _, p := range peers {
		peer := p
		s.Info("creat func peer: %v", peer)
		peerFunc := func() {
			defer wg.Done()
			s.Info("send request Peer: %v", peer)
			ch, err := reqFactory(s.MessageServer, peer)
			if err != nil {
				s.Error("RequestFactory failed, ", err)
				return
			}

			timeout := time.After(s.RequestTimeout)
			select {
			case <-timeout:
				s.Error("request to %v timed out", peer)
				return
			case v := <-ch:
				if v != nil {
					s.Debug("Peer: %v responded", peer)
					output <- v
				} else {
					s.Error("peer %v responded with nil", peer)
				}
			}
		}

		peerfuncs = append(peerfuncs, peerFunc)
	}

	wrkFunc := func() {
		for _, p := range peerfuncs {
			go p()
		}
		wg.Wait()
	}

	worker := worker{Log: s.Log, Once: mu, workCount: &count, output: output, work: wrkFunc}

	return worker, output

}

func NewNeighborhoodWorker(s *Syncer,
	mu *sync.Once,
	count *int32,
	output chan interface{},
	reqFactory RequestFactory) worker {
	workFunc := func() {
		for _, p := range s.GetPeers() {
			peer := p
			s.Info("send request Peer: %v", peer)
			ch, _ := reqFactory(s.MessageServer, peer)
			timeout := time.After(s.RequestTimeout)
			select {
			case <-timeout:
				s.Error("request to %v timed out", peer)
			case v := <-ch:
				if v != nil {
					output <- v
					return
				}
				s.Error("peer %v responded with nil", peer)
			}
		}
	}

	return worker{Log: s.Log, Once: mu, workCount: count, output: output, work: workFunc}

}
