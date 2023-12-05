package discovery

import (
	"context"
	"fmt"
	"time"

	levelds "github.com/ipfs/go-ds-leveldb"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	record "github.com/libp2p/go-libp2p-record"
	p2pdisc "github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	p2pdiscr "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	ldbopts "github.com/syndtr/goleveldb/leveldb/opt"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

const (
	discoveryNS             = "spacemesh-disc"
	discoveryTag            = "spacemesh-disc"
	discoveryTagValue       = 1
	discoveryHighPeersDelay = 10 * time.Second
	protocolPrefix          = "/spacekad"
	ProtocolID              = protocolPrefix + "/kad/1.0.0"
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

func DisableDHT() Opt {
	return func(d *Discovery) {
		d.disableDht = true
	}
}

func WithRelayCandidateChannel(relayCh chan<- peer.AddrInfo) Opt {
	return func(d *Discovery) {
		d.relayCh = relayCh
	}
}

func EnableRoutingDiscovery() Opt {
	return func(d *Discovery) {
		d.enableRoutingDiscovery = true
	}
}

type DiscoveryHost interface {
	host.Host
	NeedPeerDiscovery() bool
}

func New(h DiscoveryHost, opts ...Opt) (*Discovery, error) {
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
	public                 bool
	server                 bool
	disableDht             bool
	dir                    string
	relayCh                chan<- peer.AddrInfo
	enableRoutingDiscovery bool

	logger *zap.Logger
	eg     errgroup.Group
	ctx    context.Context
	cancel context.CancelFunc

	h         DiscoveryHost
	dht       *dht.IpfsDHT
	datastore *levelds.Datastore

	// how often to check if we have enough peers
	period time.Duration
	// timeout used for connections
	timeout             time.Duration
	bootstrapDuration   time.Duration
	minPeers, highPeers int
	backup, bootnodes   []peer.AddrInfo
}

func (d *Discovery) DHT() *dht.IpfsDHT { return d.dht }

func (d *Discovery) Start() {
	d.eg.Go(d.ensureAtLeastMinPeers)
	if d.enableRoutingDiscovery {
		d.eg.Go(d.discoverPeers)
	}
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
		dht.ProtocolPrefix(protocolPrefix),
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

func (d *Discovery) ensureAtLeastMinPeers() error {
	var connEg errgroup.Group
	disconnected := make(chan struct{}, 1)
	disconnected <- struct{}{} // trigger bootstrap when node starts immediately
	// TODO: connectedF, disconnectedF: track enough rendezvous peers
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
}

func (d *Discovery) discoverPeers() error {
	disc := p2pdiscr.NewRoutingDiscovery(d.dht)

	ticker := time.NewTicker(time.Second * 1)
	defer ticker.Stop()
	peerCh, err := disc.FindPeers(d.ctx, discoveryNS)
	if err != nil {
		return fmt.Errorf("error finding peers: %w", err)
	}

	reAdvCh := time.After(10 * time.Second)

	for {
		for !d.h.NeedPeerDiscovery() {
			d.logger.With().Info("suspending routing discovery",
				zap.Duration("delay", discoveryHighPeersDelay))
			select {
			case <-d.ctx.Done():
				return nil
			case <-time.After(discoveryHighPeersDelay):
			}
		}

		select {
		case <-d.ctx.Done():
			return nil
		case <-reAdvCh:
			ttl, err := disc.Advertise(d.ctx, discoveryNS, p2pdisc.TTL(10*time.Second))
			if err != nil {
				d.logger.Error("failed to re-advertise for discovery", zap.Error(err))
				reAdvCh = time.After(10 * time.Second)
				continue
			}
			reAdvCh = time.After(ttl)
		case p, ok := <-peerCh:
			if !ok {
				time.Sleep(time.Second) // FIXME
				peerCh, err = disc.FindPeers(d.ctx, discoveryNS)
				if err != nil {
					return fmt.Errorf("error finding peers: %w", err)
				}
				continue
			}
			if p.ID == d.h.ID() {
				continue
			}
			if d.h.Network().Connectedness(p.ID) != network.Connected {
				if _, err = d.h.Network().DialPeer(d.ctx, p.ID); err != nil {
					d.logger.Error("error dialing peer", zap.Any("peer", p),
						zap.Error(err))
					continue
				}
			}
			// tag peer to prioritize it over the peers found by other means
			d.h.ConnManager().TagPeer(p.ID, discoveryTag, discoveryTagValue)
			d.logger.Info("found peer via rendezvous", zap.Any("peer", p))
			if d.relayCh != nil && len(p.Addrs) != 0 {
				select {
				case d.relayCh <- p:
				default: // do not block
				}
			}
		}
	}
}
