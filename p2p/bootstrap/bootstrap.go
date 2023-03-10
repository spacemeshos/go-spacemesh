package bootstrap

import (
	"context"
	"fmt"

	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/handshake"
)

// EventSpacemeshPeer is emitted when peer is connected after handshake or disconnected.
type EventSpacemeshPeer struct {
	PID           peer.ID
	Direction     network.Direction
	Connectedness network.Connectedness
}

// NewBootstrap create Bootstrap instance.
func NewBootstrap(logger log.Log, h host.Host) (*Bootstrap, error) {
	// TODO(dshulyak) refactor to option and merge Bootstrap with Peers to avoid unnecessary event
	ctx, cancel := context.WithCancel(context.Background())
	b := &Bootstrap{
		cancel: cancel,
		logger: logger,
		host:   h,
	}
	emitter, err := h.EventBus().Emitter(new(EventSpacemeshPeer))
	if err != nil {
		logger.With().Panic("failed to create emitter for EventSpacemeshPeer", log.Err(err))
	}
	sub, err := h.EventBus().Subscribe(new(handshake.EventHandshakeComplete))
	if err != nil {
		logger.With().Panic("failed to subscribe for events", log.Err(err))
	}

	b.eg.Go(func() error {
		return b.run(ctx, sub, emitter)
	})
	return b, nil
}

// Bootstrap enforces required number of outbound connections.
type Bootstrap struct {
	logger log.Log

	host host.Host

	cancel context.CancelFunc
	eg     errgroup.Group
}

// Stop bootstrap and wait until background workers are terminated.
func (b *Bootstrap) Stop() error {
	b.cancel()

	if err := b.eg.Wait(); err != nil {
		return fmt.Errorf("wait: %w", err)
	}

	return nil
}

func (b *Bootstrap) run(ctx context.Context, sub event.Subscription, emitter event.Emitter) error {
	defer emitter.Close()
	defer sub.Close()

	disconnected := make(chan peer.ID, 10)
	notifier := &network.NotifyBundle{
		DisconnectedF: func(_ network.Network, c network.Conn) {
			select {
			case disconnected <- c.RemotePeer():
			case <-ctx.Done():
			}
		},
	}
	b.host.Network().Notify(notifier)
	defer b.host.Network().StopNotify(notifier)

	peers := map[peer.ID]network.Direction{}

	for {
		select {
		case evt := <-sub.Out():
			hs, ok := evt.(handshake.EventHandshakeComplete)
			if !ok {
				panic("event must be of type EventHandshakeComplete")
			}
			if _, exist := peers[hs.PID]; exist {
				continue
			}
			if b.host.Network().Connectedness(hs.PID) == network.NotConnected {
				continue
			}
			peers[hs.PID] = hs.Direction
			b.logger.With().Info("peer is connected",
				log.String("pid", hs.PID.Pretty()),
				log.Bool("outbound", hs.Direction == network.DirOutbound),
				log.Int("total", len(peers)),
			)
			emitter.Emit(EventSpacemeshPeer{
				PID:           hs.PID,
				Direction:     hs.Direction,
				Connectedness: network.Connected,
			})
		case pid := <-disconnected:
			_, exist := peers[pid]
			if exist && b.host.Network().Connectedness(pid) == network.NotConnected {
				emitter.Emit(EventSpacemeshPeer{
					PID:           pid,
					Direction:     peers[pid],
					Connectedness: network.NotConnected,
				})
				b.logger.With().Info("peer is disconnected",
					log.String("pid", pid.Pretty()),
					log.Bool("outbound", peers[pid] == network.DirOutbound),
					log.Int("total", len(peers)-1),
				)
				delete(peers, pid)
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
