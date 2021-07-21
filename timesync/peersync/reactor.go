package peersync

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/peers"
)

type Reactor struct {
	round  uint64
	offset int64

	log log.Log

	requiredResponseCount int
	roundInterval         time.Duration
	roundTimeout          time.Duration
	retryInterval         time.Duration

	ctx       context.Context
	requests  chan []DirectRequest
	responses chan Response // should be buffered with max number of peers
	peers     chan []peers.Peer
	completed chan time.Duration
}

func (r *Reactor) Responses() chan<- Response {
	return r.responses
}

func (r *Reactor) Peers() chan<- []peers.Peer {
	return r.peers
}

func (r *Reactor) Requests() <-chan []DirectRequest {
	return r.requests
}

func (r *Reactor) Completed() <-chan time.Duration {
	return r.completed
}

func (r *Reactor) Run() error {
	var (
		peers           []peers.Peer
		round           uint64
		state           *Round
		timer           *time.Timer
		timerChan       <-chan time.Time
		pendingRequests []DirectRequest
		activeRequests  chan []DirectRequest
	)
	for {
		select {
		case <-r.ctx.Done():
			r.log.Debug("terminating reactor on closed context")
			return r.ctx.Err()
		case peers = <-r.peers:
			r.log.Debug("received updated set of peers",
				log.Int("peers_count", len(peers)),
			)
			if timer == nil {
				timer = time.NewTimer(r.roundTimeout)
				timerChan = timer.C
			} else if state == nil {
				timer.Reset(r.roundTimeout)
			}
			if state == nil {
				r.log.Info("starting time synchronization round with peers",
					log.Int("peers_count", len(peers)),
					log.Uint64("round", round),
				)
				state = &Round{
					ID:        round,
					Timestamp: uint64(time.Now().UnixNano()),
				}
				round++
				pendingRequests = make([]DirectRequest, 0, len(peers))
				for _, peer := range peers {
					pendingRequests = append(pendingRequests, DirectRequest{
						Peer:    peer,
						Request: Request{ID: state.ID},
					})
				}
			}
		case <-timerChan:
			if state == nil {
				timer.Reset(r.roundTimeout)
				r.log.Info("starting time synchronization round with peers",
					log.Int("peers_count", len(peers)),
					log.Uint64("round", round),
				)
				state = &Round{
					ID:        round,
					Timestamp: uint64(time.Now().UnixNano()),
				}
				atomic.StoreUint64(&r.round, round)
				round++
				pendingRequests = make([]DirectRequest, 0, len(peers))
				for _, peer := range peers {
					pendingRequests = append(pendingRequests, DirectRequest{
						Peer:    peer,
						Request: Request{ID: state.ID},
					})
				}
			} else if state != nil {
				if state.Ready() {
					offset := state.Offset()
					atomic.StoreInt64(&r.offset, int64(offset))
					timer.Reset(r.roundInterval)
					r.log.Info("time synchronization round with peers finished",
						log.Duration("offset", offset),
						log.Uint64("round", state.ID),
					)
					select {
					case r.completed <- offset:
					case <-r.ctx.Done():
					}
				} else {
					r.log.Info("time synchronization round with peers failed on timeout",
						log.Uint64("round", state.ID),
					)
					timer.Reset(r.retryInterval)
				}
				state = nil
			}
		case response := <-r.responses:
			if state == nil {
				continue
			}
			state.AddResponse(response)
		case activeRequests <- pendingRequests:
			pendingRequests = nil
			activeRequests = nil
		}
		if pendingRequests != nil {
			activeRequests = r.requests
		}

		// when we got disconnected there is no sense in retrying a failed round or starting a new one.
		// wait until peers are not nil, and start the round from scratch.
		// FIXME(dshulyak) it is possible that the time will be wasted here
		if peers == nil && state == nil {
			timer.Stop()
			<-timer.C
		}
	}
}

func (r *Reactor) Round() uint64 {
	return atomic.LoadUint64(&r.round)
}

func (r *Reactor) Offset() time.Duration {
	return time.Duration(atomic.LoadInt64(&r.offset))
}

type timedResponse struct {
	response         Response
	receiveTimestamp uint64
}

type Round struct {
	ID                    uint64
	Timestamp             uint64
	RequiredResponseCount int
	responses             []timedResponse
}

func (r *Round) AddResponse(resp Response) {
	if resp.ID != r.ID {
		return
	}
	r.responses = append(r.responses,
		timedResponse{response: resp, receiveTimestamp: uint64(time.Now().UnixNano())})
}

func (r *Round) Ready() bool {
	return len(r.responses) >= r.RequiredResponseCount
}

func (r *Round) Offset() time.Duration {
	return 0
}
