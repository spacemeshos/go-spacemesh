package discovery

import (
	"context"
	"fmt"
	"time"

	levelds "github.com/ipfs/go-ds-leveldb"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	record "github.com/libp2p/go-libp2p-record"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ldbopts "github.com/syndtr/goleveldb/leveldb/opt"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

type Opt func(*Discovery)

func WithPeriod(period time.Duration) Opt {
	return func(d *Discovery) {
		d.period = period
	}
}

func WithTimeout(timeout time.Duration) Opt {
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

func WithHighPeers(peers int) Opt {
	return func(d *Discovery) {
		d.highPeers = peers
	}
}

func WithBootnodes(bootnodes []peer.AddrInfo) Opt {
	return func(d *Discovery) {
		d.bootnodes = bootnodes
	}
}

func WithBackup(backup []peer.AddrInfo) Opt {
	return func(d *Discovery) {
		d.backup = backup
	}
}

func WithLogger(logger *zap.Logger) Opt {
	return func(d *Discovery) {
		d.logger = logger
	}
}

func Server() Opt {
	return func(d *Discovery) {
		d.server = true
	}
}

func Private() Opt {
	return func(d *Discovery) {
		d.public = false
	}
}

func WithDir(path string) Opt {
	return func(d *Discovery) {
		d.dir = path
	}
}

func WithPingPeers(peers []peer.ID) Opt {
	return func(d *Discovery) {
		d.pingPeers = peers
	}
}

func DisableDHT() Opt {
	return func(d *Discovery) {
		d.disableDht = true
	}
}

func New(h host.Host, opts ...Opt) (*Discovery, error) {
	ctx, cancel := context.WithCancel(context.Background())
	d := Discovery{
		public:            true,
		logger:            zap.NewNop(),
		ctx:               ctx,
		cancel:            cancel,
		h:                 h,
		period:            10 * time.Second,
		timeout:           30 * time.Second,
		bootstrapDuration: 30 * time.Second,
		minPeers:          20,
		highPeers:         40,
	}
	for _, opt := range opts {
		opt(&d)
	}
	if len(d.bootnodes) == 0 {
		d.logger.Warn("no bootnodes in the config")
	}
	if !d.disableDht {
		err := d.newDht(ctx, h, d.public, d.server, d.dir)
		if err != nil {
			return nil, err
		}
	}
	return &d, nil
}

type Discovery struct {
	public     bool
	server     bool
	disableDht bool
	dir        string

	logger *zap.Logger
	eg     errgroup.Group
	ctx    context.Context
	cancel context.CancelFunc

	h         host.Host
	dht       *dht.IpfsDHT
	datastore *levelds.Datastore

	// how often to check if we have enough peers
	period time.Duration
	// timeout used for connections
	timeout             time.Duration
	bootstrapDuration   time.Duration
	minPeers, highPeers int
	backup, bootnodes   []peer.AddrInfo
	pingPeers           []peer.ID
}

func (d *Discovery) Start() {
	if len(d.pingPeers) != 0 {
		d.eg.Go(func() error {
			ticker := time.NewTicker(15 * time.Second)
			for {
				select {
				case <-ticker.C:
					func() {
						ctx, cancel := context.WithTimeout(d.ctx, 10*time.Second)
						defer cancel()
						for _, p := range d.pingPeers {
							d.logger.Info("pinging peer",
								zap.String("peer", p.String()))
							addrInfo, err := d.dht.FindPeer(ctx, p)
							if err != nil {
								d.logger.Error("failed to find peer",
									zap.String("peer", p.String()),
									zap.Error(err))
								continue
							}
							if err := d.dht.Ping(ctx, p); err != nil {
								d.logger.Error("ping failed for peer",
									zap.String("peer", p.String()),
									zap.Any("addrInfo", addrInfo),
									zap.Error(err))
								continue
							} else {
								d.logger.Info("ping succeeded for peer",
									zap.String("peer", p.String()),
									zap.Any("addrInfo", addrInfo))
							}
						}
					}()
				case <-d.ctx.Done():
					ticker.Stop()
					return nil
				}
			}
		})
	}
	d.eg.Go(func() error {
		// time.Sleep(30 * time.Second)
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
			case <-d.ctx.Done():
				ticker.Stop()
				d.h.Network().StopNotify(notifiee)
				return nil
			case <-ticker.C:
			case <-disconnected:
			}
			if connected := len(d.h.Network().Peers()); connected >= d.minPeers {
				d.backup = nil // once got enough peers no need to keep backup, they are either already connected or unavailable
				d.logger.Debug("node is connected with required number of peers. skipping bootstrap",
					zap.Int("required", d.minPeers),
					zap.Int("connected", connected),
				)
			} else {
				d.connect(&connEg, d.backup)
				// no reason to spend more resources if we got enough from backup
				if connected := len(d.h.Network().Peers()); connected >= d.minPeers {
					continue
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
	if !d.disableDht {
		if err := d.dht.Close(); err != nil {
			d.logger.Error("error closing dht", zap.Error(err))
		}
		if err := d.datastore.Close(); err != nil {
			d.logger.Error("error closing level datastore", zap.Error(err))
		}
	}
}

func (d *Discovery) bootstrap() {
	if d.dht == nil {
		return
	}
	ctx, cancel := context.WithTimeout(d.ctx, d.bootstrapDuration)
	defer cancel()
	if err := d.dht.Bootstrap(ctx); err != nil {
		d.logger.Error("unexpected error from discovery dht", zap.Error(err))
	}
	<-ctx.Done()
}

func (d *Discovery) connect(eg *errgroup.Group, nodes []peer.AddrInfo) {
	ctx, cancel := context.WithTimeout(d.ctx, d.timeout)
	defer cancel()
	for _, boot := range nodes {
		boot := boot
		if boot.ID == d.h.ID() {
			d.logger.Debug("not dialing self")
			continue
		}
		eg.Go(func() error {
			if err := d.h.Connect(ctx, boot); err != nil {
				d.logger.Warn("failed to connect",
					zap.Stringer("address", boot),
					zap.Error(err),
				)
			}
			return nil
		})
	}
	eg.Wait()
}

func (d *Discovery) newDht(ctx context.Context, h host.Host, public, server bool, dir string) error {
	ds, err := levelds.NewDatastore(dir, &levelds.Options{
		Compression: ldbopts.NoCompression,
		NoSync:      false,
		Strict:      ldbopts.StrictAll,
		ReadOnly:    false,
	})
	if err != nil {
		return fmt.Errorf("open leveldb at %s: %w", dir, err)
	}
	opts := []dht.Option{
		dht.Validator(record.PublicKeyValidator{}),
		dht.Datastore(ds),
		dht.ProtocolPrefix("/spacekad"),
		dht.DisableProviders(),
		dht.DisableValues(),
	}
	if public {
		opts = append(opts, dht.QueryFilter(dht.PublicQueryFilter),
			dht.RoutingTableFilter(dht.PublicRoutingTableFilter))
	}
	if server {
		opts = append(opts, dht.Mode(dht.ModeServer))
	} else {
		opts = append(opts, dht.Mode(dht.ModeAutoServer))
	}
	dht, err := dht.New(ctx, h, opts...)
	if err != nil {
		if err := ds.Close(); err != nil {
			d.logger.Error("error closing level datastore", zap.Error(err))
		}
		return err
	}
	d.dht = dht
	d.datastore = ds
	return nil
}
