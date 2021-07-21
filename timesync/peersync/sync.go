package peersync

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/peers"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
)

const (
	DefaultMaxErrorCount = 10
)

var (
	ErrPeersNotSynced = errors.New("timesync: peers are not time synced")
)

type Time interface {
	Now() time.Time
}

type systemTime struct{}

func (s systemTime) Now() time.Time {
	return time.Now()
}

type MessageServer interface {
	SendRequest(context.Context, server.MessageType, []byte, peers.Peer, func([]byte), func(error)) error
	RegisterBytesMsgHandler(server.MessageType, func(ctx context.Context, b []byte) []byte)
}

type Request struct {
	ID uint64
}

type DirectRequest struct {
	Peer peers.Peer
	Request
}

type Response struct {
	ID               uint64
	ReceiveTimestamp uint64
	SendTimestamp    uint64
}

type SyncOption func(*Sync)

func WithTime(t Time) SyncOption {
	return func(s *Sync) {
		s.time = t
	}
}

func WithContext(ctx context.Context) SyncOption {
	return func(s *Sync) {
		s.ctx = ctx
	}
}

func WithLog(lg log.Log) SyncOption {
	return func(s *Sync) {
		s.log = lg
	}
}

func New(srv MessageServer, opts ...SyncOption) *Sync {
	sync := &Sync{
		log:       log.NewNop(),
		ctx:       context.Background(),
		time:      systemTime{},
		maxErrCnt: DefaultMaxErrorCount,
	}
	for _, opt := range opts {
		opt(sync)
	}
	sync.ctx, sync.cancel = context.WithCancel(sync.ctx)
	srv.RegisterBytesMsgHandler(server.RequestTimeSync, sync.requestHandler)
	return sync
}

type Sync struct {
	errCnt         uint32
	maxErrCnt      uint32
	log            log.Log
	maxClockOffset time.Duration

	wg      sync.WaitGroup
	once    sync.Once
	reactor Reactor
	srv     MessageServer
	ctx     context.Context
	cancel  func()
	errors  chan error
	time    Time
}

func (s *Sync) requestHandler(ctx context.Context, buf []byte) []byte {
	var request Request
	if err := types.BytesToInterface(buf, &request); err != nil {
		return nil
	}
	resp := Response{
		ID:               request.ID,
		ReceiveTimestamp: uint64(time.Now().UnixNano()),
		SendTimestamp:    uint64(time.Now().UnixNano()),
	}
	buf, err := types.InterfaceToBytes(&resp)
	if err != nil {
		s.log.Panic("failed to encode timesync response", log.Binary("response", buf), log.Err(err))
	}
	return buf
}

func (s *Sync) responseHandler(buf []byte) {
	var response Response
	if err := types.BytesToInterface(buf, &response); err != nil {
		s.log.Debug("failed to decode timeync response", log.Binary("response", buf), log.Err(err))
		return
	}
	select {
	case <-s.ctx.Done():
	case s.reactor.Responses() <- response:
	}
}

func (s *Sync) Start() {
	s.once.Do(func() {
		s.wg.Add(1)
		go func() {
			s.errors <- s.run()
			s.wg.Done()
		}()
		s.wg.Add(1)
		go func() {
			s.errors <- s.reactor.Run()
			s.wg.Done()
		}()
		go func() {
			s.wg.Wait()
			close(s.errors)
		}()
	})
}

func (s *Sync) run() error {
	for {
		select {
		case <-s.ctx.Done():
			return s.ctx.Err()
		case requests := <-s.reactor.Requests():
			for _, req := range requests {
				buf, err := types.InterfaceToBytes(&req.Request)
				if err != nil {
					panic(err)
				}
				if err := s.srv.SendRequest(s.ctx, server.RequestTimeSync, buf, req.Peer, s.responseHandler, func(error) {}); err != nil {
					s.log.With().Error("can't send timesync request", req.Peer, log.Err(err))
				}
			}
		case offset := <-s.reactor.Completed():
			if offset > s.maxClockOffset || (offset < 0 && offset < s.maxClockOffset) {
				s.log.With().Error("peers offset is larger than max allowed offset",
					log.Duration("offset", offset), log.Duration("max_offset", s.maxClockOffset))
				if atomic.AddUint32(&s.errCnt, 1) == s.maxErrCnt {
					return ErrPeersNotSynced
				}
			} else {
				atomic.StoreUint32(&s.errCnt, 0)
			}
		}
	}
}

func (s *Sync) Stop() {
	s.cancel()
}

func (s *Sync) Wait() error {
	var rst error
	for err := range s.errors {
		if !errors.Is(err, context.Canceled) {
			s.cancel()
			rst = err
		}
	}
	return rst
}
