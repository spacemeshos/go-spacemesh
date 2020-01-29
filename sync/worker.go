package sync

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"sync"
	"sync/atomic"
	"time"
)

type RequestFactory func(s WorkerInfra, peer p2p.Peer) (chan interface{}, error)
type BatchRequestFactory func(s WorkerInfra, peer p2p.Peer, id []types.Hash32) (chan []Item, error)

type WorkerInfra interface {
	GetPeers() []p2p.Peer
	SendRequest(msgType server.MessageType, payload []byte, address p2pcrypto.PublicKey, resHandler func(msg []byte)) error
	GetTimeout() time.Duration
	GetExit() chan struct{}
	log.Logger
}

type workerInfra struct {
	RequestTimeout time.Duration
	*server.MessageServer
	p2p.Peers
	exit chan struct{}
}

func (ms *workerInfra) Close() {
	ms.MessageServer.Close()
	ms.Peers.Close()
}

func (ms *workerInfra) GetTimeout() time.Duration {
	return ms.RequestTimeout
}

func (ms *workerInfra) GetExit() chan struct{} {
	return ms.exit
}

type worker struct {
	*sync.Once
	work      func()
	workCount *int32
	output    chan interface{}
	log.Logger
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
	return &worker{Logger: w.Logger, Once: w.Once, workCount: w.workCount, output: w.output, work: w.work}
}

func NewPeersWorker(s WorkerInfra, peers []p2p.Peer, mu *sync.Once, reqFactory RequestFactory) worker {
	count := int32(1)
	numOfpeers := len(peers)
	output := make(chan interface{}, numOfpeers)
	peerfuncs := []func(){}
	wg := sync.WaitGroup{}
	wg.Add(len(peers))
	lg := s.WithName("peersWrkr")
	for _, p := range peers {
		peer := p
		peerFunc := func() {
			defer wg.Done()
			lg.Info("send request Peer: %v", peer)
			ch, err := reqFactory(s, peer)
			if err != nil {
				s.Error("request failed, ", err)
				return
			}

			timeout := time.After(s.GetTimeout())
			select {
			case <-s.GetExit():
				lg.Info("worker received interrupt")
				return
			case <-timeout:
				lg.Error("request to %v timed out", peer)
				return
			case v := <-ch:
				if v != nil {
					lg.Info("Peer: %v responded", peer)
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

	return worker{Logger: lg, Once: mu, workCount: &count, output: output, work: wrkFunc}

}

func NewNeighborhoodWorker(s WorkerInfra, count int, reqFactory RequestFactory) worker {
	output := make(chan interface{}, count)
	acount := int32(count)
	mu := &sync.Once{}
	lg := s.WithName("HoodWrker")
	workFunc := func() {
		peers := s.GetPeers()
		for _, p := range peers {
			peer := p
			lg.Info("send request Peer: %v", peer)
			ch, _ := reqFactory(s, peer)
			timeout := time.After(s.GetTimeout())
			select {
			case <-s.GetExit():
				lg.Info("worker received interrupt")
				return
			case <-timeout:
				lg.Error("request to %v timed out", peer)
			case v := <-ch:
				if v != nil {
					lg.Info("Peer: %v responded ", peer)
					lg.Debug("Peer: %v response was  %v", v)
					output <- v
					return
				}
			}
		}
	}

	return worker{Logger: lg, Once: mu, workCount: &acount, output: output, work: workFunc}

}

func NewFetchWorker(s WorkerInfra, count int, reqFactory BatchRequestFactory, idsChan chan []types.Hash32, name string) worker {
	output := make(chan interface{}, 10)
	acount := int32(count)
	mu := &sync.Once{}
	lg := s.WithName("FetchWrker")
	workFunc := func() {
		for ids := range idsChan {
			if ids == nil {
				lg.Info("close fetch worker ")
				return
			}
			leftToFetch := toMap(ids)
			var fetched []Item
		next:
			for _, p := range s.GetPeers() {
				peer := p
				remainingItems := toSlice(leftToFetch)
				idsStr := concatShortIds(remainingItems)
				lg.Info("send %s fetch request to Peer: %v ids: %v", name, peer.String(), idsStr)
				ch, _ := reqFactory(s, peer, remainingItems)
				timeout := time.After(s.GetTimeout())
				select {
				case <-s.GetExit():
					lg.Info("worker received interrupt")
					return
				case <-timeout:
					lg.Error("fetch %s request to %v on %v timed out %s", name, peer.String(), idsStr)
				case v := <-ch:
					if v != nil && len(v) > 0 {
						lg.Info("Peer: %v responded to fetch %s request %s", peer.String(), name, idsStr)
						// 	remove ids from leftToFetch add to fetched
						for _, itm := range v {
							fetched = append(fetched, itm)
							delete(leftToFetch, itm.Hash32())
						}

						//if no more left to fetch
						if len(leftToFetch) == 0 {
							break next
						}

					}
					lg.Info("next peer")
				}
			}
			//finished pass results to chan
			output <- fetchJob{ids: ids, items: fetched}
		}
	}
	return worker{Logger: lg, Once: mu, workCount: &acount, output: output, work: workFunc}
}

func toMap(ids []types.Hash32) map[types.Hash32]struct{} {
	mp := make(map[types.Hash32]struct{}, len(ids))
	for _, id := range ids {
		mp[id] = struct{}{}
	}
	return mp
}

func toSlice(mp map[types.Hash32]struct{}) []types.Hash32 {
	sl := make([]types.Hash32, 0, len(mp))
	for id := range mp {
		sl = append(sl, id)
	}
	return sl
}
