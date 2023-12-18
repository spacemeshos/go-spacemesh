package p2p

import (
	"context"
	"errors"
	"fmt"
	"time"

	lp2plog "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p"
	ccmgr "github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/pnet"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/core/transport"
	"github.com/libp2p/go-libp2p/p2p/host/autorelay"
	"github.com/libp2p/go-libp2p/p2p/host/peerstore/pstoremem"
	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	tptu "github.com/libp2p/go-libp2p/p2p/net/upgrader"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	quic "github.com/libp2p/go-libp2p/p2p/transport/quic"
	"github.com/libp2p/go-libp2p/p2p/transport/quicreuse"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	"github.com/multiformats/go-multiaddr"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/handshake"
	p2pmetrics "github.com/spacemeshos/go-spacemesh/p2p/metrics"
)

// DefaultConfig config.
func DefaultConfig() Config {
	return Config{
		ListenAddresses:        []string{"/ip4/0.0.0.0/tcp/7513"},
		Flood:                  false,
		MinPeers:               20,
		LowPeers:               40,
		HighPeers:              100,
		AutoscalePeers:         true,
		GracePeersShutdown:     30 * time.Second,
		MaxMessageSize:         2 << 20,
		DisableResourceManager: true,
		AcceptQueue:            tptu.AcceptQueueLength,
		EnableHolepunching:     true,
		InboundFraction:        0.8,
		OutboundFraction:       1.1,
		RelayServer:            RelayServer{TTL: 20 * time.Minute, Reservations: 512},
		IP4Blocklist: []string{
			// localhost
			"127.0.0.0/8",
			// private networks
			"10.0.0.0/8",
			"100.64.0.0/10",
			"172.16.0.0/12",
			"192.168.0.0/16",
			// link local
			"169.254.0.0/16",
		},
		IP6Blocklist: []string{
			// localhost
			"::1/128",
			// ULA reserved
			"fc00::/7",
			// link local
			"fe80::/10",
		},
		GossipQueueSize:             50000,
		GossipValidationThrottle:    50000,
		GossipAtxValidationThrottle: 50000,
		EnableTCPTransport:          true,
		EnableQUICTransport:         false,
	}
}

const (
	PublicReachability  = "public"
	PrivateReachability = "private"
)

// Config for all things related to p2p layer.
type Config struct {
	DataDir            string
	LogLevel           log.Level
	GracePeersShutdown time.Duration
	MaxMessageSize     int

	// see https://lwn.net/Articles/542629/ for reuseport explanation
	DisableReusePort            bool        `mapstructure:"disable-reuseport"`
	DisableNatPort              bool        `mapstructure:"disable-natport"`
	DisableConnectionManager    bool        `mapstructure:"disable-connection-manager"`
	DisableResourceManager      bool        `mapstructure:"disable-resource-manager"`
	DisableDHT                  bool        `mapstructure:"disable-dht"`
	Flood                       bool        `mapstructure:"flood"`
	Listen                      string      `mapstructure:"listen"`
	ListenAddresses             []string    `mapstructure:"listen-addresses"`
	Bootnodes                   []string    `mapstructure:"bootnodes"`
	Direct                      []string    `mapstructure:"direct"`
	MinPeers                    int         `mapstructure:"min-peers"`
	LowPeers                    int         `mapstructure:"low-peers"`
	HighPeers                   int         `mapstructure:"high-peers"`
	InboundFraction             float64     `mapstructure:"inbound-fraction"`
	OutboundFraction            float64     `mapstructure:"outbound-fraction"`
	AutoscalePeers              bool        `mapstructure:"autoscale-peers"`
	AdvertiseAddress            string      `mapstructure:"advertise-address"`
	AdvertiseAddresses          []string    `mapstructure:"advertise-addresses"`
	AcceptQueue                 int         `mapstructure:"p2p-accept-queue"`
	Metrics                     bool        `mapstructure:"p2p-metrics"`
	Bootnode                    bool        `mapstructure:"p2p-bootnode"`
	ForceReachability           string      `mapstructure:"p2p-reachability"`
	ForceDHTServer              bool        `mapstructure:"force-dht-server"`
	EnableHolepunching          bool        `mapstructure:"p2p-holepunching"`
	PrivateNetwork              bool        `mapstructure:"p2p-private-network"`
	RelayServer                 RelayServer `mapstructure:"relay-server"`
	IP4Blocklist                []string    `mapstructure:"ip4-blocklist"`
	IP6Blocklist                []string    `mapstructure:"ip6-blocklist"`
	GossipQueueSize             int         `mapstructure:"gossip-queue-size"`
	GossipValidationThrottle    int         `mapstructure:"gossip-validation-throttle"`
	GossipAtxValidationThrottle int         `mapstructure:"gossip-atx-validation-throttle"`
	PingPeers                   []string    `mapstructure:"ping-peers"`
	Relay                       bool        `mapstructure:"relay"`
	Relays                      []string    `mapstructure:"relays"`
	EnableTCPTransport          bool        `mapstructure:"enable-tcp-transport"`
	EnableQUICTransport         bool        `mapstructure:"enable-quic-transport"`
	EnableRoutingDiscovery      bool        `mapstructure:"enable-routing-discovery"`
	RoutingDiscoveryNoAdvertise bool        `mapstructure:"routing-discovery-no-advertise"`
	// TBD: adv peer fraction
}

type RelayServer struct {
	Enable       bool          `mapstructure:"enable"`
	Reservations int           `mapstructure:"reservations"`
	TTL          time.Duration `mapstructure:"ttl"`
}

func (cfg *Config) listenAddrs() []string {
	if cfg.Listen != "" {
		return []string{cfg.Listen}
	}
	return cfg.ListenAddresses
}

func (cfg *Config) advertisedAddrs() []string {
	if cfg.AdvertiseAddress != "" {
		return []string{cfg.AdvertiseAddress}
	}
	return cfg.AdvertiseAddresses
}

func (cfg *Config) Validate() error {
	if !cfg.EnableTCPTransport && !cfg.EnableQUICTransport {
		return errors.New("no transports enabled")
	}
	if len(cfg.ForceReachability) > 0 {
		if cfg.ForceReachability != PublicReachability &&
			cfg.ForceReachability != PrivateReachability {
			return fmt.Errorf("p2p-reachability flag is invalid. should be one of %s, %s. got %s",
				PublicReachability, PrivateReachability, cfg.ForceReachability,
			)
		}
	}

	for _, addrStr := range append(cfg.advertisedAddrs(), cfg.advertisedAddrs()...) {
		_, err := multiaddr.NewMultiaddr(addrStr)
		if err != nil {
			return fmt.Errorf("address %s is not a valid multiaddr %w", addrStr, err)
		}
	}

	return nil
}

// New initializes libp2p host configured for spacemesh.
func New(
	_ context.Context,
	logger log.Log,
	cfg Config,
	prologue []byte,
	quicNetCookie handshake.NetworkCookie,
	opts ...Opt,
) (*Host, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	logger.Zap().Info("starting libp2p host", zap.Any("config", &cfg))
	key, err := EnsureIdentity(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	lp2plog.SetPrimaryCore(logger.Core())
	lp2plog.SetAllLoggers(lp2plog.LogLevel(cfg.LogLevel))
	cm, err := connmgr.NewConnManager(
		cfg.LowPeers,
		cfg.HighPeers,
		connmgr.WithGracePeriod(cfg.GracePeersShutdown),
	)
	if err != nil {
		return nil, fmt.Errorf("p2p create conn mgr: %w", err)
	}
	streamer := *yamux.DefaultTransport
	streamer.Config().ConnectionWriteTimeout = 25 * time.Second // should be NOT exposed in the config
	ps, err := pstoremem.NewPeerstore()
	if err != nil {
		return nil, fmt.Errorf("can't create peer store: %w", err)
	}

	bootnodesMap := make(map[peer.ID]struct{})
	bootnodes, err := parseIntoAddr(cfg.Bootnodes)
	if err != nil {
		return nil, err
	}
	for _, pid := range bootnodes {
		bootnodesMap[pid.ID] = struct{}{}
	}

	directMap := make(map[peer.ID]struct{})
	direct, err := parseIntoAddr(cfg.Direct)
	if err != nil {
		return nil, err
	}
	for _, pid := range direct {
		directMap[pid.ID] = struct{}{}
	}
	// leaves a small room for outbound connections in order to
	// reduce risk of network isolation
	g := &gater{
		inbound:  int(float64(cfg.HighPeers) * cfg.InboundFraction),
		outbound: int(float64(cfg.HighPeers) * cfg.OutboundFraction),
		direct:   directMap,
	}

	g.direct = directMap
	lopts := []libp2p.Option{
		libp2p.Identity(key),
		libp2p.ListenAddrStrings(cfg.listenAddrs()...),
		libp2p.UserAgent("go-spacemesh"),
		libp2p.Muxer("/yamux/1.0.0", &streamer),
		libp2p.Peerstore(ps),
		libp2p.BandwidthReporter(p2pmetrics.NewBandwidthCollector()),
		libp2p.EnableNATService(),
		libp2p.ConnectionGater(g),
	}
	if cfg.EnableTCPTransport {
		lopts = append(lopts,
			libp2p.Transport(
				func(upgrader transport.Upgrader, rcmgr network.ResourceManager) (transport.Transport, error) {
					opts := []tcp.Option{}
					if cfg.DisableReusePort {
						opts = append(opts, tcp.DisableReuseport())
					}
					if cfg.Metrics {
						opts = append(opts, tcp.WithMetrics())
					}
					return tcp.NewTCPTransport(upgrader, rcmgr, opts...)
				},
			),
			libp2p.Security(
				noise.ID,
				func(id protocol.ID, privkey crypto.PrivKey, muxers []tptu.StreamMuxer) (*noise.SessionTransport, error) {
					tp, err := noise.New(id, privkey, muxers)
					if err != nil {
						return nil, err
					}
					return tp.WithSessionOptions(noise.Prologue(prologue))
				},
			),
		)
	}
	if cfg.EnableQUICTransport {
		lopts = append(lopts,
			libp2p.Transport(
				func(key crypto.PrivKey, connManager *quicreuse.ConnManager, psk pnet.PSK,
					gater ccmgr.ConnectionGater,
					rcmgr network.ResourceManager,
				) (transport.Transport, error) {
					tr, err := quic.NewTransport(key, connManager, psk, gater, rcmgr)
					if err != nil {
						return nil, err
					}
					return handshake.MaybeWrapTransport(tr, quicNetCookie,
						handshake.WithLog(logger)), nil
				}),
		)
	}
	if !cfg.DisableConnectionManager {
		lopts = append(lopts, libp2p.ConnectionManager(cm))
	}
	if advAddrs := cfg.advertisedAddrs(); len(advAddrs) > 0 {
		var addrs []multiaddr.Multiaddr
		for _, addrStr := range advAddrs {
			addr, err := multiaddr.NewMultiaddr(addrStr)
			if err != nil {
				panic(err) // validated in config
			}
			addrs = append(addrs, addr)
		}
		lopts = append(
			lopts,
			libp2p.AddrsFactory(func([]multiaddr.Multiaddr) []multiaddr.Multiaddr {
				return addrs
			}),
		)
	}
	if cfg.EnableHolepunching {
		lopts = append(lopts, libp2p.EnableHolePunching())
	}
	if cfg.RelayServer.Enable {
		resources := relay.DefaultResources()
		resources.MaxReservations = cfg.RelayServer.Reservations
		resources.ReservationTTL = cfg.RelayServer.TTL
		lopts = append(lopts, libp2p.EnableRelayService(relay.WithResources(resources)))
	}
	if cfg.Relay {
		lopts = append(lopts, libp2p.EnableRelay())
	}
	if len(cfg.Relays) != 0 {
		relays, err := parseIntoAddr(cfg.Relays)
		if err != nil {
			return nil, err
		}
		lopts = append(lopts, libp2p.EnableAutoRelayWithStaticRelays(relays))
	} else {
		peerSrc, relayCh := relayPeerSource(logger)
		lopts = append(lopts, libp2p.EnableAutoRelayWithPeerSource(peerSrc))
		opts = append(opts, WithRelayCandidateChannel(relayCh))
	}
	if cfg.ForceReachability == PublicReachability {
		lopts = append(lopts, libp2p.ForceReachabilityPublic())
	} else if cfg.ForceReachability == PrivateReachability {
		lopts = append(lopts, libp2p.ForceReachabilityPrivate())
	}
	if cfg.Metrics {
		lopts = append(lopts, setupResourcesManager(cfg))
	}
	if !cfg.DisableNatPort {
		lopts = append(lopts, libp2p.NATPortMap())
	}
	if cfg.AcceptQueue != 0 {
		tptu.AcceptQueueLength = cfg.AcceptQueue
	}
	h, err := libp2p.New(lopts...)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize libp2p host: %w", err)
	}
	g.updateHost(h)
	h.Network().Notify(p2pmetrics.NewConnectionsMeeter())

	logger.Zap().Info("local node identity", zap.Stringer("identity", h.ID()))
	// TODO(dshulyak) this is small mess. refactor to avoid this patching
	// both New and Upgrade should use options.
	opts = append(
		opts,
		WithConfig(cfg),
		WithLog(logger),
		WithBootnodes(bootnodesMap),
		WithDirectNodes(directMap),
	)
	return Upgrade(h, opts...)
}

func setupResourcesManager(hostcfg Config) func(cfg *libp2p.Config) error {
	return func(cfg *libp2p.Config) error {
		rcmgr.MustRegisterWith(prometheus.DefaultRegisterer)
		str, err := rcmgr.NewStatsTraceReporter()
		if err != nil {
			return err
		}
		highPeers := hostcfg.HighPeers
		limits := rcmgr.DefaultLimits
		limits.SystemBaseLimit.ConnsInbound = highPeers
		limits.SystemBaseLimit.ConnsOutbound = highPeers
		limits.SystemBaseLimit.Conns = 2 * highPeers
		limits.SystemBaseLimit.FD = 2 * highPeers
		limits.SystemBaseLimit.StreamsInbound = 8 * highPeers
		limits.SystemBaseLimit.StreamsOutbound = 8 * highPeers
		limits.SystemBaseLimit.Streams = 16 * highPeers
		limits.ServiceBaseLimit.StreamsInbound = 8 * highPeers
		limits.ServiceBaseLimit.StreamsOutbound = 8 * highPeers
		limits.ServiceBaseLimit.Streams = 16 * highPeers

		limits.ProtocolBaseLimit.StreamsInbound = 8 * highPeers
		limits.ProtocolBaseLimit.StreamsOutbound = 8 * highPeers
		limits.ProtocolBaseLimit.Streams = 16 * highPeers
		libp2p.SetDefaultServiceLimits(&limits)

		concrete := limits.AutoScale()
		if !hostcfg.AutoscalePeers {
			concrete = limits.Scale(0, 0)
		}
		if hostcfg.DisableResourceManager {
			concrete = rcmgr.InfiniteLimits
		}
		mgr, err := rcmgr.NewResourceManager(
			rcmgr.NewFixedLimiter(concrete),
			rcmgr.WithTraceReporter(str),
		)
		if err != nil {
			return err
		}
		cfg.Apply(libp2p.ResourceManager(mgr))
		return nil
	}
}

func parseIntoAddr(nodes []string) ([]peer.AddrInfo, error) {
	var addrs []peer.AddrInfo
	for _, boot := range nodes {
		addr, err := peer.AddrInfoFromString(boot)
		if err != nil {
			return nil, fmt.Errorf("can't parse bootnode %s: %w", boot, err)
		}
		addrs = append(addrs, *addr)
	}
	return addrs, nil
}

func relayPeerSource(logger log.Logger) (autorelay.PeerSource, chan<- peer.AddrInfo) {
	relayCandidateCh := make(chan peer.AddrInfo)
	return func(ctx context.Context, num int) <-chan peer.AddrInfo {
		r := make(chan peer.AddrInfo)
		go func() {
			defer close(r)
			for ; num != 0; num-- {
				select {
				case addrInfo, ok := <-relayCandidateCh:
					if !ok {
						return
					}
					select {
					case r <- addrInfo:
						logger.With().Debug("discovered relay candidate",
							log.Stringer("addrInfo", addrInfo))
					case <-ctx.Done():
						return
					}
				case <-ctx.Done():
					return
				}
			}
		}()
		return r
	}, relayCandidateCh
}
