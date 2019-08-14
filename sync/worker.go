package sync

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/types"
	"sync"
	"sync/atomic"
	"time"
)

type RequestFactory func(s *server.MessageServer, peer p2p.Peer) (chan interface{}, error)
type BlockRequestFactory func(s *server.MessageServer, peer p2p.Peer, id types.BlockID) (chan interface{}, error)

type worker struct {
	*sync.Once
	sync.WaitGroup
	work      func()
	workCount *int32
	output    chan interface{}
	log.Log
}

func (w *worker) Work() {
	w.Debug("worker work")
	w.work()
	atomic.AddInt32(w.workCount, -1)
	w.Debug("worker done")
	if atomic.LoadInt32(w.workCount) == 0 { //close once everyone is finished
		w.Debug("worker teardown")
		w.Do(func() { close(w.output) })
	}
}

func (w *worker) Clone() *worker {
	return &worker{Log: w.Log, Once: w.Once, workCount: w.workCount, output: w.output, work: w.work}
}

func NewPeersWorker(s *Syncer, peers []p2p.Peer, mu *sync.Once, reqFactory RequestFactory) (worker, chan interface{}) {
	count := int32(1)
	numOfpeers := len(peers)
	output := make(chan interface{}, numOfpeers)
	peerfuncs := []func(){}
	wg := sync.WaitGroup{}
	wg.Add(len(peers))

	for _, p := range peers {
		peer := p
		peerFunc := func() {
			defer wg.Done()
			s.Info("send request Peer: %v", peer)
			ch, err := reqFactory(s.MessageServer, peer)
			if err != nil {
				s.Error("request failed, ", err)
				return
			}

			timeout := time.After(s.RequestTimeout)
			select {
			case <-timeout:
				s.Error("request to %v timed out", peer)
				return
			case v := <-ch:
				if v != nil {
					s.Info("Peer: %v responded", peer)
					output <- v
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

	worker := worker{Log: s.Log.WithName("peersWrkr"), Once: mu, workCount: &count, output: output, work: wrkFunc}

	return worker, output

}

func NewNeighborhoodWorker(s *Syncer, count int, reqFactory RequestFactory) worker {
	output := make(chan interface{}, count)
	acount := int32(count)
	mu := &sync.Once{}
	workFunc := func() {
		peers := s.GetPeers()
		for _, p := range peers {
			peer := p
			s.Info("send request Peer: %v", peer)
			ch, _ := reqFactory(s.MessageServer, peer)
			timeout := time.After(s.RequestTimeout)
			select {
			case <-timeout:
				s.Error("request to %v timed out", peer)
			case v := <-ch:
				if v != nil {
					s.Info("Peer: %v responded ", peer)
					s.Debug("Peer: %v response was  %v", v)
					output <- v
					return
				}
			}
		}
	}

	return worker{Log: s.Log, Once: mu, workCount: &acount, output: output, work: workFunc}

}

func NewBlockWorker(s *Syncer, count int, reqFactory BlockRequestFactory, ids chan types.BlockID) worker {
	output := make(chan interface{}, count)
	acount := int32(count)
	mu := &sync.Once{}
	workFunc := func() {
		for id := range ids {
			retrived := false
		next:
			for _, p := range s.GetPeers() {
				peer := p
				s.Info("send %v block request to Peer: %v", id, peer)
				ch, _ := reqFactory(s.MessageServer, peer, id)
				timeout := time.After(s.RequestTimeout)
				select {
				case <-timeout:
					s.Error("block %v request to %v timed out", id, peer)
				case v := <-ch:
					if i, ok := v.(*types.Block); ok {
						retrived = true
						s.Info("Peer: %v responded to %v block request ", peer, i.ID())
						output <- v
						break next
					}
				}
			}
			if !retrived {
				// for now panic if the timeout is not enough for all peers
				s.Panic("Failed to retrieve block %v from all peers", id)
				output <- nil
			}
		}
	}

	return worker{Log: s.Log, Once: mu, workCount: &acount, output: output, work: workFunc}
}
