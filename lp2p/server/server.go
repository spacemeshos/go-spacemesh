package server

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"time"

	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/log"
)

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

type response struct {
	Data  []byte
	Error string
}

// Server for the Handler.
type Server struct {
	logger   log.Log
	protocol string
	handler  Handler
	timeout  time.Duration

	h host.Host

	ctx context.Context
}

// New server for the handler.
func New(h host.Host, proto string, handler Handler, opts ...Opt) *Server {
	srv := &Server{
		logger:   log.NewNop(),
		protocol: proto,
		handler:  handler,
		h:        h,
		timeout:  5 * time.Second,
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
	buf, err = s.handler(log.WithNewRequestID(s.ctx), buf)
	var resp response
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
func (s *Server) Request(ctx context.Context, p peer.ID, req []byte, resp func([]byte), failure func(error)) {
	stream, err := s.h.NewStream(network.WithNoDial(ctx, "existing connection"), p, protocol.ID(s.protocol))
	if err != nil {
		failure(err)
		return
	}
	_ = stream.SetDeadline(time.Now().Add(s.timeout))
	go func() {
		defer stream.Close()

		wr := bufio.NewWriter(stream)
		sz := make([]byte, binary.MaxVarintLen64)
		n := binary.PutUvarint(sz, uint64(len(req)))
		_, err := wr.Write(sz[:n])
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
		var r response
		if _, err := codec.DecodeFrom(rd, &r); err != nil {
			failure(err)
		}
		if len(r.Error) > 0 {
			failure(errors.New(r.Error))
		} else {
			resp(r.Data)
		}
	}()
}
