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

// WithContext configures parent context for contexts that are passed to the handler.
func WithContext(ctx context.Context) Opt {
	return func(s *Server) {
		s.ctx = ctx
	}
}

// Handler is the handler to be defined by the application.
type Handler func(context.Context, []byte) ([]byte, error)

//go:generate scalegen -types Response

// Response is a server response.
type Response struct {
	Data  []byte `scale:"max=1024"` // TODO(mafa): generic type? how to define max size?
	Error string `scale:"max=1024"` // TODO(mafa): check if correct size
}

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./server.go

// Host is a subset of libp2p Host interface that needs to be implemented to be usable with server.
type Host interface {
	SetStreamHandler(protocol.ID, network.StreamHandler)
	NewStream(context.Context, peer.ID, ...protocol.ID) (network.Stream, error)
	Network() network.Network
}

// Server for the Handler.
type Server struct {
	logger   log.Log
	protocol string
	handler  Handler
	timeout  time.Duration

	h Host

	ctx context.Context
}

// New server for the handler.
func New(h Host, proto string, handler Handler, opts ...Opt) *Server {
	srv := &Server{
		ctx:      context.Background(),
		logger:   log.NewNop(),
		protocol: proto,
		handler:  handler,
		h:        h,
		timeout:  10 * time.Second,
	}
	for _, opt := range opts {
		opt(srv)
	}
	h.SetStreamHandler(protocol.ID(proto), srv.streamHandler)
	return srv
}

func (s *Server) streamHandler(stream network.Stream) {
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(s.timeout))
	defer stream.SetDeadline(time.Time{})
	rd := bufio.NewReader(stream)
	size, err := binary.ReadUvarint(rd)
	if err != nil {
		return
	}
	buf := make([]byte, size)
	_, err = io.ReadFull(rd, buf)
	if err != nil {
		return
	}
	start := time.Now()
	buf, err = s.handler(log.WithNewRequestID(s.ctx), buf)
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
func (s *Server) Request(ctx context.Context, pid peer.ID, req []byte, resp func([]byte), failure func(error)) error {
	if s.h.Network().Connectedness(pid) != network.Connected {
		return fmt.Errorf("%w: %s", ErrNotConnected, pid)
	}
	go func() {
		start := time.Now()
		defer func() {
			s.logger.WithContext(ctx).With().Debug("request execution time",
				log.String("protocol", s.protocol),
				log.Duration("duration", time.Since(start)),
			)
		}()
		ctx, cancel := context.WithTimeout(ctx, s.timeout)
		defer cancel()
		stream, err := s.h.NewStream(network.WithNoDial(ctx, "existing connection"), pid, protocol.ID(s.protocol))
		if err != nil {
			failure(err)
			return
		}
		defer stream.Close()
		defer stream.SetDeadline(time.Time{})
		_ = stream.SetDeadline(time.Now().Add(s.timeout))

		wr := bufio.NewWriter(stream)
		sz := make([]byte, binary.MaxVarintLen64)
		n := binary.PutUvarint(sz, uint64(len(req)))
		_, err = wr.Write(sz[:n])
		if err != nil {
			failure(err)
			return
		}
		_, err = wr.Write(req)
		if err != nil {
			failure(err)
			return
		}
		if err := wr.Flush(); err != nil {
			failure(err)
			return
		}

		rd := bufio.NewReader(stream)
		var r Response
		if _, err := codec.DecodeFrom(rd, &r); err != nil {
			failure(err)
			return
		}
		if len(r.Error) > 0 {
			failure(errors.New(r.Error))
		} else {
			resp(r.Data)
		}
	}()
	return nil
}
