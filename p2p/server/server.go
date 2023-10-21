package server

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-varint"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/log"
)

// ErrNotConnected is returned when peer is not connected.
var ErrNotConnected = errors.New("peer is not connected")

// Opt is a type to configure a server.
type Opt func(s *Server)

// WithTimeout configures stream timeout.
func WithTimeout(timeout time.Duration) Opt {
	return func(s *Server) {
		s.timeout = timeout
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

// Handler is the handler to be defined by the application.
type Handler func(context.Context, []byte) ([]byte, error)

//go:generate scalegen -types Response

// Response is a server response.
type Response struct {
	Data  []byte `scale:"max=10485760"` // 10 MiB
	Error string `scale:"max=1024"`     // TODO(mafa): make error code instead of string
}

//go:generate mockgen -typed -package=mocks -destination=./mocks/mocks.go -source=./server.go

// Host is a subset of libp2p Host interface that needs to be implemented to be usable with server.
type Host interface {
	SetStreamHandler(protocol.ID, network.StreamHandler)
	NewStream(context.Context, peer.ID, ...protocol.ID) (network.Stream, error)
	Network() network.Network
}

// Server for the Handler.
type Server struct {
	logger              log.Log
	protocol            string
	handler             Handler
	timeout             time.Duration
	requestLimit        int
	queueSize           int
	requestsPerInterval int
	interval            time.Duration

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
		timeout:             10 * time.Second,
		requestLimit:        10240,
		queueSize:           1000,
		requestsPerInterval: 100,
		interval:            time.Second,
	}
	for _, opt := range opts {
		opt(srv)
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
			if err := limit.Wait(ctx); err != nil {
				eg.Wait()
				return nil
			}
			eg.Go(func() error {
				s.queueHandler(ctx, req.stream)
				if s.metrics != nil {
					s.metrics.serverLatency.Observe(time.Since(req.received).Seconds())
					s.metrics.completed.Inc()
				}
				return nil
			})
		}
	}
}

func (s *Server) queueHandler(ctx context.Context, stream network.Stream) {
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(s.timeout))
	defer stream.SetDeadline(time.Time{})
	rd := bufio.NewReader(stream)
	size, err := varint.ReadUvarint(rd)
	if err != nil {
		return
	}
	if size > uint64(s.requestLimit) {
		s.logger.With().Warning("request limit overflow",
			log.Int("limit", s.requestLimit),
			log.Uint64("request", size),
		)
		stream.Conn().Close()
		return
	}
	buf := make([]byte, size)
	_, err = io.ReadFull(rd, buf)
	if err != nil {
		return
	}
	start := time.Now()
	buf, err = s.handler(log.WithNewRequestID(ctx), buf)
	s.logger.With().Debug("protocol handler execution time",
		log.String("protocol", s.protocol),
		log.Duration("duration", time.Since(start)),
	)
	var resp Response
	if err != nil {
		resp.Error = err.Error()
	} else {
		resp.Data = buf
	}

	wr := bufio.NewWriter(stream)
	if _, err := codec.EncodeTo(wr, &resp); err != nil {
		s.logger.With().Warning("failed to write response", log.Err(err))
		return
	}
	if err := wr.Flush(); err != nil {
		s.logger.With().Warning("failed to flush stream", log.Err(err))
	}
}

// Request sends a binary request to the peer. Request is executed in the background, one of the callbacks
// is guaranteed to be called on success/error.
func (s *Server) Request(
	ctx context.Context,
	pid peer.ID,
	req []byte,
	resp func([]byte),
	failure func(error),
) error {
	start := time.Now()
	if len(req) > s.requestLimit {
		return fmt.Errorf("request length (%d) is longer than limit %d", len(req), s.requestLimit)
	}
	if s.h.Network().Connectedness(pid) != network.Connected {
		return fmt.Errorf("%w: %s", ErrNotConnected, pid)
	}
	go func() {
		data, err := s.request(ctx, pid, req)
		if err != nil {
			failure(err)
		} else if len(data.Error) > 0 {
			failure(errors.New(data.Error))
		} else {
			resp(data.Data)
		}
		s.logger.WithContext(ctx).With().Debug("request execution time",
			log.String("protocol", s.protocol),
			log.Duration("duration", time.Since(start)),
			log.Err(err),
		)
		switch {
		case s.metrics == nil:
			return
		case err != nil:
			s.metrics.clientLatencyFailure.Observe(time.Since(start).Seconds())
		case err == nil:
			s.metrics.clientLatency.Observe(time.Since(start).Seconds())
		}
	}()
	return nil
}

func (s *Server) request(ctx context.Context, pid peer.ID, req []byte) (*Response, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
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
	_ = stream.SetDeadline(time.Now().Add(s.timeout))

	wr := bufio.NewWriter(stream)
	sz := make([]byte, binary.MaxVarintLen64)
	n := binary.PutUvarint(sz, uint64(len(req)))
	if _, err := wr.Write(sz[:n]); err != nil {
		return nil, err
	}
	if _, err := wr.Write(req); err != nil {
		return nil, err
	}
	if err := wr.Flush(); err != nil {
		return nil, err
	}

	rd := bufio.NewReader(stream)
	var r Response
	if _, err = codec.DecodeFrom(rd, &r); err != nil {
		return nil, err
	}
	return &r, nil
}
