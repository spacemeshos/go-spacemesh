package sync

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	p2ppeers "github.com/spacemeshos/go-spacemesh/p2p/peers"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
)

type requestFactory func(ctx context.Context, com networker, peer p2ppeers.Peer) (chan interface{}, error)
type batchRequestFactory func(ctx context.Context, com networker, peer p2ppeers.Peer, id []types.Hash32) (chan []item, error)

type networker interface {
	GetPeers() []p2ppeers.Peer
	SendRequest(ctx context.Context, msgType server.MessageType, payload []byte, address p2pcrypto.PublicKey, resHandler func(msg []byte)) error
	GetTimeout() time.Duration
	GetExit() chan struct{}
	log.Logger
}

type peers interface {
	GetPeers() []p2ppeers.Peer
	Close()
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

func newPeersWorker(ctx context.Context, s networker, peers []p2ppeers.Peer, mu *sync.Once, reqFactory requestFactory) worker {
	count := int32(1)
	numOfpeers := len(peers)
	output := make(chan interface{}, numOfpeers)
	peerfuncs := []func(){}
	wg := sync.WaitGroup{}
	wg.Add(len(peers))
	lg := s.WithName("peersWrkr")
	for _, p := range peers {
		peer := p
		lg := lg.WithFields(log.FieldNamed("peer_id", peer))
		peerFunc := func() {
			defer wg.Done()
			lg.Debug("send request")
			ch, err := reqFactory(ctx, s, peer)
			if err != nil {
				lg.With().Error("request failed", log.Err(err))
				return
			}

			timeout := time.After(s.GetTimeout())
			select {
			case <-s.GetExit():
				lg.Debug("worker received interrupt")
				return
			case <-timeout:
				lg.Error("request timed out")
				return
			case v := <-ch:
				if v != nil {
					lg.Debug("peer responded")
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

func newNeighborhoodWorker(ctx context.Context, s networker, count int, reqFactory requestFactory) worker {
	output := make(chan interface{}, count)
	acount := int32(count)
	mu := &sync.Once{}
	lg := s.WithName("HoodWrker").WithContext(ctx)
	workFunc := func() {
		peers := s.GetPeers()
		for _, p := range peers {
			peer := p
			lg := lg.WithFields(log.FieldNamed("peer_id", peer))
			lg.Info("send request to peer")
			ch, _ := reqFactory(ctx, s, peer)
			timeout := time.After(s.GetTimeout())
			select {
			case <-s.GetExit():
				lg.Debug("worker received interrupt")
				return
			case <-timeout:
				lg.Error("request to peer timed out")
			case v := <-ch:
				if v != nil {
					lg.Info("peer responded")
					lg.With().Debug("peer response", log.String("response", fmt.Sprint(v)))
					output <- v
					return
				}
			}
		}
	}

	return worker{Logger: lg, Once: mu, workCount: &acount, output: output, work: workFunc}

}

func newFetchWorker(ctx context.Context, s networker, count int, reqFactory batchRequestFactory, idsChan chan []types.Hash32, name string) worker {
	output := make(chan interface{}, 10)
	acount := int32(count)
	mu := &sync.Once{}
	lg := s.WithName("FetchWrker").WithContext(ctx)
	workFunc := func() {
		for ids := range idsChan {
			if ids == nil {
				lg.Info("close fetch worker")
				return
			}
			leftToFetch := toMap(ids)
			var fetched []item
		next:
			for _, p := range s.GetPeers() {
				peer := p
				lg := lg.WithFields(log.FieldNamed("peer_id", peer))
				remainingItems := toSlice(leftToFetch)
				idsStr := concatShortIds(remainingItems)
				lg.With().Info("send fetch request",
					log.String("type", name),
					log.String("ids", idsStr))
				ch, _ := reqFactory(ctx, s, peer, remainingItems)
				timeout := time.After(s.GetTimeout())
				select {
				case <-s.GetExit():
					lg.Debug("worker received interrupt")
					return
				case <-timeout:
					lg.With().Error("fetch request timed out",
						log.String("type", name),
						log.String("ids", idsStr))
				case v := <-ch:
					if v != nil && len(v) > 0 {
						lg.With().Info("peer responded to fetch request",
							log.String("type", name),
							log.String("ids", idsStr))
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
