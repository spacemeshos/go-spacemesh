package peersync

import (
	"context"
	"errors"
	"fmt"
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
	// ErrPeersNotSynced returned if system clock is out of sync with peers clock for configured period of time.
	ErrPeersNotSynced = errors.New("timesync: peers are not time synced")
	// ErrTimesyncTimeout returned if we weren't able to collect enough clock samples from peers in required time.
	ErrTimesyncTimeout = errors.New("timesync: timeout")
)

//go:generate mockgen -package=mocks -destination=./mocks/message_server_mock.go -source=./sync.go MessageServer,Time,PeersProvider

// Time ...
type Time interface {
	Now() time.Time
}

// MessageServer ...
type MessageServer interface {
	SendRequest(context.Context, server.MessageType, []byte, p2pcrypto.PublicKey, func([]byte), func(error)) error
	RegisterBytesMsgHandler(server.MessageType, func(ctx context.Context, b []byte) []byte)
}

// PeersProvider ...
type PeersProvider interface {
	SubscribePeerEvents() (added, expired chan p2pcrypto.PublicKey)
}

type systemTime struct{}

func (s systemTime) Now() time.Time {
	return time.Now()
}

// Request for time from a peer.
type Request struct {
	ID uint64
}

// Response for time from a peer.
type Response struct {
	ID        uint64
	Timestamp int64
}

// DefaultConfig for Sync.
func DefaultConfig() Config {
	return Config{
		RoundRetryInterval: 5 * time.Second,
		RoundInterval:      30 * time.Minute,
		RoundTimeout:       5 * time.Second,
		MaxClockOffset:     10 * time.Second,
		MaxOffsetErrors:    10,
		RequiredResponses:  3,
	}
}

// Config for Sync.
type Config struct {
	RoundRetryInterval time.Duration `mapstructure:"round-retry-interval"`
	RoundInterval      time.Duration `mapstructure:"round-interval"`
	RoundTimeout       time.Duration `mapstructure:"round-timeout"`
	MaxClockOffset     time.Duration `mapstructure:"max-clock-offset"`
	MaxOffsetErrors    int           `mapstructure:"max-offset-errors"`
	RequiredResponses  int           `mapstructure:"required-responses"`
}

// Option to modify Sync behavior.
type Option func(*Sync)

// WithTime modifies source of time used in Sync.
func WithTime(t Time) Option {
	return func(s *Sync) {
		s.time = t
	}
}

// WithContext modifies parent context that is used for all operations in Sync.
func WithContext(ctx context.Context) Option {
	return func(s *Sync) {
		s.ctx = ctx
	}
}

// WithLog modifies Log used in Sync.
func WithLog(lg log.Log) Option {
	return func(s *Sync) {
		s.log = lg
	}
}

// WithConfig modifies config used in Sync.
func WithConfig(config Config) Option {
	return func(s *Sync) {
		s.config = config
	}
}

// New creates Sync instance and returns pointer.
func New(srv MessageServer, peers PeersProvider, opts ...Option) *Sync {
	sync := &Sync{
		log:           log.NewNop(),
		ctx:           context.Background(),
		time:          systemTime{},
		config:        DefaultConfig(),
		srv:           srv,
		peersProvider: peers,
		peersWatcher: peersWatcher{
			requests: make(chan *waitPeersReq),
		},
	}
	for _, opt := range opts {
		opt(sync)
	}
	srv.RegisterBytesMsgHandler(server.RequestTimeSync, sync.requestHandler)

	sync.ctx, sync.cancel = context.WithCancel(sync.ctx)
	sync.wg, sync.ctx = errgroup.WithContext(sync.ctx)
	return sync
}

// Sync manages background worker that compares peers time with system time.
type Sync struct {
	errCnt uint32

	config        Config
	log           log.Log
	srv           MessageServer
	time          Time
	peersProvider PeersProvider
	peersWatcher  peersWatcher

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
			added, expired := s.peersProvider.SubscribePeerEvents()
			return s.peersWatcher.run(s.ctx, added, expired)
		})
	})
}

// Stop background workers. If Sync was created WithContext calling Stop to is not necessary.
// Sync will exit if parent context will be cancelled.
func (s *Sync) Stop() {
	s.cancel()
}

// Wait will return first error that is returned by background workers.
func (s *Sync) Wait() error {
	err := s.wg.Wait()
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func (s *Sync) run() error {
	var (
		timer *time.Timer
		round uint64
	)
	s.log.With().Debug("started sync background worker")
	defer s.log.With().Debug("exiting sync background worker")
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
				atomic.StoreUint32(&s.errCnt, 0)
			}
			timeout = s.config.RoundInterval
		} else {
			s.log.With().Error("failed to fetch offset from peers", log.Err(err))
			timeout = s.config.RoundRetryInterval
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
		round     = round{
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
	var errCnt int
	for _, peer := range prs {
		wg.Add(1)
		// TODO(dshulyak) double-check that only one handler can be executed.
		// this should exit only if we weren't able to send request to required number of peers
		if err := s.srv.SendRequest(ctx, server.RequestTimeSync, buf, peer, func(buf []byte) {
			defer wg.Done()
			var response Response
			if err := types.BytesToInterface(buf, &response); err != nil {
				s.log.Debug("can't decode response", log.Binary("response", buf), log.Err(err))
				return
			}
			responses <- response
		}, func(error) { wg.Done() }); err != nil {
			wg.Done()
			errCnt++
		}
		if len(prs)-errCnt < s.config.RequiredResponses {
			return 0, fmt.Errorf("failed to send requests to %d peers", errCnt)
		}
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
