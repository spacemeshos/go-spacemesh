package server

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-varint"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/log"
)

type DecayingTagSpec struct {
	Interval time.Duration `mapstructure:"interval"`
	Inc      int           `mapstructure:"inc"`
	Dec      int           `mapstructure:"dec"`
	Cap      int           `mapstructure:"cap"`
}

var (
	// ErrNotConnected is returned when peer is not connected.
	ErrNotConnected = errors.New("peer is not connected")
	// ErrPeerResponseFailed raised if peer responded with an error.
	ErrPeerResponseFailed = errors.New("peer response failed")
)

// Opt is a type to configure a server.
type Opt func(s *Server)

// WithTimeout configures stream timeout.
// The requests are terminated when no data is received or sent for
// the specified duration.
func WithTimeout(timeout time.Duration) Opt {
	return func(s *Server) {
		s.timeout = timeout
	}
}

// WithHardTimeout configures the hard timeout for requests.
// Requests are terminated if they take longer than the specified
// duration.
func WithHardTimeout(timeout time.Duration) Opt {
	return func(s *Server) {
		s.hardTimeout = timeout
	}
}

// WithLog configures logger for the server.
func WithLog(log log.Log) Opt {
	return func(s *Server) {
		s.logger = log
	}
}

func WithRequestSizeLimit(limit int) Opt {
	return func(s *Server) {
		s.requestLimit = limit
	}
}

// WithMetrics will enable metrics collection in the server.
func WithMetrics() Opt {
	return func(s *Server) {
		s.metrics = newTracker(s.protocol)
	}
}

// WithQueueSize parametrize number of message that will be kept in queue
// and eventually processed by server. Otherwise stream is closed immediately.
//
// Size of the queue should be set to account for maximum expected latency, such as if expected latency is 10s
// and server processes 1000 requests per second size should be 100.
//
// Defaults to 100.
func WithQueueSize(size int) Opt {
	return func(s *Server) {
		s.queueSize = size
	}
}

// WithRequestsPerInterval parametrizes server rate limit to limit maximum amount of bandwidth
// that this handler can consume.
//
// Defaults to 100 requests per second.
func WithRequestsPerInterval(n int, interval time.Duration) Opt {
	return func(s *Server) {
		s.requestsPerInterval = n
		s.interval = interval
	}
}

func WithDecayingTag(tag DecayingTagSpec) Opt {
	return func(s *Server) {
		s.decayingTagSpec = &tag
	}
}

// Handler is the handler to be defined by the application.
type Handler func(context.Context, []byte) ([]byte, error)

//go:generate scalegen -types Response

// Response is a server response.
type Response struct {
	Data  []byte `scale:"max=89128960"` // 85 MiB
	Error string `scale:"max=1024"`     // TODO(mafa): make error code instead of string
}

// Server for the Handler.
type Server struct {
	logger              log.Log
	protocol            string
	handler             Handler
	timeout             time.Duration
	hardTimeout         time.Duration
	requestLimit        int
	queueSize           int
	requestsPerInterval int
	interval            time.Duration
	decayingTagSpec     *DecayingTagSpec
	decayingTag         connmgr.DecayingTag

	metrics *tracker // metrics can be nil

	h Host
}

// New server for the handler.
func New(h Host, proto string, handler Handler, opts ...Opt) *Server {
	srv := &Server{
		logger:              log.NewNop(),
		protocol:            proto,
		handler:             handler,
		h:                   h,
		timeout:             25 * time.Second,
		hardTimeout:         5 * time.Minute,
		requestLimit:        10240,
		queueSize:           1000,
		requestsPerInterval: 100,
		interval:            time.Second,
	}
	for _, opt := range opts {
		opt(srv)
	}

	if srv.decayingTagSpec != nil {
		decayer, supported := connmgr.SupportsDecay(h.ConnManager())
		if supported {
			tag, err := decayer.RegisterDecayingTag(
				"server:"+proto,
				srv.decayingTagSpec.Interval,
				connmgr.DecayFixed(srv.decayingTagSpec.Dec),
				connmgr.BumpSumBounded(0, srv.decayingTagSpec.Cap))
			if err != nil {
				srv.logger.Error("error registering decaying tag", log.Err(err))
			} else {
				srv.decayingTag = tag
			}
		}
	}

	return srv
}

type request struct {
	stream   network.Stream
	received time.Time
}

func (s *Server) Run(ctx context.Context) error {
	limit := rate.NewLimiter(rate.Every(s.interval/time.Duration(s.requestsPerInterval)), s.requestsPerInterval)
	queue := make(chan request, s.queueSize)
	if s.metrics != nil {
		s.metrics.targetQueue.Set(float64(s.queueSize))
		s.metrics.targetRps.Set(float64(limit.Limit()))
	}
	s.h.SetStreamHandler(protocol.ID(s.protocol), func(stream network.Stream) {
		select {
		case queue <- request{stream: stream, received: time.Now()}:
			if s.metrics != nil {
				s.metrics.queue.Set(float64(len(queue)))
				s.metrics.accepted.Inc()
			}
		default:
			if s.metrics != nil {
				s.metrics.dropped.Inc()
			}
			stream.Close()
		}
	})

	var eg errgroup.Group
	eg.SetLimit(s.queueSize)
	for {
		select {
		case <-ctx.Done():
			eg.Wait()
			return nil
		case req := <-queue:
			if s.metrics != nil {
				s.metrics.inQueueLatency.Observe(time.Since(req.received).Seconds())
			}
			if err := limit.Wait(ctx); err != nil {
				eg.Wait()
				return nil
			}
			eg.Go(func() error {
				if s.decayingTag != nil {
					s.decayingTag.Bump(req.stream.Conn().RemotePeer(), s.decayingTagSpec.Inc)
				}
				ok := s.queueHandler(ctx, req.stream)
				if s.metrics != nil {
					s.metrics.serverLatency.Observe(time.Since(req.received).Seconds())
					if ok {
						s.metrics.completed.Inc()
					} else {
						s.metrics.failed.Inc()
					}
				}
				return nil
			})
		}
	}
}

func (s *Server) queueHandler(ctx context.Context, stream network.Stream) bool {
	defer stream.Close()
	defer stream.SetDeadline(time.Time{})
	dadj := newDeadlineAdjuster(stream, s.timeout, s.hardTimeout)
	rd := bufio.NewReader(dadj)
	size, err := varint.ReadUvarint(rd)
	if err != nil {
		s.logger.With().Debug("initial read failed",
			log.String("protocol", s.protocol),
			log.Stringer("remotePeer", stream.Conn().RemotePeer()),
			log.Stringer("remoteMultiaddr", stream.Conn().RemoteMultiaddr()),
			log.Err(err),
		)
		return false
	}
	if size > uint64(s.requestLimit) {
		s.logger.With().Warning("request limit overflow",
			log.String("protocol", s.protocol),
			log.Stringer("remotePeer", stream.Conn().RemotePeer()),
			log.Stringer("remoteMultiaddr", stream.Conn().RemoteMultiaddr()),
			log.Int("limit", s.requestLimit),
			log.Uint64("request", size),
		)
		stream.Conn().Close()
		return false
	}
	buf := make([]byte, size)
	_, err = io.ReadFull(rd, buf)
	if err != nil {
		s.logger.With().Debug("error reading request",
			log.String("protocol", s.protocol),
			log.Stringer("remotePeer", stream.Conn().RemotePeer()),
			log.Stringer("remoteMultiaddr", stream.Conn().RemoteMultiaddr()),
			log.Err(err),
		)
		return false
	}
	start := time.Now()
	buf, err = s.handler(log.WithNewRequestID(ctx), buf)
	s.logger.With().Debug("protocol handler execution time",
		log.String("protocol", s.protocol),
		log.Stringer("remotePeer", stream.Conn().RemotePeer()),
		log.Stringer("remoteMultiaddr", stream.Conn().RemoteMultiaddr()),
		log.Duration("duration", time.Since(start)),
	)
	var resp Response
	if err != nil {
		s.logger.With().Debug("handler reported error",
			log.String("protocol", s.protocol),
			log.Stringer("remotePeer", stream.Conn().RemotePeer()),
			log.Stringer("remoteMultiaddr", stream.Conn().RemoteMultiaddr()),
			log.Err(err),
		)
		resp.Error = err.Error()
	} else {
		resp.Data = buf
	}

	wr := bufio.NewWriter(dadj)
	if _, err := codec.EncodeTo(wr, &resp); err != nil {
		s.logger.With().Warning(
			"failed to write response",
			log.String("protocol", s.protocol),
			log.Stringer("remotePeer", stream.Conn().RemotePeer()),
			log.Stringer("remoteMultiaddr", stream.Conn().RemoteMultiaddr()),
			log.Int("resp.Data len", len(resp.Data)),
			log.Int("resp.Error len", len(resp.Error)),
			log.Err(err),
		)
		return false
	}
	if err := wr.Flush(); err != nil {
		s.logger.With().Warning("failed to flush stream",
			log.String("protocol", s.protocol),
			log.Stringer("remotePeer", stream.Conn().RemotePeer()),
			log.Stringer("remoteMultiaddr", stream.Conn().RemoteMultiaddr()),
			log.Err(err))
		return false
	}

	return true
}

// Request sends a binary request to the peer. Request is executed in the background, one of the callbacks
// is guaranteed to be called on success/error.
func (s *Server) Request(ctx context.Context, pid peer.ID, req []byte) ([]byte, error) {
	start := time.Now()
	if len(req) > s.requestLimit {
		return nil, fmt.Errorf("request length (%d) is longer than limit %d", len(req), s.requestLimit)
	}
	if s.h.Network().Connectedness(pid) != network.Connected {
		return nil, fmt.Errorf("%w: %s", ErrNotConnected, pid)
	}
	data, err := s.request(ctx, pid, req)
	s.logger.WithContext(ctx).With().Debug("request execution time",
		log.String("protocol", s.protocol),
		log.Duration("duration", time.Since(start)),
		log.Err(err),
	)

	took := time.Since(start).Seconds()
	switch {
	case err != nil:
		if s.metrics != nil {
			s.metrics.clientFailed.Inc()
			s.metrics.clientLatencyFailure.Observe(took)
		}
		return nil, err
	case len(data.Error) > 0:
		if s.metrics != nil {
			s.metrics.clientServerError.Inc()
			s.metrics.clientLatency.Observe(took)
		}
		return nil, fmt.Errorf("peer error: %s", data.Error)
	case s.metrics != nil:
		s.metrics.clientSucceeded.Inc()
		s.metrics.clientLatency.Observe(took)
	}
	return data.Data, nil
}

func (s *Server) request(ctx context.Context, pid peer.ID, req []byte) (*Response, error) {
	ctx, cancel := context.WithTimeout(ctx, s.hardTimeout)
	defer cancel()

	var stream network.Stream
	stream, err := s.h.NewStream(
		network.WithNoDial(ctx, "existing connection"),
		pid,
		protocol.ID(s.protocol),
	)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	defer stream.SetDeadline(time.Time{})
	dadj := newDeadlineAdjuster(stream, s.timeout, s.hardTimeout)

	wr := bufio.NewWriter(dadj)
	sz := make([]byte, binary.MaxVarintLen64)
	n := binary.PutUvarint(sz, uint64(len(req)))
	if _, err := wr.Write(sz[:n]); err != nil {
		return nil, fmt.Errorf("peer %s address %s: %w",
			pid, stream.Conn().RemoteMultiaddr(), err)
	}
	if _, err := wr.Write(req); err != nil {
		return nil, fmt.Errorf("peer %s address %s: %w",
			pid, stream.Conn().RemoteMultiaddr(), err)
	}
	if err := wr.Flush(); err != nil {
		return nil, fmt.Errorf("peer %s address %s: %w",
			pid, stream.Conn().RemoteMultiaddr(), err)
	}

	rd := bufio.NewReader(dadj)
	var r Response
	if _, err = codec.DecodeFrom(rd, &r); err != nil {
		return nil, fmt.Errorf("peer %s address %s: %w",
			pid, stream.Conn().RemoteMultiaddr(), err)
	}
	return &r, nil
}
