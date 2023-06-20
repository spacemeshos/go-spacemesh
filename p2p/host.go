package p2p

import (
	"context"
	"fmt"
	"time"

	lp2plog "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/core/transport"
	"github.com/libp2p/go-libp2p/p2p/host/peerstore/pstoremem"
	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
	rcmgrObs "github.com/libp2p/go-libp2p/p2p/host/resource-manager/obs"
	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	tptu "github.com/libp2p/go-libp2p/p2p/net/upgrader"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/log"
	p2pmetrics "github.com/spacemeshos/go-spacemesh/p2p/metrics"
)

// DefaultConfig config.
func DefaultConfig() Config {
	return Config{
		Listen:             "/ip4/0.0.0.0/tcp/7513",
		Flood:              false,
		MinPeers:           6,
		LowPeers:           40,
		HighPeers:          100,
		GracePeersShutdown: 30 * time.Second,
		MaxMessageSize:     2 << 20,
		AcceptQueue:        tptu.AcceptQueueLength,
	}
}

// Config for all things related to p2p layer.
type Config struct {
	DataDir            string
	LogLevel           log.Level
	GracePeersShutdown time.Duration
	MaxMessageSize     int

	// see https://lwn.net/Articles/542629/ for reuseport explanation
	DisableReusePort bool     `mapstructure:"disable-reuseport"`
	DisableNatPort   bool     `mapstructure:"disable-natport"`
	Flood            bool     `mapstructure:"flood"`
	Listen           string   `mapstructure:"listen"`
	Bootnodes        []string `mapstructure:"bootnodes"`
	MinPeers         int      `mapstructure:"min-peers"`
	LowPeers         int      `mapstructure:"low-peers"`
	HighPeers        int      `mapstructure:"high-peers"`
	AdvertiseAddress string   `mapstructure:"advertise-address"`
	AcceptQueue      int      `mapstructure:"p2p-accept-queue"`
	Metrics          bool     `mapstructure:"p2p-metrics"`
}

// New initializes libp2p host configured for spacemesh.
func New(_ context.Context, logger log.Log, cfg Config, prologue []byte, opts ...Opt) (*Host, error) {
	logger.Zap().Info("starting libp2p host", zap.Any("config", &cfg))
	key, err := EnsureIdentity(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	lp2plog.SetPrimaryCore(logger.Core())
	lp2plog.SetAllLoggers(lp2plog.LogLevel(cfg.LogLevel))
	cm, err := connmgr.NewConnManager(cfg.LowPeers, cfg.HighPeers, connmgr.WithGracePeriod(cfg.GracePeersShutdown))
	if err != nil {
		return nil, fmt.Errorf("p2p create conn mgr: %w", err)
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

		libp2p.Transport(func(upgrader transport.Upgrader, rcmgr network.ResourceManager) (transport.Transport, error) {
			opts := []tcp.Option{}
			if cfg.DisableReusePort {
				opts = append(opts, tcp.DisableReuseport())
			}
			if cfg.Metrics {
				opts = append(opts, tcp.WithMetrics())
			}
			return tcp.NewTCPTransport(upgrader, rcmgr, opts...)
		}),
		libp2p.Security(noise.ID, func(id protocol.ID, privkey crypto.PrivKey, muxers []tptu.StreamMuxer) (*noise.SessionTransport, error) {
			tp, err := noise.New(id, privkey, muxers)
			if err != nil {
				return nil, err
			}
			return tp.WithSessionOptions(noise.Prologue(prologue))
		}),
		libp2p.Muxer("/yamux/1.0.0", &streamer),

		libp2p.ConnectionManager(cm),
		libp2p.Peerstore(ps),
		libp2p.BandwidthReporter(p2pmetrics.NewBandwidthCollector()),
	}
	if cfg.Metrics {
		lopts = append(lopts, setupResourcesManager)
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
	h.Network().Notify(p2pmetrics.NewConnectionsMeeter())

	logger.Zap().Info("local node identity", zap.Stringer("identity", h.ID()))
	// TODO(dshulyak) this is small mess. refactor to avoid this patching
	// both New and Upgrade should use options.
	opts = append(opts, WithConfig(cfg), WithLog(logger))
	return Upgrade(h, opts...)
}

func setupResourcesManager(cfg *libp2p.Config) error {
	rcmgrObs.MustRegisterWith(prometheus.DefaultRegisterer)
	str, err := rcmgrObs.NewStatsTraceReporter()
	if err != nil {
		return err
	}
	limits := rcmgr.DefaultLimits
	libp2p.SetDefaultServiceLimits(&limits)

	mgr, err := rcmgr.NewResourceManager(
		rcmgr.NewFixedLimiter(limits.AutoScale()),
		rcmgr.WithTraceReporter(str),
	)
	if err != nil {
		return err
	}
	cfg.Apply(libp2p.ResourceManager(mgr))
	return nil
}
