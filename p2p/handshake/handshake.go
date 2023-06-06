package handshake

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// EventHandshakeComplete is emitted after handshake is completed.
type EventHandshakeComplete struct {
	PID       peer.ID
	Direction network.Direction
}

// Opt is for configuring Handshake.
type Opt func(*Handshake)

// WithLog configures logger for handshake protocol.
func WithLog(logger log.Log) Opt {
	return func(hs *Handshake) {
		hs.logger = logger
	}
}

const (
	hsprotocol    = "handshake/1"
	streamTimeout = 10 * time.Second
)

//go:generate scalegen -types HandshakeMessage,HandshakeAck

// HandshakeMessage is a handshake message.
type HandshakeMessage struct {
	GenesisID types.Hash20
}

// HandshakeAck is a handshake ack.
type HandshakeAck struct {
	Error string `scale:"max=256"` // TODO(mafa): make error code instead of string
}

// New instantiates handshake protocol for the host.
func New(h host.Host, genesisID types.Hash20, opts ...Opt) *Handshake {
	ctx, cancel := context.WithCancel(context.Background())
	hs := &Handshake{
		logger:    log.NewNop(),
		genesisID: genesisID,
		h:         h,
		cancel:    cancel,
	}
	for _, opt := range opts {
		opt(hs)
	}
	h.SetStreamHandler(hsprotocol, hs.handler)
	emitter, err := h.EventBus().Emitter(new(EventHandshakeComplete))
	if err != nil {
		hs.logger.With().Panic("failed to initialize emitter for handshake", log.Err(err))
	}
	sub, err := h.EventBus().Subscribe(new(event.EvtPeerIdentificationCompleted))
	if err != nil {
		hs.logger.With().Panic("failed to subsribe for events", log.Err(err))
	}
	hs.emitter = emitter
	hs.start(ctx, sub)
	return hs
}

// Handshake is a protocol for application specific handshake.
type Handshake struct {
	logger log.Log

	emitter   event.Emitter
	genesisID types.Hash20
	h         host.Host

	cancel context.CancelFunc
	eg     errgroup.Group
}

func (h *Handshake) start(ctx context.Context, sub event.Subscription) {
	h.eg.Go(func() error {
		defer sub.Close()
		for {
			select {
			case <-ctx.Done():
				if ctx.Err() != nil {
					return fmt.Errorf("handshake ctx: %w", ctx.Err())
				}
				return nil
			case evt := <-sub.Out():
				id, ok := evt.(event.EvtPeerIdentificationCompleted)
				if !ok {
					panic("expecting event.EvtPeerIdentificationCompleted")
				}
				logger := h.logger.WithFields(log.String("pid", id.Peer.Pretty()))
				h.eg.Go(func() error {
					logger.Debug("handshake with peer")
					if err := h.Request(ctx, id.Peer); err != nil {
						logger.With().Warning("failed to complete handshake with peer",
							log.Err(err),
						)
						h.h.Network().ClosePeer(id.Peer)
						return nil
					}
					logger.Debug("handshake completed")
					return nil
				})
			}
		}
	})
}

// Stop closes any background workers.
func (h *Handshake) Stop() {
	h.cancel()
	h.eg.Wait()
	h.emitter.Close()
}

// Request handshake with a peer.
func (h *Handshake) Request(ctx context.Context, pid peer.ID) error {
	stream, err := h.h.NewStream(network.WithNoDial(ctx, "existing connection"), pid, protocol.ID(hsprotocol))
	if err != nil {
		return fmt.Errorf("failed to init stream: %w", err)
	}
	defer stream.Close()
	stream.SetDeadline(time.Now().Add(streamTimeout))
	defer stream.SetDeadline(time.Time{})
	if _, err = codec.EncodeTo(stream, &HandshakeMessage{GenesisID: h.genesisID}); err != nil {
		return fmt.Errorf("failed to send handshake msg: %w", err)
	}
	var ack HandshakeAck
	if _, err := codec.DecodeFrom(stream, &ack); err != nil {
		return fmt.Errorf("failed to receive handshake ack: %w", err)
	}
	if len(ack.Error) > 0 {
		return errors.New(ack.Error)
	}
	if err := h.emitter.Emit(EventHandshakeComplete{
		PID:       pid,
		Direction: stream.Conn().Stat().Direction,
	}); err != nil {
		h.logger.With().Error("failed to emit handshake event", log.Err(err))
	}
	return nil
}

func (h *Handshake) handler(stream network.Stream) {
	defer stream.Close()
	stream.SetDeadline(time.Now().Add(streamTimeout))
	defer stream.SetDeadline(time.Time{})
	var msg HandshakeMessage
	if _, err := codec.DecodeFrom(stream, &msg); err != nil {
		return
	}
	if h.genesisID != msg.GenesisID {
		h.logger.With().Warning("network id mismatch",
			log.Stringer("genesis id", h.genesisID),
			log.Stringer("peer genesis id", msg.GenesisID),
			log.String("peer-id", stream.Conn().RemotePeer().String()),
			log.String("peer-address", stream.Conn().RemoteMultiaddr().String()),
		)
		return
	}
	if _, err := codec.EncodeTo(stream, &HandshakeAck{}); err != nil {
		return
	}
}
