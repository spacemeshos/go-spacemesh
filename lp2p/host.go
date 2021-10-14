package lp2p

import (
	"context"
	"fmt"
	"time"

	"github.com/btcsuite/btcutil/base58"
	lp2plog "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p"
	connmgr "github.com/libp2p/go-libp2p-connmgr"
	"github.com/libp2p/go-libp2p-core/peer"
	mplex "github.com/libp2p/go-libp2p-mplex"
	noise "github.com/libp2p/go-libp2p-noise"
	"github.com/libp2p/go-libp2p-peerstore/pstoremem"
	"github.com/libp2p/go-tcp-transport"

	"github.com/spacemeshos/go-spacemesh/log"
)

// Peer is an alias to libp2p's peer.ID.
type Peer = peer.ID

// Default config.
func Default() Config {
	return Config{
		Listen:             "/ip4/0.0.0.0/tcp/7513",
		Flood:              true,
		TargetOutbound:     5,
		LowPeers:           40,
		HighPeers:          100,
		GracePeersShutdown: 30 * time.Second,
		BootstrapTimeout:   10 * time.Second,
		MaxMessageSize:     200 << 10,
	}
}

// Config for all things related to p2p layer.
type Config struct {
	DataDir            string
	GracePeersShutdown time.Duration
	BootstrapTimeout   time.Duration
	MaxMessageSize     int

	Flood          bool     `mapstructure:"flood"`
	Listen         string   `mapstructure:"listen"`
	NetworkID      uint32   `mapstructure:"network-id"`
	Bootnodes      []string `mapstructure:"bootnodes"`
	TargetOutbound int      `mapstructure:"target-outbound"`
	LowPeers       int      `mapstructure:"low-peers"`
	HighPeers      int      `mapstructure:"high-peers"`
}

// New initializes libp2p host configured for spacemesh.
func New(ctx context.Context, logger log.Log, cfg Config, opts ...Opt) (*Host, error) {
	logger.Info("starting libp2p host with config %+v", cfg)
	key, err := ensureIdentity(cfg.DataDir)
	if err != nil {
		return nil, err
	}

	cm := connmgr.NewConnManager(cfg.LowPeers, cfg.HighPeers, cfg.GracePeersShutdown)
	for _, p := range cfg.Bootnodes {
		addr, err := peer.AddrInfoFromString(p)
		if err != nil {
			return nil, fmt.Errorf("can't create peer addr from %s: %w", p, err)
		}
		cm.Protect(addr.ID, "bootstrap")
	}
	h, err := libp2p.New(ctx,
		libp2p.Identity(key),
		libp2p.ListenAddrStrings(cfg.Listen),
		libp2p.Ping(true),
		libp2p.UserAgent("go-spacemesh"),
		libp2p.NATPortMap(),
		libp2p.DisableRelay(),

		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.Muxer("/mplex/6.7.0", mplex.DefaultTransport),
		libp2p.Security(noise.ID, noise.New),

		libp2p.ConnectionManager(cm),
		libp2p.Peerstore(pstoremem.NewPeerstore()),
	)
	if err != nil {
		return nil, err
	}
	pub, err := key.GetPublic().Raw()
	if err != nil {
		return nil, err
	}
	lp2plog.SetPrimaryCore(logger.Core())

	logger.With().Info("local node identity",
		log.String("key", base58.Encode(pub)),
		log.String("identity", h.ID().String()),
	)
	// TODO(dshulyak) this is small mess. refactor to avoid this patching
	opts = append(opts, WithConfig(cfg), WithLog(logger))
	return Wrap(h, opts...)
}
