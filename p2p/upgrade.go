package p2p

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"

	"github.com/spacemeshos/go-spacemesh/log"
	discovery "github.com/spacemeshos/go-spacemesh/p2p/dhtdiscovery"
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

	closed struct {
		sync.Mutex
		closed bool
	}

	host.Host
	*pubsub.PubSub

	nodeReporter func()

	discovery *discovery.Discovery
	legacy    *peerexchange.Discovery
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
	bootnodes, err := parseIntoAddr(fh.cfg.Bootnodes)
	if err != nil {
		return nil, err
	}
	if fh.PubSub, err = pubsub.New(fh.ctx, fh.logger, h, pubsub.Config{
		Flood:          cfg.Flood,
		IsBootnode:     cfg.Bootnode,
		Bootnodes:      bootnodes,
		MaxMessageSize: cfg.MaxMessageSize,
	}); err != nil {
		return nil, fmt.Errorf("failed to initialize pubsub: %w", err)
	}
	if !cfg.DisableLegacyDiscovery {
		if fh.legacy, err = peerexchange.New(fh.logger, h, peerexchange.Config{
			DataDir:          cfg.DataDir,
			Bootnodes:        cfg.Bootnodes,
			AdvertiseAddress: cfg.AdvertiseAddress,
			MinPeers:         cfg.MinPeers,
			SlowCrawl:        10 * time.Minute,
			FastCrawl:        10 * time.Second,
		}); err != nil {
			return nil, fmt.Errorf("failed to initialize peerexchange discovery: %w", err)
		}
	}

	dopts := []discovery.Opt{
		discovery.WithDir(cfg.DataDir),
		discovery.WithBootnodes(bootnodes),
	}
	if cfg.Bootnode {
		dopts = append(dopts, discovery.Server())
	}
	if cfg.PrivateNetwork {
		dopts = append(dopts, discovery.Private())
	}
	dhtdisc, err := discovery.New(fh, dopts...)
	if err != nil {
		return nil, err
	}
	fh.discovery = dhtdisc
	if fh.nodeReporter != nil {
		fh.Network().Notify(&network.NotifyBundle{
			ConnectedF: func(network.Network, network.Conn) {
				fh.nodeReporter()
			},
			DisconnectedF: func(network.Network, network.Conn) {
				fh.nodeReporter()
			},
		})
	}
	return fh, nil
}

// GetPeers returns connected peers.
func (fh *Host) GetPeers() []Peer {
	return fh.Host.Network().Peers()
}

// PeerCount returns number of connected peers.
func (fh *Host) PeerCount() uint64 {
	return uint64(len(fh.Host.Network().Peers()))
}

func (fh *Host) Start() error {
	fh.closed.Lock()
	defer fh.closed.Unlock()
	if fh.closed.closed {
		return errors.New("p2p: closed")
	}
	if fh.legacy != nil {
		fh.legacy.StartScan()
	}
	fh.discovery.Start()
	return nil
}

// Stop background workers and release external resources.
func (fh *Host) Stop() error {
	fh.closed.Lock()
	defer fh.closed.Unlock()
	if fh.closed.closed {
		return errors.New("p2p: closed")
	}
	fh.closed.closed = true
	if fh.legacy != nil {
		fh.legacy.Stop()
	}
	if err := fh.Host.Close(); err != nil {
		return fmt.Errorf("failed to close libp2p host: %w", err)
	}
	fh.discovery.Stop()
	return nil
}
