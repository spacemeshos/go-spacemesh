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

type Interactor interface {
	Send(data []byte) error
	SendError(err error) error
	Receive() ([]byte, error)
}

type ServerHandler interface {
	Handle(ctx context.Context, i Interactor) (time.Duration, error)
}

type InteractiveHandler func(ctx context.Context, i Interactor) (time.Duration, error)

func (ih InteractiveHandler) Handle(ctx context.Context, i Interactor) (time.Duration, error) {
	return ih(ctx, i)
}

// Handler is the handler to be defined by the application.
type Handler func(context.Context, []byte) ([]byte, error)

func (h Handler) Handle(ctx context.Context, i Interactor) (time.Duration, error) {
	in, err := i.Receive()
	if err != nil {
		return 0, err
	}
	start := time.Now()
	out, err := h(ctx, in)
	duration := time.Since(start)
	if err != nil {
		if err := i.SendError(err); err != nil {
			return duration, err
		}
		return duration, err
	}
	return duration, i.Send(out)
}

//go:generate scalegen -types Response

// Response is a server response.
type Response struct {
	Data  []byte `scale:"max=41943040"` // 40 MiB
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
	handler             ServerHandler
	timeout             time.Duration
	requestLimit        int
	queueSize           int
	requestsPerInterval int
	interval            time.Duration

	metrics *tracker // metrics can be nil

	h Host
}

// New server for the handler.
func New(h Host, proto string, handler ServerHandler, opts ...Opt) *Server {
	srv := &Server{
		logger:              log.NewNop(),
		protocol:            proto,
		handler:             handler,
		h:                   h,
		timeout:             25 * time.Second,
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
	ps := s.peerStream(stream)
	defer ps.Close()
	defer ps.clearDeadline()
	if err := ps.readInitialRequest(); err != nil {
		s.logger.With().Debug("error receiving request", log.Err(err))
		ps.Conn().Close()
		return
	}
	d, err := s.handler.Handle(ctx, ps)
	if err != nil {
		s.logger.With().Debug("protocol handler execution time",
			log.String("protocol", s.protocol),
			log.Duration("duration", d))
	} else {
		s.logger.With().Debug("protocol handler execution failed",
			log.String("protocol", s.protocol),
			log.Duration("duration", d),
			log.Err(err))
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

func (s *Server) InteractiveRequest(
	ctx context.Context,
	pid peer.ID,
	initialRequest []byte,
	handler InteractiveHandler,
	failure func(error),
) error {
	// start := time.Now()
	if len(initialRequest) > s.requestLimit {
		return fmt.Errorf("request length (%d) is longer than limit %d", len(initialRequest), s.requestLimit)
	}
	if s.h.Network().Connectedness(pid) != network.Connected {
		return fmt.Errorf("%w: %s", ErrNotConnected, pid)
	}
	go func() {
		ps, err := s.beginRequest(ctx, pid)
		if err != nil {
			failure(err)
			return
		}
		defer ps.Close()
		defer ps.clearDeadline()

		ps.updateDeadline()
		ps.sendInitialRequest(initialRequest)

		if _, err = handler.Handle(ctx, ps); err != nil {
			failure(err)
		}
		// TODO: client latency metrics
	}()
	return nil
}

func (s *Server) request(ctx context.Context, pid peer.ID, req []byte) (*Response, error) {
	ps, err := s.beginRequest(ctx, pid)
	if err != nil {
		return nil, err
	}
	defer ps.Close()
	defer ps.clearDeadline()

	ps.updateDeadline()
	ps.sendInitialRequest(req)
	return ps.readResponse()
}

func (s *Server) beginRequest(ctx context.Context, pid peer.ID) (*peerStream, error) {
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

	return s.peerStream(stream), err
}

func (s *Server) peerStream(stream network.Stream) *peerStream {
	return &peerStream{
		Stream: stream,
		rd:     bufio.NewReader(stream),
		wr:     bufio.NewWriter(stream),
		s:      s,
	}
}

type peerStream struct {
	network.Stream
	rd      *bufio.Reader
	wr      *bufio.Writer
	s       *Server
	initReq []byte
}

func (ps *peerStream) updateDeadline() {
	ps.SetDeadline(time.Now().Add(ps.s.timeout))
}

func (ps *peerStream) clearDeadline() {
	ps.SetDeadline(time.Time{})
}

func (ps *peerStream) sendInitialRequest(data []byte) error {
	sz := make([]byte, binary.MaxVarintLen64)
	n := binary.PutUvarint(sz, uint64(len(data)))
	if _, err := ps.wr.Write(sz[:n]); err != nil {
		return err
	}
	if _, err := ps.wr.Write(data); err != nil {
		return err
	}
	if err := ps.wr.Flush(); err != nil {
		return err
	}
	return nil
}

var errOversizedRequest = errors.New("request size limit exceeded")

func (ps *peerStream) readInitialRequest() error {
	size, err := varint.ReadUvarint(ps.rd)
	if err != nil {
		return err
	}
	if size > uint64(ps.s.requestLimit) {
		ps.s.logger.With().Warning("request limit overflow",
			log.Int("limit", ps.s.requestLimit),
			log.Uint64("request", size),
		)
		ps.Conn().Close()
		return errOversizedRequest
	}
	ps.initReq = make([]byte, size)
	_, err = io.ReadFull(ps.rd, ps.initReq)
	if err != nil {
		return err
	}
	return nil
}

func (ps *peerStream) sendResponse(resp *Response) error {
	if _, err := codec.EncodeTo(ps.wr, resp); err != nil {
		ps.s.logger.With().Warning(
			"failed to write response",
			log.Int("resp.Data len", len(resp.Data)),
			log.Int("resp.Error len", len(resp.Error)),
			log.Err(err),
		)
		return err
	}

	if err := ps.wr.Flush(); err != nil {
		ps.s.logger.With().Warning("failed to flush stream", log.Err(err))
		return err
	}

	return nil
}

func (ps *peerStream) readResponse() (*Response, error) {
	var r Response
	if _, err := codec.DecodeFrom(ps.rd, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (ps *peerStream) Receive() (data []byte, err error) {
	if ps.initReq != nil {
		data, ps.initReq = ps.initReq, nil
		return data, nil
	}
	resp, err := ps.readResponse()
	switch {
	case err != nil:
		return nil, err
	case resp.Error != "":
		return nil, fmt.Errorf("%w: %s", RemoteError, resp.Error)
	default:
		ps.updateDeadline()
		return resp.Data, nil
	}
}

func (ps *peerStream) Send(data []byte) error {
	ps.updateDeadline()
	return ps.sendResponse(&Response{Data: data})
}

func (ps *peerStream) SendError(err error) error {
	return ps.sendResponse(&Response{Error: err.Error()})
}

var RemoteError = errors.New("peer reported an error")

// TODO: InteractiveRequest should be same as Request
