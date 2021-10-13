package handshake

import (
	"context"
	"errors"
	"time"

	"github.com/libp2p/go-libp2p-core/event"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/log"
)

// EventHandshakeComplete is emitted after handshake is completed.
type EventHandshakeComplete struct {
	PID       peer.ID
	Direction network.Direction
}

// Opt is for configuring Handshake.
type Opt func(*Handshake)

func WithLog(logger log.Log) Opt {
	return func(hs *Handshake) {
		hs.logger = logger
	}
}

const (
	hsprotocol    = "handshake/1.0.0"
	streamTimeout = 10 * time.Second
)

type handshakeMessage struct {
	Network uint32
}

type handshakeAck struct {
	Error string
}

// New instantiates handshake protocol for the host.
func New(h host.Host, netid uint32, opts ...Opt) *Handshake {
	emitter, err := h.EventBus().Emitter(new(EventHandshakeComplete))
	if err != nil {
		panic(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	hs := &Handshake{
		logger:  log.NewNop(),
		netid:   netid,
		emitter: emitter,
		h:       h,
		ctx:     ctx,
		cancel:  cancel,
	}
	for _, opt := range opts {
		opt(hs)
	}
	h.SetStreamHandler(protocol.ID(hsprotocol), hs.handler)
	sub, err := h.EventBus().Subscribe(new(event.EvtPeerIdentificationCompleted))
	if err != nil {
		hs.logger.With().Panic("failed to subsribe for events", log.Err(err))
	}
	hs.start(sub)
	return hs
}

// Handshake is a protocol for application specific handshake.
type Handshake struct {
	logger log.Log

	netid uint32
	h     host.Host

	emitter event.Emitter
	ctx     context.Context
	cancel  context.CancelFunc
	eg      errgroup.Group
}

func (h *Handshake) start(sub event.Subscription) {
	h.eg.Go(func() error {
		for {
			select {
			case <-h.ctx.Done():
				return h.ctx.Err()
			case evt := <-sub.Out():
				id, ok := evt.(event.EvtPeerIdentificationCompleted)
				if !ok {
					panic("expecting event.EvtPeerIdentificationCompleted")
				}
				logger := h.logger.WithFields(log.String("pid", id.Peer.Pretty()))
				h.eg.Go(func() error {
					logger.Debug("handshake with peer")
					if err := h.Request(h.ctx, id.Peer); err != nil {
						logger.Warning("failed to complete handshake with peer",
							log.Err(err),
						)
						_ = h.h.Network().ClosePeer(id.Peer)
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
		return err
	}
	defer stream.Close()
	stream.SetDeadline(time.Now().Add(streamTimeout))
	if _, err = codec.EncodeTo(stream, &handshakeMessage{Network: h.netid}); err != nil {
		return err
	}
	var ack handshakeAck
	if _, err := codec.DecodeFrom(stream, &ack); err != nil {
		return err
	}
	if len(ack.Error) > 0 {
		return errors.New(ack.Error)
	}
	h.emitter.Emit(EventHandshakeComplete{PID: pid, Direction: stream.Conn().Stat().Direction})
	return nil
}

func (h *Handshake) handler(stream network.Stream) {
	defer stream.Close()
	stream.SetDeadline(time.Now().Add(streamTimeout))
	var msg handshakeMessage
	if _, err := codec.DecodeFrom(stream, &msg); err != nil {
		return
	}
	if msg.Network != h.netid {
		return
	}
	if _, err := codec.EncodeTo(stream, &handshakeAck{}); err != nil {
		return
	}
}
