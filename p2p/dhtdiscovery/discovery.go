package discovery

import (
	"context"
	"errors"
	"fmt"
	"sync"
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
	relayNS                 = "spacemesh-disc-relay"
	discoveryTag            = "spacemesh-disc"
	discoveryTagValue       = 10 // 5 is used for kbucket (DHT)
	discoveryHighPeersDelay = 10 * time.Second
	protocolPrefix          = "/spacekad"
	ProtocolID              = protocolPrefix + "/kad/1.0.0"
	findPeersRetryDelay     = time.Second
	advertiseRetryInterval  = time.Second
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

func WithMode(mode dht.ModeOpt) Opt {
	return func(d *Discovery) {
		d.mode = mode
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

func AdvertiseForPeerDiscovery() Opt {
	return func(d *Discovery) {
		d.advertise = true
	}
}

func WithAdvertiseInterval(aint time.Duration) Opt {
	return func(d *Discovery) {
		d.advertiseInterval = aint
	}
}

type DiscoveryHost interface {
	host.Host
	NeedPeerDiscovery() bool
	HaveRelay() bool
}

func New(h DiscoveryHost, opts ...Opt) (*Discovery, error) {
	d := Discovery{
		public:            true,
		logger:            zap.NewNop(),
		h:                 h,
		period:            10 * time.Second,
		timeout:           30 * time.Second,
		bootstrapDuration: 30 * time.Second,
		minPeers:          20,
		highPeers:         40,
		advertiseInterval: time.Minute,
	}
	for _, opt := range opts {
		opt(&d)
	}
	if len(d.bootnodes) == 0 {
		d.logger.Warn("no bootnodes in the config")
	}
	return &d, nil
}

type Discovery struct {
	public                 bool
	mode                   dht.ModeOpt
	disableDht             bool
	dir                    string
	relayCh                chan<- peer.AddrInfo
	enableRoutingDiscovery bool
	advertise              bool

	logger *zap.Logger
	eg     errgroup.Group
	cancel context.CancelFunc

	h         DiscoveryHost
	dhtLock   sync.Mutex
	dht       *dht.IpfsDHT
	datastore *levelds.Datastore
	disc      *p2pdiscr.RoutingDiscovery

	// how often to check if we have enough peers
	period time.Duration
	// timeout used for connections
	timeout             time.Duration
	bootstrapDuration   time.Duration
	minPeers, highPeers int
	backup, bootnodes   []peer.AddrInfo
	advertiseInterval   time.Duration
}

func (d *Discovery) FindPeer(ctx context.Context, p peer.ID) (peer.AddrInfo, error) {
	d.dhtLock.Lock()
	dht := d.dht
	d.dhtLock.Unlock()
	if dht == nil {
		return peer.AddrInfo{}, errors.New("discovery not started")
	}
	return dht.FindPeer(ctx, p)
}

func (d *Discovery) Start() error {
	if d.cancel != nil {
		return nil
	}
	var startCtx context.Context
	startCtx, d.cancel = context.WithCancel(context.Background())

	if !d.disableDht {
		if err := d.setupDHT(startCtx); err != nil {
			return err
		}
	}

	d.eg.Go(func() error {
		return d.ensureAtLeastMinPeers(startCtx)
	})

	if !d.disableDht {
		d.disc = p2pdiscr.NewRoutingDiscovery(d.dht)
		if d.advertise {
			d.eg.Go(func() error {
				return d.advertiseNS(startCtx, discoveryNS, nil)
			})
		}
		if d.enableRoutingDiscovery {
			d.eg.Go(func() error {
				return d.discoverPeers(startCtx)
			})
			if d.relayCh != nil {
				d.eg.Go(func() error {
					d.discoverRelays(startCtx)
					return nil
				})
			}
		}
		d.eg.Go(func() error {
			return d.advertiseNS(startCtx, relayNS, d.h.HaveRelay)
		})
	}

	return nil
}

func (d *Discovery) Stop() {
	if d.cancel == nil {
		return
	}
	d.cancel()
	d.cancel = nil
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

func (d *Discovery) bootstrap(ctx context.Context) {
	if d.dht == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, d.bootstrapDuration)
	defer cancel()
	if err := d.dht.Bootstrap(ctx); err != nil {
		d.logger.Error("unexpected error from discovery dht", zap.Error(err))
	}
	<-ctx.Done()
}

func (d *Discovery) connect(ctx context.Context, eg *errgroup.Group, nodes []peer.AddrInfo) {
	conCtx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	for _, boot := range nodes {
		boot := boot
		if boot.ID == d.h.ID() {
			d.logger.Debug("not dialing self")
			continue
		}
		eg.Go(func() error {
			if err := d.h.Connect(conCtx, boot); err != nil {
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

func (d *Discovery) setupDHT(ctx context.Context) error {
	d.dhtLock.Lock()
	defer d.dhtLock.Unlock()
	ds, err := levelds.NewDatastore(d.dir, &levelds.Options{
		Compression: ldbopts.NoCompression,
		NoSync:      false,
		Strict:      ldbopts.StrictAll,
		ReadOnly:    false,
	})
	if err != nil {
		return fmt.Errorf("open leveldb at %s: %w", d.dir, err)
	}
	opts := []dht.Option{
		dht.Validator(record.PublicKeyValidator{}),
		dht.Datastore(ds),
		dht.ProtocolPrefix(protocolPrefix),
	}
	if d.public {
		opts = append(opts, dht.QueryFilter(dht.PublicQueryFilter),
			dht.RoutingTableFilter(dht.PublicRoutingTableFilter))
	}
	opts = append(opts, dht.Mode(d.mode))
	dht, err := dht.New(ctx, d.h, opts...)
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

func (d *Discovery) ensureAtLeastMinPeers(ctx context.Context) error {
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
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
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
			d.connect(ctx, &connEg, d.backup)
			// no reason to spend more resources if we got enough from backup
			if connected := len(d.h.Network().Peers()); connected >= d.minPeers {
				continue
			}
			d.connect(ctx, &connEg, d.bootnodes)
			d.bootstrap(ctx)
		}
	}
}

func (d *Discovery) peerHasTag(p peer.ID) bool {
	ti := d.h.ConnManager().GetTagInfo(p)
	if ti == nil {
		return false
	}
	_, found := ti.Tags[discoveryTag]
	return found
}

func (d *Discovery) advertiseNS(ctx context.Context, ns string, active func() bool) error {
	for {
		var ttl time.Duration
		if active == nil || active() {
			var err error
			d.logger.Debug("advertising for routing discovery", zap.String("ns", ns))
			ttl, err = d.disc.Advertise(ctx, ns, p2pdisc.TTL(d.advertiseInterval))
			if err != nil {
				d.logger.Error("failed to re-advertise for discovery", zap.String("ns", ns), zap.Error(err))
				ttl = advertiseRetryInterval
			}
		} else {
			ttl = d.advertiseInterval
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(ttl):
		}
	}
}

func (d *Discovery) discoverPeers(ctx context.Context) error {
	for p := range d.findPeersContinuously(ctx, discoveryNS) {
		wasSuspended := false
		for !d.h.NeedPeerDiscovery() {
			wasSuspended = true
			d.logger.Info("suspending routing discovery",
				zap.Duration("delay", discoveryHighPeersDelay))
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(discoveryHighPeersDelay):
			}
		}
		if wasSuspended {
			d.logger.Info("resuming routing discovery")
		}

		if p.ID == d.h.ID() {
			continue
		}
		freshlyConnected := !d.peerHasTag(p.ID)
		// trim open conns to free up some space
		d.h.ConnManager().TrimOpenConns(ctx)
		// tag peer to prioritize it over the peers found by other means
		d.h.ConnManager().TagPeer(p.ID, discoveryTag, discoveryTagValue)
		if d.h.Network().Connectedness(p.ID) != network.Connected {
			if _, err := d.h.Network().DialPeer(ctx, p.ID); err != nil {
				d.logger.Debug("error dialing peer", zap.Any("peer", p),
					zap.Error(err))
				continue
			}
			freshlyConnected = true
		}
		if freshlyConnected {
			d.logger.Info("found peer via rendezvous", zap.Any("peer", p))
		}
	}

	return nil
}

func (d *Discovery) discoverRelays(ctx context.Context) {
	for p := range d.findPeersContinuously(ctx, relayNS) {
		if len(p.Addrs) != 0 {
			d.logger.Debug("found relay candidate", zap.Any("p", p))
			select {
			case d.relayCh <- p:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (d *Discovery) findPeersContinuously(ctx context.Context, ns string) <-chan peer.AddrInfo {
	r := make(chan peer.AddrInfo)
	d.eg.Go(func() error {
		defer close(r)
		var peerCh <-chan peer.AddrInfo
		for {
			if peerCh == nil {
				var err error
				peerCh, err = d.disc.FindPeers(ctx, ns)
				if err != nil {
					d.logger.Error("error finding relay peers", zap.Error(err))
					select {
					case <-ctx.Done():
						return nil
					case <-time.After(findPeersRetryDelay):
					}
					peerCh = nil
					continue
				}
			}

			select {
			case <-ctx.Done():
				return nil
			case p, ok := <-peerCh:
				if !ok {
					peerCh = nil
					select {
					case <-ctx.Done():
						return nil
					case <-time.After(findPeersRetryDelay):
					}
					continue
				}
				select {
				case r <- p:
				case <-ctx.Done():
				}
			}
		}
	})
	return r
}
