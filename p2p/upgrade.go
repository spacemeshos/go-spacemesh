package p2p

import (
	"context"
	"fmt"

	"github.com/libp2p/go-libp2p/core/host"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/addressbook"
	"github.com/spacemeshos/go-spacemesh/p2p/bootstrap"
	"github.com/spacemeshos/go-spacemesh/p2p/handshake"
	"github.com/spacemeshos/go-spacemesh/p2p/peerexchange"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
)

// Opt is for configuring Host.
type Opt func(fh *Host)

// WithLog configures logger for Host.
func WithLog(logger log.Log) Opt {
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
func WithNodeReporter(reporter func()) Opt {
	return func(fh *Host) {
		fh.nodeReporter = reporter
	}
}

// Host is a conveniency wrapper for all p2p related functionality required to run
// a full spacemesh node.
type Host struct {
	ctx    context.Context
	cfg    Config
	logger log.Log

	host.Host
	*pubsub.PubSub

	nodeReporter func()
	*bootstrap.Peers

	discovery *peerexchange.Discovery
	hs        *handshake.Handshake
	bootstrap *bootstrap.Bootstrap
}

func isBootnode(h host.Host, bootnodes []string) (bool, error) {
	for _, raw := range bootnodes {
		info, err := addressbook.ParseAddrInfo(raw)
		if err != nil {
			return false, fmt.Errorf("failed to parse bootstrap node: %w", err)
		}
		if h.ID() == info.ID {
			return true, nil
		}
	}
	return false, nil
}

// Upgrade creates Host instance from host.Host.
func Upgrade(h host.Host, opts ...Opt) (*Host, error) {
	fh := &Host{
		ctx:    context.Background(),
		cfg:    DefaultConfig(),
		logger: log.NewNop(),
		Host:   h,
	}
	for _, opt := range opts {
		opt(fh)
	}
	cfg := fh.cfg
	bootnode, err := isBootnode(h, cfg.Bootnodes)
	if err != nil {
		return nil, fmt.Errorf("check node as bootnode: %w", err)
	}
	if fh.PubSub, err = pubsub.New(fh.ctx, fh.logger, h, pubsub.Config{
		Flood:          cfg.Flood,
		IsBootnode:     bootnode,
		MaxMessageSize: cfg.MaxMessageSize,
	}); err != nil {
		return nil, fmt.Errorf("failed to initialize pubsub: %w", err)
	}
	fh.Peers = bootstrap.StartPeers(h,
		bootstrap.WithLog(fh.logger),
		bootstrap.WithContext(fh.ctx),
		bootstrap.WithNodeReporter(fh.nodeReporter),
	)
	if fh.discovery, err = peerexchange.New(fh.logger, h, peerexchange.Config{
		DataDir:              cfg.DataDir,
		Bootnodes:            cfg.Bootnodes,
		CheckPeersNumber:     cfg.CheckPeersNumber,
		CheckTimeout:         cfg.CheckTimeout,
		CheckInterval:        cfg.CheckInterval,
		CheckPeersUsedBefore: cfg.CheckPeersUsedBefore,
		PeerExchange:         cfg.peerExchange,
	}); err != nil {
		return nil, fmt.Errorf("failed to initialize peerexchange discovery: %w", err)
	}
	if fh.bootstrap, err = bootstrap.NewBootstrap(fh.logger, bootstrap.Config{
		TargetOutbound: cfg.TargetOutbound,
		Timeout:        cfg.BootstrapTimeout,
	}, fh, fh.discovery); err != nil {
		return nil, fmt.Errorf("failed to initiliaze bootstrap: %w", err)
	}
	fh.hs = handshake.New(fh, cfg.NetworkID, handshake.WithLog(fh.logger))
	return fh, nil
}

// Stop background workers and release external resources.
func (fh *Host) Stop() error {
	fh.discovery.Stop()
	fh.bootstrap.Stop()
	fh.Peers.Stop()
	fh.hs.Stop()
	if err := fh.Host.Close(); err != nil {
		return fmt.Errorf("failed to close libp2p host: %w", err)
	}
	return nil
}
