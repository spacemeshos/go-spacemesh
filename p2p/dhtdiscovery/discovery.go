package discovery

import (
	"context"
	"errors"
	"fmt"
	randv2 "math/rand/v2"
	"sync"
	"time"

	levelds "github.com/ipfs/go-ds-leveldb"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	record "github.com/libp2p/go-libp2p-record"
	p2pdisc "github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	backoff "github.com/libp2p/go-libp2p/p2p/discovery/backoff"
	p2pdiscr "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	ldbopts "github.com/syndtr/goleveldb/leveldb/opt"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

const (
	discoveryNS         = "spacemesh-disc"
	relayNS             = "spacemesh-disc-relay"
	protocolPrefix      = "/spacekad"
	ProtocolID          = protocolPrefix + "/kad/1.0.0"
	advRecheckInterval  = time.Minute
	discRecheckInterval = 10 * time.Second
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

func WithAdvertiseRetryDelay(value time.Duration) Opt {
	return func(d *Discovery) {
		d.advertiseRetryDelay = value
	}
}

func WithAdvertiseDelay(value time.Duration) Opt {
	return func(d *Discovery) {
		d.advertiseDelay = value
	}
}

func WithAdvertiseInterval(value time.Duration) Opt {
	return func(d *Discovery) {
		d.advertiseInterval = value
	}
}

func WithAdvertiseIntervalSpread(value time.Duration) Opt {
	return func(d *Discovery) {
		d.advertiseIntervalSpread = value
	}
}

func WithFindPeersRetryDelay(value time.Duration) Opt {
	return func(d *Discovery) {
		d.findPeersRetryDelay = value
	}
}

func WithDiscoveryBackoff(backoff bool) Opt {
	return func(d *Discovery) {
		d.discBackoff = backoff
	}
}

func WithDiscoveryBackoffTimings(minBackoff, maxBackoff time.Duration) Opt {
	return func(d *Discovery) {
		d.minBackoff = minBackoff
		d.maxBackoff = maxBackoff
	}
}

func WithConnBackoffTimings(minBackoff, maxBackoff, dialTimeout time.Duration) Opt {
	return func(d *Discovery) {
		d.minConnBackoff = minBackoff
		d.maxConnBackoff = maxBackoff
		d.dialTimeout = dialTimeout
	}
}

type DiscoveryHost interface {
	host.Host
	NeedPeerDiscovery() bool
	HaveRelay() bool
}

func New(h DiscoveryHost, opts ...Opt) (*Discovery, error) {
	d := Discovery{
		public:              true,
		logger:              zap.NewNop(),
		h:                   h,
		period:              10 * time.Second,
		timeout:             30 * time.Second,
		bootstrapDuration:   30 * time.Second,
		minPeers:            20,
		advertiseInterval:   time.Minute,
		advertiseRetryDelay: time.Minute,
		findPeersRetryDelay: time.Minute,
		minBackoff:          60 * time.Second,
		maxBackoff:          time.Hour,
		minConnBackoff:      10 * time.Second,
		maxConnBackoff:      time.Hour,
		dialTimeout:         2 * time.Minute,
	}
	for _, opt := range opts {
		opt(&d)
	}
	if len(d.bootnodes) == 0 {
		d.logger.Warn("no bootnodes in the config")
	}
	cacheSize := 100
	var err error
	d.connBackoff, err = backoff.NewBackoffConnector(h, cacheSize, d.dialTimeout,
		backoff.NewExponentialBackoff(d.minConnBackoff, d.maxConnBackoff, backoff.FullJitter,
			time.Second, 5.0, 0, rngSource{}))
	if err != nil {
		return nil, fmt.Errorf("error creating backoff connector: %w", err)
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
	disc      p2pdisc.Discovery

	// how often to check if we have enough peers
	period time.Duration
	// timeout used for connections
	timeout                 time.Duration
	bootstrapDuration       time.Duration
	minPeers                int
	backup, bootnodes       []peer.AddrInfo
	advertiseDelay          time.Duration
	advertiseInterval       time.Duration
	advertiseIntervalSpread time.Duration
	advertiseRetryDelay     time.Duration
	findPeersRetryDelay     time.Duration
	minBackoff              time.Duration
	maxBackoff              time.Duration
	minConnBackoff          time.Duration
	maxConnBackoff          time.Duration
	dialTimeout             time.Duration
	discBackoff             bool
	connBackoff             *backoff.BackoffConnector
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
		var err error
		d.disc = p2pdiscr.NewRoutingDiscovery(d.dht)
		if d.discBackoff {
			d.disc, err = backoff.NewBackoffDiscovery(
				d.disc,
				backoff.NewExponentialBackoff(
					d.minBackoff, d.maxBackoff, backoff.FullJitter,
					time.Second, 5.0, 0, rngSource{}),
			)
			if err != nil {
				d.Stop()
				return fmt.Errorf("error setting up BackoffDiscovery: %w", err)
			}
		}
		if d.advertise {
			d.eg.Go(func() error {
				return d.advertiseNS(startCtx, discoveryNS, nil)
			})
		}
		if d.enableRoutingDiscovery {
			d.eg.Go(func() error {
				d.discoverPeers(startCtx)
				return nil
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
		d.dhtLock.Lock()
		dht := d.dht
		ds := d.datastore
		d.dhtLock.Unlock()
		if dht != nil {
			if err := dht.Close(); err != nil {
				d.logger.Error("error closing dht", zap.Error(err))
			}
		}
		if ds != nil {
			if err := ds.Close(); err != nil {
				d.logger.Error("error closing level datastore", zap.Error(err))
			}
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
			d.backup = nil // once got enough peers no need to keep, they are either already connected or unavailable
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

func (d *Discovery) advInterval() time.Duration {
	if d.advertiseIntervalSpread == 0 {
		return d.advertiseInterval
	}

	return d.advertiseInterval - d.advertiseIntervalSpread + randv2.N(2*d.advertiseIntervalSpread)
}

func (d *Discovery) advertiseNS(ctx context.Context, ns string, active func() bool) error {
	if d.advertiseDelay != 0 {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(d.advertiseDelay):
		}
	}
	for {
		var ttl time.Duration
		if active == nil || active() {
			var err error
			d.logger.Debug("advertising for routing discovery", zap.String("ns", ns))
			ttl, err = d.disc.Advertise(ctx, ns, p2pdisc.TTL(d.advInterval()))
			if err != nil {
				d.logger.Error("failed to re-advertise for discovery", zap.String("ns", ns), zap.Error(err))
				ttl = d.advertiseRetryDelay
			}
		} else {
			// At this moment, advertisement is not needed.
			// There was previous delay already, just need to recheck if we need
			// to start advertising again
			ttl = advRecheckInterval
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(ttl):
		}
	}
}

func (d *Discovery) discoverPeers(ctx context.Context) {
	peerCh := d.findPeersContinuously(ctx, discoveryNS, d.h.NeedPeerDiscovery)
	d.connBackoff.Connect(ctx, peerCh)
}

func (d *Discovery) discoverRelays(ctx context.Context) {
	for p := range d.findPeersContinuously(ctx, relayNS, nil) {
		if len(p.Addrs) != 0 {
			d.logger.Debug("found relay candidate", zap.Any("p", p))
			select {
			case <-ctx.Done():
				return
			case d.relayCh <- p:
			}
		}
	}
}

func (d *Discovery) findPeersContinuously(
	ctx context.Context,
	ns string,
	active func() bool,
) <-chan peer.AddrInfo {
	peerCh := make(chan peer.AddrInfo)
	d.eg.Go(func() error {
		defer close(peerCh)
		for {
			cont, err := d.findPeers(ctx, ns, active, discRecheckInterval, peerCh)
			if err != nil {
				d.logger.Error("error finding relay peers", zap.String("ns", ns), zap.Error(err))
			}
			if !cont {
				return nil
			}
			d.logger.Debug("pausing discovery", zap.String("ns", ns),
				zap.Duration("duration", d.findPeersRetryDelay))
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(d.findPeersRetryDelay):
			}
		}
	})
	return peerCh
}

func (d *Discovery) findPeers(
	ctx context.Context,
	ns string,
	active func() bool,
	checkInterval time.Duration,
	out chan<- peer.AddrInfo,
) (cont bool, err error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if active != nil && !active() {
		d.logger.Debug("discovery not active", zap.String("ns", ns))
		return true, nil
	}
	d.logger.Debug("started finding peers", zap.String("ns", ns))
	defer d.logger.Debug("done finding peers", zap.String("ns", ns))
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	peerCh, err := d.disc.FindPeers(childCtx, ns)
	if err != nil {
		return !errors.Is(err, context.Canceled), err
	}
	var tickerCh <-chan time.Time
	if active != nil {
		ticker := time.NewTicker(checkInterval)
		tickerCh = ticker.C
		defer ticker.Stop()
	}

	for {
		select {
		case <-ctx.Done():
			return false, nil
		case <-tickerCh:
			// Note: ticker is not started if active is nil.
			if !active() {
				return true, nil
			}
		case p, ok := <-peerCh:
			if !ok {
				return true, nil
			}

			if p.ID == d.h.ID() {
				continue // skip self
			}

			select {
			case <-ctx.Done():
				return false, nil
			case <-tickerCh:
				if !active() {
					return true, nil
				}
			case out <- p:
				d.logger.Debug("discovered peer", zap.String("ns", ns), zap.Stringer("peer", p))
			}
		}
	}
}

type rngSource struct{}

func (r rngSource) Int63() int64 {
	return randv2.Int64()
}

func (r rngSource) Seed(seed int64) {}
