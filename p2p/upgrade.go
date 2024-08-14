package p2p

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	lp2plog "github.com/ipfs/go-log/v2"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/host/eventbus"
	ma "github.com/multiformats/go-multiaddr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/sync/errgroup"

	discovery "github.com/spacemeshos/go-spacemesh/p2p/dhtdiscovery"
	"github.com/spacemeshos/go-spacemesh/p2p/peerinfo"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
)

// Opt is for configuring Host.
type Opt func(fh *Host)

// WithLog configures logger for Host.
func WithLog(logger *zap.Logger) Opt {
	return func(fh *Host) {
		fh.logger = logger
	}
}

// WithConfig sets Config for Host.
func WithConfig(cfg Config) Opt {
	return func(fh *Host) {
		fh.cfg = cfg
	}
}

// WithContext set context for Host.
func WithContext(ctx context.Context) Opt {
	return func(fh *Host) {
		fh.ctx = ctx
	}
}

// WithNodeReporter updates reporter that is notified every time when
// node added or removed a peer.
func WithNodeReporter(reporter func() error) Opt {
	return func(fh *Host) {
		fh.nodeReporter = reporter
	}
}

func WithDirectNodes(direct map[peer.ID]struct{}) Opt {
	return func(fh *Host) {
		fh.direct = direct
	}
}

func WithBootnodes(bootnodes map[peer.ID]struct{}) Opt {
	return func(fh *Host) {
		fh.bootnode = bootnodes
	}
}

func WithRelayCandidateChannel(relayCh chan<- peer.AddrInfo) Opt {
	return func(fh *Host) {
		fh.relayCh = relayCh
	}
}

func WithPeerInfo(pi peerinfo.PeerInfo) Opt {
	return func(fh *Host) {
		fh.peerInfo = pi
	}
}

// Host is a conveniency wrapper for all p2p related functionality required to run
// a full spacemesh node.
type Host struct {
	eg     errgroup.Group
	ctx    context.Context
	cancel context.CancelFunc

	cfg    Config
	logger *zap.Logger

	closed struct {
		sync.Mutex
		closed bool
	}

	host.Host
	peerInfo peerinfo.PeerInfo
	pubsub.PubSub

	nodeReporter func() error

	discovery        *discovery.Discovery
	direct, bootnode map[peer.ID]struct{}
	relayCh          chan<- peer.AddrInfo

	natTypeSub event.Subscription
	natType    struct {
		sync.Mutex
		udpNATType network.NATDeviceType
		tcpNATType network.NATDeviceType
	}
	reachSub     event.Subscription
	reachability struct {
		sync.Mutex
		value network.Reachability
	}

	ping *Ping
}

// Upgrade creates Host instance from host.Host.
func Upgrade(h host.Host, opts ...Opt) (*Host, error) {
	ctx, cancel := context.WithCancel(context.Background())
	fh := &Host{
		ctx:    ctx,
		cancel: cancel,
		cfg:    DefaultConfig(),
		logger: zap.NewNop(),
		Host:   h,
	}
	for _, opt := range opts {
		opt(fh)
	}
	cfg := fh.cfg
	bootnodes, err := parseIntoAddr(fh.cfg.Bootnodes)
	if err != nil {
		return nil, err
	}
	direct, err := parseIntoAddr(fh.cfg.Direct)
	if err != nil {
		return nil, err
	}
	for _, peer := range direct {
		h.ConnManager().Protect(peer.ID, "direct")
		// TBD: also protect ping
	}
	if fh.cfg.DisablePubSub {
		fh.PubSub = &pubsub.NullPubSub{}
	} else {
		if fh.PubSub, err = pubsub.New(fh.ctx, fh.logger, h, pubsub.Config{
			Flood:                 cfg.Flood,
			IsBootnode:            cfg.Bootnode,
			Direct:                direct,
			Bootnodes:             bootnodes,
			MaxMessageSize:        cfg.MaxMessageSize,
			QueueSize:             cfg.GossipQueueSize,
			PeerOutboundQueueSize: cfg.GossipPeerOutboundQueueSize,
			Throttle:              cfg.GossipValidationThrottle,
			EvictionStrategy:      cfg.GossipEvictionStrategy,
		}); err != nil {
			return nil, fmt.Errorf("failed to initialize pubsub: %w", err)
		}
	}
	dopts := []discovery.Opt{
		discovery.WithMinPeers(cfg.MinPeers),
		discovery.WithHighPeers(cfg.HighPeers),
		discovery.WithDir(cfg.DataDir),
		discovery.WithBootnodes(bootnodes),
		discovery.WithLogger(fh.logger),
		discovery.WithAdvertiseDelay(fh.cfg.DiscoveryTimings.AdvertiseDelay),
		discovery.WithAdvertiseInterval(fh.cfg.DiscoveryTimings.AdvertiseInterval),
		discovery.WithAdvertiseIntervalSpread(fh.cfg.DiscoveryTimings.AdvertiseIntervalSpread),
		discovery.WithAdvertiseRetryDelay(fh.cfg.DiscoveryTimings.AdvertiseInterval),
		discovery.WithFindPeersRetryDelay(fh.cfg.DiscoveryTimings.FindPeersRetryDelay),
		discovery.WithDiscoveryBackoff(true),
		discovery.WithDiscoveryBackoffTimings(
			fh.cfg.DiscoveryTimings.MinBackoff,
			fh.cfg.DiscoveryTimings.MaxBackoff,
		),
		discovery.WithConnBackoffTimings(
			fh.cfg.DiscoveryTimings.MinConnBackoff,
			fh.cfg.DiscoveryTimings.MaxConnBackoff,
			fh.cfg.DiscoveryTimings.DialTimeout,
		),
	}
	if cfg.PrivateNetwork {
		dopts = append(dopts, discovery.Private())
	}
	if cfg.DisableDHT {
		dopts = append(dopts, discovery.DisableDHT())
	}
	if cfg.Bootnode || cfg.ForceDHTServer {
		dopts = append(dopts, discovery.WithMode(dht.ModeServer))
	} else {
		dopts = append(dopts, discovery.WithMode(dht.ModeAutoServer))
		backup, err := loadPeers(cfg.DataDir)
		if err != nil {
			fh.logger.Warn("failed to to load backup peers", zap.Error(err))
		} else if len(backup) > 0 {
			dopts = append(dopts, discovery.WithBackup(backup))
		}
	}
	if fh.relayCh != nil {
		dopts = append(dopts, discovery.WithRelayCandidateChannel(fh.relayCh))
	}
	if fh.cfg.EnableRoutingDiscovery {
		dopts = append(dopts, discovery.EnableRoutingDiscovery())
	}
	if fh.cfg.RoutingDiscoveryAdvertise {
		dopts = append(dopts, discovery.AdvertiseForPeerDiscovery())
	}

	dhtdisc, err := discovery.New(fh, dopts...)
	if err != nil {
		return nil, err
	}
	fh.discovery = dhtdisc
	if fh.nodeReporter != nil {
		fh.Network().Notify(&network.NotifyBundle{
			ConnectedF: func(network.Network, network.Conn) {
				if err := fh.nodeReporter(); err != nil {
					fh.logger.Error("Failed to emit status update", zap.Error(err))
				} else {
					fh.logger.Debug("reported status update")
				}
			},
			DisconnectedF: func(network.Network, network.Conn) {
				if err := fh.nodeReporter(); err != nil {
					fh.logger.Error("Failed to emit status update", zap.Error(err))
				} else {
					fh.logger.Debug("reported status update")
				}
			},
		})
	}

	var peers []peer.ID
	for _, p := range cfg.PingPeers {
		peerID, err := peer.Decode(p)
		if err != nil {
			fh.logger.Warn("ignoring invalid ping peer", zap.Error(err))
			continue
		}
		peers = append(peers, peerID)
	}
	if len(peers) != 0 {
		fh.ping = NewPing(fh.logger, fh, peers, fh.discovery, WithPingInterval(fh.cfg.PingInterval))
	}

	fh.natTypeSub, err = fh.EventBus().Subscribe(new(event.EvtNATDeviceTypeChanged),
		eventbus.Name("nat type changed (Host)"))
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to reachability NAT type event: %s", err)
	}
	fh.reachSub, err = fh.EventBus().Subscribe(new(event.EvtLocalReachabilityChanged),
		eventbus.Name("reachability changed (Host)"))
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to reachability NAT type event: %s", err)
	}

	return fh, nil
}

// GetPeers returns connected peers.
func (fh *Host) GetPeers() []Peer {
	return fh.Host.Network().Peers()
}

// Connected returns true if the specified peer is connected.
// Peers that only have transient connections to them aren't considered connected.
func (fh *Host) Connected(p Peer) bool {
	if fh.Host.Network().Connectedness(p) != network.Connected {
		return false
	}
	for _, c := range fh.Host.Network().ConnsToPeer(p) {
		if !c.Stat().Limited {
			return true
		}
	}
	return false
}

// ConnectedPeerInfo retrieves a peer info object for the given peer.ID, if the
// given peer is not connected then nil is returned.
func (fh *Host) ConnectedPeerInfo(id peer.ID) *PeerInfo {
	if fh.peerInfo == nil {
		panic("no PeerInfo")
	}
	conns := fh.Network().ConnsToPeer(id)
	// there's no sync between  Peers() and ConnsToPeer() so by the time we
	// try to get the conns they may not exist.
	if len(conns) == 0 {
		return nil
	}

	pi := fh.PeerInfo().EnsurePeerInfo(id)
	var connections []ConnectionInfo
	for _, c := range conns {
		connections = append(connections, ConnectionInfo{
			Address:  c.RemoteMultiaddr(),
			Uptime:   time.Since(c.Stat().Opened),
			Outbound: c.Stat().Direction == network.DirOutbound,
			Kind:     pi.Kind(c),
		})
	}
	var tags []string

	if _, ok := fh.direct[id]; ok {
		tags = append(tags, "direct")
	}
	if _, ok := fh.bootnode[id]; ok {
		tags = append(tags, "bootnode")
	}
	return &PeerInfo{
		ID:          id,
		Connections: connections,
		ClientStats: grabPeerConnStats(&pi.ClientStats),
		ServerStats: grabPeerConnStats(&pi.ServerStats),
		DataStats: DataStats{
			BytesSent:     pi.BytesSent(),
			BytesReceived: pi.BytesReceived(),
			SendRate: [2]int64{
				pi.SendRate(1),
				pi.SendRate(2),
			},
			RecvRate: [2]int64{
				pi.RecvRate(1),
				pi.RecvRate(2),
			},
		},
		Tags: tags,
	}
}

// ProtocolDataStats returns per-protocol data stats.
func (fh *Host) ProtocolDataStats() map[protocol.ID]*peerinfo.DataStats {
	r := make(map[protocol.ID]*peerinfo.DataStats)
	for _, proto := range fh.peerInfo.Protocols() {
		r[proto] = fh.peerInfo.EnsureProtoStats(proto)
	}
	return r
}

// ListenAddresses returns the addresses on which this host listens.
func (fh *Host) ListenAddresses() []ma.Multiaddr {
	return fh.Network().ListenAddresses()
}

// KnownAddresses returns the addresses by which the peers know this one.
func (fh *Host) KnownAddresses() []ma.Multiaddr {
	return fh.Network().Peerstore().Addrs(fh.ID())
}

// NATDeviceType returns NATDeviceType returns the NAT device types for
// UDP and TCP so far for this host.
func (fh *Host) NATDeviceType() (udpNATType, tcpNATType network.NATDeviceType) {
	fh.natType.Lock()
	defer fh.natType.Unlock()
	return fh.natType.udpNATType, fh.natType.tcpNATType
}

// Reachability returns reachability of the host (public, private, unknown).
func (fh *Host) Reachability() network.Reachability {
	fh.reachability.Lock()
	defer fh.reachability.Unlock()
	return fh.reachability.value
}

// DHTServerEnabled returns true if the server has DHT running in server mode.
func (fh *Host) DHTServerEnabled() bool {
	return slices.Contains(fh.Mux().Protocols(), discovery.ProtocolID)
}

// NeedPeerDiscovery returns true if it makes sense to do additional
// discovery of non-DHT (NATed) peers.
func (fh *Host) NeedPeerDiscovery() bool {
	// Once we get LowPeers, the discovery mechanism is no longer
	// needed
	if len(fh.Network().Peers()) >= fh.cfg.LowPeers {
		return false
	}

	// Check if this is a public-reachable node which can reach
	// nodes behind Cone NAT
	if fh.Reachability() == network.ReachabilityPublic {
		return true
	}

	// Check if we have Cone NAT for either TCP or UDP. If so,
	// hole punching should work for other NATed nodes. Also, in
	// case of an unknown NAT type, assume there's chance at hole
	// punching
	udpNATType, tcpNATType := fh.NATDeviceType()
	if fh.cfg.EnableQUICTransport && udpNATType != network.NATDeviceTypeSymmetric {
		return true
	}
	if fh.cfg.EnableTCPTransport && tcpNATType != network.NATDeviceTypeSymmetric {
		return true
	}

	// Symmetric NAT for both TCP and UDP, hole punching will not
	// work so we're not looking for NATed peers. Will only
	// connect to the nodes with DHT Server mode
	return false
}

// HaveRelay returns true if this host can be used as a relay, that
// is, it supports relay service and has public reachability.
func (fh *Host) HaveRelay() bool {
	return fh.cfg.RelayServer.Enable && fh.Reachability() == network.ReachabilityPublic
}

// PeerCount returns number of connected peers.
func (fh *Host) PeerCount() uint64 {
	return uint64(len(fh.Host.Network().Peers()))
}

// PeerProtocols returns the protocols supported by peer.
func (fh *Host) PeerProtocols(p Peer) ([]protocol.ID, error) {
	return fh.Peerstore().GetProtocols(p)
}

// Ping returns Ping structure for this Host, if any PingPeers are
// specified in the config. Otherwise, it returns nil.
func (fh *Host) Ping() *Ping {
	return fh.ping
}

func (fh *Host) Start() error {
	fh.closed.Lock()
	defer fh.closed.Unlock()
	if fh.closed.closed {
		return errors.New("p2p: closed")
	}
	if err := fh.Network().Listen(fh.cfg.Listen...); err != nil {
		fh.Network().Close()
		return fmt.Errorf("p2p: can't listen: %w", err)
	}
	if err := fh.discovery.Start(); err != nil {
		return fmt.Errorf("starting discovery: %w", err)
	}
	if fh.ping != nil {
		fh.ping.Start()
	}
	if !fh.cfg.Bootnode {
		fh.eg.Go(func() error {
			persist(fh.ctx, fh.logger, fh.Host, fh.cfg.DataDir, 30*time.Minute)
			return nil
		})
	}
	fh.eg.Go(fh.trackNetEvents)
	return nil
}

// Stop background workers and release external resources.
func (fh *Host) Stop() error {
	fh.closed.Lock()
	defer fh.closed.Unlock()
	if fh.closed.closed {
		return errors.New("p2p: closed")
	}
	fh.cancel()
	fh.closed.closed = true
	fh.discovery.Stop()
	fh.reachSub.Close()
	fh.natTypeSub.Close()
	fh.eg.Wait()
	if err := fh.Host.Close(); err != nil {
		return fmt.Errorf("failed to close libp2p host: %w", err)
	}
	lp2plog.SetPrimaryCore(zapcore.NewNopCore())
	return nil
}

func (fh *Host) trackNetEvents() error {
	natEvCh := fh.natTypeSub.Out()
	reachEvCh := fh.reachSub.Out()
	for {
		select {
		case ev, ok := <-natEvCh:
			if !ok {
				return nil
			}
			natEv := ev.(event.EvtNATDeviceTypeChanged)
			fh.logger.Info("NAT type changed",
				zap.Stringer("transportProtocol", natEv.TransportProtocol),
				zap.Stringer("type", natEv.NatDeviceType))
			fh.natType.Lock()
			switch natEv.TransportProtocol {
			case network.NATTransportUDP:
				fh.natType.udpNATType = natEv.NatDeviceType
			case network.NATTransportTCP:
				fh.natType.tcpNATType = natEv.NatDeviceType
			}
			fh.natType.Unlock()
		case ev, ok := <-reachEvCh:
			if !ok {
				return nil
			}
			reachEv := ev.(event.EvtLocalReachabilityChanged)
			fh.logger.Info("local reachability changed",
				zap.Stringer("reachability", reachEv.Reachability))
			fh.reachability.Lock()
			fh.reachability.value = reachEv.Reachability
			fh.reachability.Unlock()
		case <-fh.ctx.Done():
			return fh.ctx.Err()
		}
	}
}

func (fh *Host) PeerInfo() peerinfo.PeerInfo {
	return fh.peerInfo
}
