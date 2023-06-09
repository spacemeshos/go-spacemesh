package peersync

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

const (
	protocolName = "/peersync/1.0/"
)

var (
	// ErrPeersNotSynced returned if system clock is out of sync with peers clock for configured period of time.
	ErrPeersNotSynced = errors.New("timesync: peers are not time synced")
	// ErrTimesyncFailed returned if we weren't able to collect enough clock samples from peers.
	ErrTimesyncFailed = errors.New("timesync: failed request")
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./sync.go

// Time provides interface for current time.
type Time interface {
	Now() time.Time
}

type systemTime struct{}

func (s systemTime) Now() time.Time {
	return time.Now()
}

type getPeers interface {
	GetPeers() []p2p.Peer
}

//go:generate scalegen -types Request,Response

// Request is a sync request.
type Request struct {
	ID uint64
}

// Response is a sync response.
type Response struct {
	ID        uint64
	Timestamp uint64
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
	Disable            bool          `mapstructure:"disable"`
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
func New(h host.Host, peers getPeers, opts ...Option) *Sync {
	sync := &Sync{
		log:    log.NewNop(),
		ctx:    context.Background(),
		time:   systemTime{},
		h:      h,
		config: DefaultConfig(),
		peers:  peers,
	}
	for _, opt := range opts {
		opt(sync)
	}
	sync.ctx, sync.cancel = context.WithCancel(sync.ctx)
	h.SetStreamHandler(protocolName, sync.streamHandler)
	return sync
}

// Sync manages background worker that compares peers time with system time.
type Sync struct {
	errCnt uint32

	config Config
	log    log.Log
	time   Time
	h      host.Host
	peers  getPeers

	eg     errgroup.Group
	ctx    context.Context
	cancel func()
}

func (s *Sync) streamHandler(stream network.Stream) {
	defer stream.Close()
	_ = stream.SetDeadline(s.time.Now().Add(s.config.RoundTimeout))
	defer stream.SetDeadline(time.Time{})
	var request Request
	if _, err := codec.DecodeFrom(stream, &request); err != nil {
		s.log.With().Warning("can't decode request", log.Err(err))
		return
	}
	resp := Response{
		ID:        request.ID,
		Timestamp: uint64(s.time.Now().UnixNano()),
	}
	if _, err := codec.EncodeTo(stream, &resp); err != nil {
		s.log.With().Warning("can't encode response", log.Err(err))
	}
}

// Start background workers.
func (s *Sync) Start() {
	s.eg.Go(func() error {
		return s.run()
	})
}

// Stop background workers.
func (s *Sync) Stop() {
	s.cancel()
	s.Wait()
}

// Wait will return first error that is returned by background workers.
func (s *Sync) Wait() error {
	err := s.eg.Wait()
	if errors.Is(err, context.Canceled) {
		return nil
	}

	return fmt.Errorf("taskgroup: %w", err)
}

func (s *Sync) run() error {
	var (
		timer *time.Timer
		round uint64
	)
	s.log.With().Debug("started sync background worker")
	defer s.log.With().Debug("exiting sync background worker")
	for {
		prs := s.peers.GetPeers()
		timeout := s.config.RoundRetryInterval
		if len(prs) >= s.config.RequiredResponses {
			s.log.With().Debug("starting time sync round with peers",
				log.Uint64("round", round),
				log.Int("peers_count", len(prs)),
				log.Uint32("errors_count", atomic.LoadUint32(&s.errCnt)),
			)
			ctx, cancel := context.WithTimeout(s.ctx, s.config.RoundTimeout)
			offset, err := s.GetOffset(ctx, round, prs)
			cancel()
			if err == nil {
				if offset > s.config.MaxClockOffset || (offset < 0 && -offset > s.config.MaxClockOffset) {
					s.log.With().Warning("peers offset is larger than max allowed clock difference",
						log.Uint64("round", round),
						log.Duration("offset", offset),
						log.Duration("max_offset", s.config.MaxClockOffset),
					)
					if atomic.AddUint32(&s.errCnt, 1) == uint32(s.config.MaxOffsetErrors) {
						return clockError{
							err:     ErrPeersNotSynced,
							details: clockErrorDetails{Drift: offset},
						}
					}
				} else {
					s.log.With().Debug("peers offset is within max allowed clock difference",
						log.Uint64("round", round),
						log.Duration("offset", offset),
						log.Duration("max_offset", s.config.MaxClockOffset),
					)
					atomic.StoreUint32(&s.errCnt, 0)
				}
				offsetGauge.Set(offset.Seconds())
				timeout = s.config.RoundInterval
			} else {
				s.log.With().Error("failed to fetch offset from peers", log.Err(err))
			}
			round++
		}
		if timer == nil {
			timer = time.NewTimer(timeout)
		} else {
			timer.Reset(timeout)
		}
		select {
		case <-s.ctx.Done():
			return fmt.Errorf("context done: %w", s.ctx.Err())
		case <-timer.C:
		}
	}
}

// GetOffset computes offset from received response. The method is stateless and safe to use concurrently.
func (s *Sync) GetOffset(ctx context.Context, id uint64, prs []p2p.Peer) (time.Duration, error) {
	var (
		responses = make(chan Response, len(prs))
		round     = round{
			ID:                id,
			Timestamp:         s.time.Now().UnixNano(),
			RequiredResponses: s.config.RequiredResponses,
		}
		wg sync.WaitGroup
	)
	buf, err := codec.Encode(&Request{ID: id})
	if err != nil {
		s.log.With().Panic("can't encode request to bytes", log.Err(err))
	}
	for _, pid := range prs {
		wg.Add(1)
		go func(pid p2p.Peer) {
			defer wg.Done()
			logger := s.log.WithFields(log.String("pid", pid.Pretty())).With()
			stream, err := s.h.NewStream(network.WithNoDial(ctx, "existing connection"), pid, protocolName)
			if err != nil {
				logger.Warning("failed to create new stream", log.Err(err))
				return
			}
			defer stream.Close()
			_ = stream.SetDeadline(s.time.Now().Add(s.config.RoundTimeout))
			defer stream.SetDeadline(time.Time{})
			if _, err := stream.Write(buf); err != nil {
				logger.Warning("failed to send a request", log.Err(err))
				return
			}
			var resp Response
			if _, err := codec.DecodeFrom(stream, &resp); err != nil {
				logger.Warning("failed to read response from peer", log.Err(err))
				return
			}
			select {
			case <-ctx.Done():
			case responses <- resp:
			}
		}(pid)
	}
	go func() {
		wg.Wait()
		close(responses)
	}()
	for resp := range responses {
		round.AddResponse(resp, s.time.Now().UnixNano())
	}
	if round.Ready() {
		return round.Offset(), nil
	}
	return 0, fmt.Errorf("%w: failed on timeout", ErrTimesyncFailed)
}
