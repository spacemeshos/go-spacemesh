package peersync

import (
	"context"
	"errors"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/peers"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
)

var (
	ErrTimesyncTimeout = errors.New("timesync: timeout")
)

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

func GetOffset(ctx context.Context, srv MessageServer, prs []peers.Peer) (time.Duration, error) {
	lg := log.NewNop()
	responses := make(chan Response, len(prs))

	for _, peer := range prs {
		srv.SendRequest(ctx, server.RequestTimeSync, []byte{}, peer, func(buf []byte) {
			var response Response
			if err := types.BytesToInterface(buf, &response); err != nil {
				lg.Debug("failed to decode timesync response", log.Binary("response", buf), log.Err(err))
				return
			}
			select {
			case <-ctx.Done():
			case responses <- response:
			}
		}, func(error) {})
	}

	round := Round{}
	for resp := range responses {
		round.AddResponse(resp)
	}
	if round.Ready() {
		return round.Offset(), nil
	}
	return 0, ErrTimesyncTimeout
}

func Run(ctx context.Context, srv MessageServer, peersCh <-chan []peers.Peer) {
	lg := log.NewNop()
	var (
		prs   []peers.Peer
		timer *time.Timer
	)
	// FIXME(dshulyak) save peers in some data structure and check using atomic operation
	// if they became less then required, subscribe and let peers subscriber to notify you
	select {
	case <-ctx.Done():
	case prs = <-peersCh:
	}
	for {
		rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		offset, err := GetOffset(rctx, srv, prs)
		cancel()
		var timeout time.Duration
		if err == nil {
			if offset > 10*time.Second {
				lg.With().Error("offset is too far off")
			}
			timeout = 60 * time.Minute
		} else {
			lg.With().Error("failed to fetch offset from peers")
			timeout = 5 * time.Second
		}
		if timer == nil {
			timer = time.NewTimer(timeout)
		} else {
			timer.Reset(timeout)
		}
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}
