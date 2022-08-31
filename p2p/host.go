package p2p

import (
	"context"
	"fmt"
	"time"

	lp2plog "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/host/peerstore/pstoremem"
	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"

	"github.com/spacemeshos/go-spacemesh/log"
	p2pmetrics "github.com/spacemeshos/go-spacemesh/p2p/metrics"
	"github.com/spacemeshos/go-spacemesh/p2p/peerexchange"
)

// DefaultConfig config.
func DefaultConfig() Config {
	return Config{
		Listen:               "/ip4/0.0.0.0/tcp/7513",
		Flood:                true,
		TargetOutbound:       5,
		LowPeers:             40,
		HighPeers:            100,
		GracePeersShutdown:   30 * time.Second,
		BootstrapTimeout:     10 * time.Second,
		MaxMessageSize:       200 << 10,
		CheckInterval:        3 * time.Minute,
		CheckTimeout:         30 * time.Second,
		CheckPeersNumber:     10,
		CheckPeersUsedBefore: 30 * time.Minute,
	}
}

// Config for all things related to p2p layer.
type Config struct {
	DataDir            string
	LogLevel           log.Level
	GracePeersShutdown time.Duration
	BootstrapTimeout   time.Duration
	MaxMessageSize     int

	DisableNatPort bool     `mapstructure:"disable-natport"`
	Flood          bool     `mapstructure:"flood"`
	Listen         string   `mapstructure:"listen"`
	NetworkID      uint32   `mapstructure:"network-id"`
	Bootnodes      []string `mapstructure:"bootnodes"`
	TargetOutbound int      `mapstructure:"target-outbound"`
	LowPeers       int      `mapstructure:"low-peers"`
	HighPeers      int      `mapstructure:"high-peers"`

	// Discovery book check section.
	CheckInterval        time.Duration
	CheckTimeout         time.Duration
	CheckPeersNumber     int
	CheckPeersUsedBefore time.Duration
}

// New initializes libp2p host configured for spacemesh.
func New(_ context.Context, logger log.Log, cfg Config, opts ...Opt) (*Host, error) {
	logger.Info("starting libp2p host with config %+v", cfg)
	key, err := EnsureIdentity(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	// what we set in cfg.LogLevel will not be used
	// unless level of the Core is atleast as high
	lp2plog.SetPrimaryCore(logger.Core())
	lp2plog.SetAllLoggers(lp2plog.LogLevel(cfg.LogLevel))

	cm, err := connmgr.NewConnManager(cfg.LowPeers, cfg.HighPeers, connmgr.WithGracePeriod(cfg.GracePeersShutdown))
	if err != nil {
		return nil, fmt.Errorf("p2p create conn mgr: %w", err)
	}
	// TODO(dshulyak) remove this part
	for _, p := range cfg.Bootnodes {
		addr, err := peer.AddrInfoFromString(p)
		if err != nil {
			return nil, fmt.Errorf("can't create peer addr from %s: %w", p, err)
		}
		cm.Protect(addr.ID, peerexchange.BootNodeTag)
	}
	streamer := *yamux.DefaultTransport
	ps, err := pstoremem.NewPeerstore()
	if err != nil {
		return nil, fmt.Errorf("can't create peer store: %w", err)
	}
	lopts := []libp2p.Option{
		libp2p.Identity(key),
		libp2p.ListenAddrStrings(cfg.Listen),
		libp2p.UserAgent("go-spacemesh"),
		libp2p.DisableRelay(),

		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.Security(noise.ID, noise.New),
		libp2p.Muxer("/yamux/1.0.0", &streamer),

		libp2p.ConnectionManager(cm),
		libp2p.Peerstore(ps),
		libp2p.BandwidthReporter(p2pmetrics.NewBandwidthCollector()),
	}
	if !cfg.DisableNatPort {
		lopts = append(lopts, libp2p.NATPortMap())
	}
	h, err := libp2p.New(lopts...)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize libp2p host: %w", err)
	}
	h.Network().Notify(p2pmetrics.NewConnectionsMeeter())

	logger.With().Info("local node identity",
		log.String("identity", h.ID().String()),
	)
	// TODO(dshulyak) this is small mess. refactor to avoid this patching
	// both New and Upgrade should use options.
	opts = append(opts, WithConfig(cfg), WithLog(logger))
	return Upgrade(h, opts...)
}
