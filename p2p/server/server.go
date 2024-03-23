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
	dto "github.com/prometheus/client_model/go"
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

// Handler is a handler to be defined by the application.
type Handler func(context.Context, []byte) ([]byte, error)

// StreamHandler is a handler that writes the response to the stream directly instead of
// buffering the serialized representation.
type StreamHandler func(context.Context, []byte, io.ReadWriter) error

// StreamRequestCallback is a function that executes a streamed request.
type StreamRequestCallback func(context.Context, io.ReadWriter) error

// ServerError is used by the client (Request/StreamRequest) to represent an error
// returned by the server.
type ServerError struct {
	msg string
}

func NewServerError(msg string) *ServerError {
	return &ServerError{msg: msg}
}

func (*ServerError) Is(target error) bool {
	_, ok := target.(*ServerError)
	return ok
}

func (err *ServerError) Error() string {
	return fmt.Sprintf("peer error: %s", err.msg)
}

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
	handler             StreamHandler
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
func New(h Host, proto string, handler StreamHandler, opts ...Opt) *Server {
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
	dadj := newDeadlineAdjuster(stream, s.timeout, s.hardTimeout)
	defer dadj.Close()
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
	if err = s.handler(log.WithNewRequestID(ctx), buf, dadj); err != nil {
		s.logger.With().Debug("handler reported error",
			log.String("protocol", s.protocol),
			log.Stringer("remotePeer", stream.Conn().RemotePeer()),
			log.Stringer("remoteMultiaddr", stream.Conn().RemoteMultiaddr()),
			log.Err(err),
		)
		return false
	}
	s.logger.With().Debug("protocol handler execution time",
		log.String("protocol", s.protocol),
		log.Stringer("remotePeer", stream.Conn().RemotePeer()),
		log.Stringer("remoteMultiaddr", stream.Conn().RemoteMultiaddr()),
		log.Duration("duration", time.Since(start)),
	)
	return true
}

// Request sends a binary request to the peer.
func (s *Server) Request(ctx context.Context, pid peer.ID, req []byte, extraProtocols ...string) ([]byte, error) {
	var r Response
	if err := s.StreamRequest(ctx, pid, req, func(ctx context.Context, stream io.ReadWriter) error {
		rd := bufio.NewReader(stream)
		if _, err := codec.DecodeFrom(rd, &r); err != nil {
			return fmt.Errorf("peer %s: %w", pid, err)
		}
		if r.Error != "" {
			return &ServerError{msg: r.Error}
		}
		return nil
	}, extraProtocols...); err != nil {
		return nil, err
	}
	return r.Data, nil
}

// StreamRequest sends a binary request to the peer. The response is read from the stream
// by the specified callback.
func (s *Server) StreamRequest(
	ctx context.Context,
	pid peer.ID,
	req []byte,
	callback StreamRequestCallback,
	extraProtocols ...string,
) error {
	start := time.Now()
	if len(req) > s.requestLimit {
		return fmt.Errorf("request length (%d) is longer than limit %d", len(req), s.requestLimit)
	}
	if s.h.Network().Connectedness(pid) != network.Connected {
		return fmt.Errorf("%w: %s", ErrNotConnected, pid)
	}

	ctx, cancel := context.WithTimeout(ctx, s.hardTimeout)
	defer cancel()
	stream, err := s.streamRequest(ctx, pid, req, extraProtocols...)
	if err == nil {
		err = callback(ctx, stream)
		s.logger.WithContext(ctx).With().Debug("request execution time",
			log.String("protocol", s.protocol),
			log.Duration("duration", time.Since(start)),
			log.Err(err),
		)
	}

	serverError := errors.Is(err, &ServerError{})
	took := time.Since(start).Seconds()
	switch {
	case s.metrics == nil:
	case serverError:
		s.metrics.clientServerError.Inc()
		s.metrics.clientLatency.Observe(took)
	case err != nil:
		s.metrics.clientFailed.Inc()
		s.metrics.clientLatencyFailure.Observe(took)
	default:
		s.metrics.clientSucceeded.Inc()
		s.metrics.clientLatency.Observe(took)
	}
	return err
}

func (s *Server) streamRequest(
	ctx context.Context,
	pid peer.ID,
	req []byte,
	extraProtocols ...string,
) (stm io.ReadWriteCloser, err error) {
	protoIDs := make([]protocol.ID, len(extraProtocols)+1)
	for n, p := range extraProtocols {
		protoIDs[n] = protocol.ID(p)
	}
	protoIDs[len(extraProtocols)] = protocol.ID(s.protocol)
	stream, err := s.h.NewStream(
		network.WithNoDial(ctx, "existing connection"),
		pid,
		protoIDs...,
	)
	if err != nil {
		return nil, err
	}
	dadj := newDeadlineAdjuster(stream, s.timeout, s.hardTimeout)
	defer func() {
		if err != nil {
			dadj.Close()
		}
	}()
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
	return dadj, nil
}

// NumAcceptedRequests returns the number of accepted requests for this server.
// It is used for testing.
func (s *Server) NumAcceptedRequests() int {
	if s.metrics == nil {
		return -1
	}
	m := &dto.Metric{}
	if err := s.metrics.accepted.Write(m); err != nil {
		panic("failed to get metric: " + err.Error())
	}
	return int(m.Counter.GetValue())
}

func writeResponse(w io.Writer, resp *Response) error {
	wr := bufio.NewWriter(w)
	if _, err := codec.EncodeTo(wr, resp); err != nil {
		return fmt.Errorf("failed to write response (len %d err len %d): %w",
			len(resp.Data), len(resp.Error), err)
	}
	if err := wr.Flush(); err != nil {
		return fmt.Errorf("failed to write response (len %d err len %d): %w",
			len(resp.Data), len(resp.Error), err)
	}
	return nil
}

func WriteErrorResponse(w io.Writer, respErr error) error {
	return writeResponse(w, &Response{
		Error: respErr.Error(),
	})
}

func ReadResponse(r io.Reader, toCall func(resLen uint32) (int, error)) (int, error) {
	respLen, nBytes, err := codec.DecodeLen(r)
	if err != nil {
		return nBytes, err
	}
	if respLen != 0 {
		n, err := toCall(respLen)
		nBytes += n
		if err != nil {
			return nBytes, err
		}
		if int(respLen) != n {
			return nBytes, errors.New("malformed server response")
		}
	}
	errStr, n, err := codec.DecodeStringWithLimit(r, 1024)
	nBytes += n
	switch {
	case err != nil:
		return nBytes, err
	case errStr != "":
		return nBytes, NewServerError(errStr)
	case respLen == 0:
		return nBytes, errors.New("malformed server response")
	}
	return nBytes, nil
}

func WrapHandler(handler Handler) StreamHandler {
	return func(ctx context.Context, req []byte, stream io.ReadWriter) error {
		buf, err := handler(ctx, req)
		var resp Response
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.Data = buf
		}
		return writeResponse(stream, &resp)
	}
}
