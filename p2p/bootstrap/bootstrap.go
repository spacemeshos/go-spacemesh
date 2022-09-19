package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"time"

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

// Config for bootstrap.
type Config struct {
	TargetOutbound int           `mapstructure:"target-outbound"`
	Timeout        time.Duration `mapstructure:"timeout"`
}

func DefaultConfig() Config {
	return Config{
		TargetOutbound: 5,
		Timeout:        10 * time.Second,
	}
}

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./bootstrap.go

// Discovery is an interface that actively searches for peers when Bootstrap is called.
type Discovery interface {
	Bootstrap(context.Context) error
}

// NewBootstrap create Bootstrap instance.
func NewBootstrap(logger log.Log, cfg Config, h host.Host, discovery Discovery) (*Bootstrap, error) {
	// TODO(dshulyak) refactor to option and merge Bootstrap with Peers to avoid unnecessary event
	ctx, cancel := context.WithCancel(context.Background())
	b := &Bootstrap{
		cancel:    cancel,
		logger:    logger,
		cfg:       cfg,
		host:      h,
		discovery: discovery,
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
	cfg    Config

	host      host.Host
	discovery Discovery

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

	var (
		outbound int
		peers    = map[peer.ID]network.Direction{}
		limit    = make(chan struct{}, 1)
		ticker   = time.NewTicker(b.cfg.Timeout)
	)
	defer ticker.Stop()

	bootctx, cancel := context.WithTimeout(ctx, b.cfg.Timeout)
	b.triggerBootstrap(bootctx, limit, outbound)
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
			if hs.Direction == network.DirOutbound {
				outbound++
				// peer that is tagged as outbound will have higher weight then inbound peers.
				// this is taken into account when conn manager high watermark is reached and subset of peers will be pruned.
				b.host.ConnManager().TagPeer(hs.PID, "outbound", 100)
				if outbound >= b.cfg.TargetOutbound {
					// cancel bootctx to terminate bootstrap
					cancel()
				}
			}
			b.logger.With().Info("peer is connected",
				log.String("pid", hs.PID.Pretty()),
				log.Bool("outbound", hs.Direction == network.DirOutbound),
				log.Int("total", len(peers)),
				log.Int("outbound-total", outbound),
			)
			emitter.Emit(EventSpacemeshPeer{
				PID:           hs.PID,
				Direction:     hs.Direction,
				Connectedness: network.Connected,
			})
		case pid := <-disconnected:
			_, exist := peers[pid]
			if exist && b.host.Network().Connectedness(pid) == network.NotConnected {
				if peers[pid] == network.DirOutbound {
					outbound--
				}
				emitter.Emit(EventSpacemeshPeer{
					PID:           pid,
					Direction:     peers[pid],
					Connectedness: network.NotConnected,
				})
				b.logger.With().Info("peer is disconnected",
					log.String("pid", pid.Pretty()),
					log.Bool("outbound", peers[pid] == network.DirOutbound),
					log.Int("total", len(peers)-1),
					log.Int("outbound-total", outbound),
				)
				delete(peers, pid)
			}
		case <-ticker.C:
			bootctx, cancel = context.WithTimeout(ctx, b.cfg.Timeout)
			b.triggerBootstrap(bootctx, limit, outbound)
		case <-ctx.Done():
			cancel()
			return ctx.Err()
		}
	}
}

func (b *Bootstrap) triggerBootstrap(ctx context.Context, limit chan struct{}, outbound int) {
	if outbound >= b.cfg.TargetOutbound {
		return
	}
	select {
	case limit <- struct{}{}:
	default:
		return
	}
	b.eg.Go(func() error {
		err := b.discovery.Bootstrap(ctx)
		<-limit
		if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil
		}
		return fmt.Errorf("unexpected error during bootstrap: %w", err)
	})
}
