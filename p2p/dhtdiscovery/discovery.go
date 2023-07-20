package discovery

import (
	"context"
	"errors"
	"time"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

type Opt func(*Discovery)

func WithPeriod(period time.Duration) Opt {
	return func(d *Discovery) {
		d.period = period
	}
}

func WithhTimeout(timeout time.Duration) Opt {
	return func(d *Discovery) {
		d.timeout = timeout
	}
}

func WithBootstrapDuration(bootstrapDuration time.Duration) Opt {
	return func(d *Discovery) {
		d.bootstrapDuration = bootstrapDuration
	}
}

func WithMinPeers(minPeers int) Opt {
	return func(d *Discovery) {
		d.minPeers = minPeers
	}
}

func WithBootnodes(bootnodes []peer.AddrInfo) Opt {
	return func(d *Discovery) {
		d.bootnodes = bootnodes
	}
}

func WithLogger(logger *zap.Logger) Opt {
	return func(d *Discovery) {
		d.logger = logger
	}
}

func New(h host.Host, dht *dht.IpfsDHT, opts ...Opt) *Discovery {
	ctx, cancel := context.WithCancel(context.Background())
	d := Discovery{
		logger:            zap.NewNop(),
		ctx:               ctx,
		cancel:            cancel,
		h:                 h,
		dht:               dht,
		period:            10 * time.Second,
		timeout:           30 * time.Second,
		bootstrapDuration: 30 * time.Second,
		minPeers:          20,
	}
	for _, opt := range opts {
		opt(&d)
	}
	return &d
}

type Discovery struct {
	logger *zap.Logger
	eg     errgroup.Group
	ctx    context.Context
	cancel context.CancelFunc

	h   host.Host
	dht *dht.IpfsDHT

	// how often to check if we have enough peers
	period time.Duration
	// timeout used for connections
	timeout           time.Duration
	bootstrapDuration time.Duration
	minPeers          int
	bootnodes         []peer.AddrInfo
}

func (d *Discovery) Start() {
	d.eg.Go(func() error {
		var connEg errgroup.Group
		disconnected := make(chan struct{}, 1)
		disconnected <- struct{}{} // trigger bootstrap when node starts immediately
		notifiee := &network.NotifyBundle{
			DisconnectedF: func(_ network.Network, c network.Conn) {
				select {
				case disconnected <- struct{}{}:
				default:
				}
			},
		}
		d.h.Network().Notify(notifiee)
		ticker := time.NewTicker(d.period)
		for {
			select {
			case <-ticker.C:
			case <-disconnected:
			case <-d.ctx.Done():
				ticker.Stop()
				d.h.Network().StopNotify(notifiee)
				return nil
			}
			if connected := len(d.h.Network().Peers()); connected >= d.minPeers {
				d.logger.Debug("node is connected with required number of peers. skipping bootstrap",
					zap.Int("required", d.minPeers),
					zap.Int("connected", connected),
				)
			} else {
				if len(d.bootnodes) == 0 {
					d.logger.Warn("bootnodes are empty")
				}
				d.connect(&connEg, d.bootnodes)
				d.bootstrap()
			}
		}
	})
}

func (d *Discovery) Stop() {
	d.cancel()
	d.eg.Wait()
}

func (d *Discovery) bootstrap() {
	ctx, cancel := context.WithTimeout(d.ctx, d.bootstrapDuration)
	defer cancel()
	if err := d.dht.Bootstrap(ctx); err != nil {
		d.logger.Error("unexpected error from discovery dht", zap.Error(err))
	}
}

func (d *Discovery) connect(eg *errgroup.Group, nodes []peer.AddrInfo) {
	ctx, cancel := context.WithTimeout(d.ctx, d.timeout)
	defer cancel()
	for _, boot := range nodes {
		boot := boot
		eg.Go(func() error {
			if err := d.h.Connect(ctx, boot); err != nil && !errors.Is(err, context.Canceled) {
				d.logger.Warn("failed to connect with bootnode",
					zap.Stringer("address", boot),
					zap.Error(err),
				)
			}
			return nil
		})
	}
	eg.Wait()
}
