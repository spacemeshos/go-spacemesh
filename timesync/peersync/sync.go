package peersync

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"golang.org/x/sync/errgroup"
)

var (
	ErrPeersNotSynced  = errors.New("timesync: peers are not time synced")
	ErrTimesyncTimeout = errors.New("timesync: timeout")
)

type Time interface {
	Now() time.Time
}

type systemTime struct{}

func (s systemTime) Now() time.Time {
	return time.Now()
}

type MessageServer interface {
	SendRequest(context.Context, server.MessageType, []byte, p2pcrypto.PublicKey, func([]byte), func(error)) error
	RegisterBytesMsgHandler(server.MessageType, func(ctx context.Context, b []byte) []byte)
}

type PeersProvider interface {
	SubscribePeerEvents() (conn, disc chan p2pcrypto.PublicKey)
}

type Request struct {
	ID uint64
}

type Response struct {
	ID        uint64
	Timestamp int64
}

var DefaultConfig = Config{
	RoundRetryInterval: 5 * time.Second,
	RoundInterval:      30 * time.Minute,
	RoundTimeout:       5 * time.Second,
	MaxClockOffset:     10 * time.Second,
	MaxOffsetErrors:    10,
	RequiredResponses:  3,
}

type Config struct {
	RoundRetryInterval time.Duration
	RoundInterval      time.Duration
	RoundTimeout       time.Duration
	MaxClockOffset     time.Duration
	MaxOffsetErrors    int
	RequiredResponses  int
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

func WithConfig(config Config) SyncOption {
	return func(s *Sync) {
		s.config = config
	}
}

func New(srv MessageServer, peers PeersProvider, opts ...SyncOption) *Sync {
	sync := &Sync{
		log:    log.NewNop(),
		ctx:    context.Background(),
		time:   systemTime{},
		config: DefaultConfig,
	}
	for _, opt := range opts {
		opt(sync)
	}

	sync.ctx, sync.cancel = context.WithCancel(sync.ctx)
	srv.RegisterBytesMsgHandler(server.RequestTimeSync, sync.requestHandler)
	sync.wg, sync.ctx = errgroup.WithContext(sync.ctx)

	added, expired := peers.SubscribePeerEvents()
	sync.peersWatcher = peersWatcher{
		added:    added,
		expired:  expired,
		requests: make(chan *waitPeersReq),
	}
	return sync
}

type Sync struct {
	errCnt uint32

	config       Config
	log          log.Log
	srv          MessageServer
	time         Time
	peersWatcher peersWatcher

	once   sync.Once
	wg     *errgroup.Group
	ctx    context.Context
	cancel func()
}

func (s *Sync) requestHandler(ctx context.Context, buf []byte) []byte {
	var request Request
	if err := types.BytesToInterface(buf, &request); err != nil {
		s.log.Debug("can't decode request", log.Binary("request", buf), log.Err(err))
		return nil
	}
	resp := Response{
		ID:        request.ID,
		Timestamp: s.time.Now().UnixNano(),
	}
	buf, err := types.InterfaceToBytes(&resp)
	if err != nil {
		s.log.Panic("can't encode response", log.Binary("response", buf), log.Err(err))
	}
	return buf
}

// Start background workers.
func (s *Sync) Start() {
	s.once.Do(func() {
		s.wg.Go(func() error {
			return s.run()
		})
		s.wg.Go(func() error {
			return s.peersWatcher.run(s.ctx)
		})
	})
}

// Stop background workers.
func (s *Sync) Stop() {
	s.cancel()
}

// Wait will return first error that is returned by background workers.
func (s *Sync) Wait() error {
	return s.wg.Wait()
}

func (s *Sync) run() error {
	var (
		timer *time.Timer
		round uint64
	)
	for {
		prs, err := s.peersWatcher.waitPeers(s.ctx, s.config.RequiredResponses)
		if err != nil {
			return err
		}
		s.log.With().Info("starting time sync round with peers",
			log.Uint64("round", round),
			log.Int("peers_count", len(prs)),
			log.Uint32("errors_count", atomic.LoadUint32(&s.errCnt)),
		)
		rctx, cancel := context.WithTimeout(s.ctx, s.config.RoundTimeout)
		offset, err := s.GetOffset(rctx, round, prs)
		cancel()
		round++

		var timeout time.Duration
		if err == nil {
			if offset > s.config.MaxClockOffset || (offset < 0 && -offset > s.config.MaxClockOffset) {
				s.log.With().Error("peers offset is larger than max allowed clock difference",
					log.Uint64("round", round),
					log.Duration("offset", offset),
					log.Duration("max_offset", s.config.MaxClockOffset),
				)
				if atomic.AddUint32(&s.errCnt, 1) == uint32(s.config.MaxOffsetErrors) {
					return ErrPeersNotSynced
				}
			} else {
				s.log.With().Info("peers offset is within max allowed clock difference",
					log.Uint64("round", round),
					log.Duration("offset", offset),
					log.Duration("max_offset", s.config.MaxClockOffset),
				)
			}
			timeout = s.config.RoundInterval
		} else {
			s.log.With().Error("failed to fetch offset from peers", log.Err(err))
			timeout = s.config.RoundTimeout
		}
		if timer == nil {
			timer = time.NewTimer(timeout)
		} else {
			timer.Reset(timeout)
		}
		select {
		case <-s.ctx.Done():
			return s.ctx.Err()
		case <-timer.C:
		}
	}
}

// GetOffset computes offset from received response. The method is stateless and safe to use concurrently.
func (s *Sync) GetOffset(ctx context.Context, id uint64, prs []p2pcrypto.PublicKey) (time.Duration, error) {
	var (
		responses = make(chan Response, len(prs))
		wg        sync.WaitGroup
		round     = Round{
			ID:                id,
			Timestamp:         s.time.Now().UnixNano(),
			RequiredResponses: s.config.RequiredResponses,
		}
		request = Request{ID: id}
	)
	buf, err := types.InterfaceToBytes(&request)
	if err != nil {
		s.log.With().Panic("can't encode request to bytes", log.Err(err))
	}
	for _, peer := range prs {
		wg.Add(1)
		// TODO(dshulyak) double-check that only one handler can be executed
		s.srv.SendRequest(ctx, server.RequestTimeSync, buf, peer, func(buf []byte) {
			defer wg.Done()
			var response Response
			if err := types.BytesToInterface(buf, &response); err != nil {
				s.log.Debug("can't decode response", log.Binary("response", buf), log.Err(err))
				return
			}
			responses <- response
		}, func(error) { wg.Done() })
	}

	wg.Wait()
	close(responses)
	for resp := range responses {
		round.AddResponse(resp, s.time.Now().UnixNano())
	}
	if round.Ready() {
		return round.Offset(), nil
	}
	return 0, ErrTimesyncTimeout
}
