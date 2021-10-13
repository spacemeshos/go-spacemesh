package bootstrap

import (
	"context"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p-core/event"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/lp2p/handshake"
	"github.com/spacemeshos/go-spacemesh/lp2p/peerexchange"
)

// EventSpacemeshPeer is emitted when peer is connected after handshake or disconnected.
type EventSpacemeshPeer struct {
	PID           peer.ID
	Direction     network.Direction
	Connectedness network.Connectedness
}

// Config for bootstrap.
type Config struct {
	DataPath       string
	Bootstrap      []string
	TargetOutbound int
	Timeout        time.Duration
}

// NewBootstrap create Bootstrap instance.
func NewBootstrap(logger log.Log, cfg Config, h host.Host) (*Bootstrap, error) {
	discovery, err := peerexchange.New(logger, h, peerexchange.Config{
		DataDir:        cfg.DataPath,
		BootstrapNodes: cfg.Bootstrap,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize peerexchange %w", err)
	}
	emitter, err := h.EventBus().Emitter(new(EventSpacemeshPeer))
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	b := &Bootstrap{
		ctx:              ctx,
		cancel:           cancel,
		logger:           logger,
		target:           cfg.TargetOutbound,
		bootstrapTimeout: cfg.Timeout,
		host:             h,
		discovery:        discovery,
		emitter:          emitter,
	}
	sub, err := b.host.EventBus().Subscribe(new(handshake.EventHandshakeComplete))
	if err != nil {
		b.logger.With().Panic("failed to subscribe for events", log.Err(err))
	}
	b.eg.Go(func() error {
		return b.run(b.ctx, sub)
	})
	return b, nil
}

// Bootstrap enforces required number of outbound connections.
type Bootstrap struct {
	logger log.Log

	target           int
	bootstrapTimeout time.Duration

	host      host.Host
	discovery *peerexchange.Discovery

	emitter event.Emitter

	ctx    context.Context
	cancel context.CancelFunc
	eg     errgroup.Group
}

// Stop bootstrap and wait until background workers are terminated.
func (b *Bootstrap) Stop() error {
	b.cancel()
	return b.eg.Wait()
}

func (b *Bootstrap) run(ctx context.Context, sub event.Subscription) error {
	defer b.emitter.Close()
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

	var (
		outbound           int
		discoveryBootstrap bool
		peers              = map[peer.ID]network.Direction{}
		limit              = make(chan struct{}, 1)
		ticker             = time.NewTicker(b.bootstrapTimeout)
	)
	defer ticker.Stop()

	for {
		b.triggerBootstrap(ctx, limit, len(peers), outbound)
		select {
		case evt := <-sub.Out():
			hs, ok := evt.(handshake.EventHandshakeComplete)
			if !ok {
				panic("event must be of type EventHandshakeComplete")
			}
			b.logger.With().Debug("notification on handshake completion is received", log.String("pid", hs.PID.Pretty()))
			if _, exist := peers[hs.PID]; exist {
				continue
			}
			peers[hs.PID] = hs.Direction
			if hs.Direction == network.DirOutbound {
				outbound++
			}
			// peer that is tagged as outbound will have higher weight then inbound peers.
			// this is taken into account when conn manager high watermark is reached and subset of peers will be pruned.
			b.host.ConnManager().TagPeer(hs.PID, "outbound", 100)
			b.emitter.Emit(EventSpacemeshPeer{
				PID:           hs.PID,
				Direction:     hs.Direction,
				Connectedness: network.Connected,
			})
			if len(peers) >= b.target && !discoveryBootstrap {
				// NOTE(dshulyak) this is a hack to support logic in tests that expects a message
				// to be printed once
				discoveryBootstrap = true
				b.logger.Event().Info("discovery_bootstrap", log.Int("peers", len(peers)))
			}
		case pid := <-disconnected:
			_, exist := peers[pid]
			if exist && b.host.Network().Connectedness(pid) == network.NotConnected {
				if peers[pid] == network.DirOutbound {
					outbound--
				}
				b.emitter.Emit(EventSpacemeshPeer{
					PID:           pid,
					Direction:     peers[pid],
					Connectedness: network.NotConnected,
				})
				delete(peers, pid)
			}
		case <-ticker.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (b *Bootstrap) triggerBootstrap(ctx context.Context, limit chan struct{}, total, outbound int) {
	if outbound >= b.target {
		return
	}
	select {
	case limit <- struct{}{}:
	default:
		return
	}
	b.eg.Go(func() error {
		defer func() {
			<-limit
		}()
		ctx, cancel := context.WithTimeout(ctx, b.bootstrapTimeout)
		defer cancel()

		if b.target > outbound {
			b.discovery.Bootstrap(ctx)
		}
		return nil
	})
	return
}
