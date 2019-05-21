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
	output chan interface{}
	count  *int32
	action func()
	log.Log
}

func (w *worker) Work() {
	w.Debug("worker work")
	w.action()
	atomic.AddInt32(w.count, -1)
	w.Debug("worker done")
	if atomic.LoadInt32(w.count) == 0 { //close once everyone is finished
		w.Debug("worker teardown")
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
		s.Debug("send request Peer: %v", peer)
		ch, _ := reqFactory(s, peer)
		timeout := time.After(requestTimeout)
		select {
		case <-timeout:
			s.Error("request to %v timed out", peer)
			return
		case v := <-ch:
			if v != nil {
				output <- v
			} else {
				s.Error("peer %v responded with nil", peer)
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
