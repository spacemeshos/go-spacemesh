package p2p

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	lp2plog "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"go.uber.org/zap/zapcore"
	"golang.org/x/sync/errgroup"

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

// Host is a conveniency wrapper for all p2p related functionality required to run
// a full spacemesh node.
type Host struct {
	eg     errgroup.Group
	ctx    context.Context
	cancel context.CancelFunc

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

	direct, bootnode map[peer.ID]struct{}
}

// Upgrade creates Host instance from host.Host.
func Upgrade(h host.Host, opts ...Opt) (*Host, error) {
	ctx, cancel := context.WithCancel(context.Background())
	fh := &Host{
		ctx:    ctx,
		cancel: cancel,
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
	direct, err := parseIntoAddr(fh.cfg.Direct)
	if err != nil {
		return nil, err
	}
	for _, peer := range direct {
		h.ConnManager().Protect(peer.ID, "direct")
	}
	if fh.PubSub, err = pubsub.New(fh.ctx, fh.logger, h, pubsub.Config{
		Flood:          cfg.Flood,
		IsBootnode:     cfg.Bootnode,
		Direct:         direct,
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
		discovery.WithMinPeers(cfg.MinPeers),
		discovery.WithHighPeers(cfg.HighPeers),
		discovery.WithDir(cfg.DataDir),
		discovery.WithBootnodes(bootnodes),
		discovery.WithDirect(direct),
		discovery.WithLogger(fh.logger.Zap()),
	}
	if cfg.PrivateNetwork {
		dopts = append(dopts, discovery.Private())
	}
	if cfg.DisableDHT {
		dopts = append(dopts, discovery.DisableDHT())
	}
	if cfg.Bootnode {
		dopts = append(dopts, discovery.Server())
	} else {
		backup, err := loadPeers(cfg.DataDir)
		if err != nil {
			fh.logger.With().Warning("failed to to load backup peers", log.Err(err))
		} else if len(backup) > 0 {
			dopts = append(dopts, discovery.WithBackup(backup))
		}
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

// ConnectedPeerInfo retrieves a peer info object for the given peer.ID, if the
// given peer is not connected then nil is returned.
func (fh *Host) ConnectedPeerInfo(id peer.ID) *PeerInfo {
	conns := fh.Network().ConnsToPeer(id)
	// there's no sync between  Peers() and ConnsToPeer() so by the time we
	// try to get the conns they may not exist.
	if len(conns) == 0 {
		return nil
	}

	var connections []ConnectionInfo
	for _, c := range conns {
		connections = append(connections, ConnectionInfo{
			Address:  c.RemoteMultiaddr(),
			Uptime:   time.Since(c.Stat().Opened),
			Outbound: c.Stat().Direction == network.DirOutbound,
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
		Tags:        tags,
	}
}

// PeerCount returns number of connected peers.
func (fh *Host) PeerCount() uint64 {
	return uint64(len(fh.Host.Network().Peers()))
}

// PeerProtocols returns the protocols supported by peer.
func (fh *Host) PeerProtocols(p Peer) ([]protocol.ID, error) {
	return fh.Peerstore().GetProtocols(p)
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
	if !fh.cfg.Bootnode {
		fh.eg.Go(func() error {
			persist(fh.ctx, fh.logger, fh.Host, fh.cfg.DataDir, 30*time.Minute)
			return nil
		})
	}
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
	if fh.legacy != nil {
		fh.legacy.Stop()
	}
	fh.discovery.Stop()
	fh.eg.Wait()
	if err := fh.Host.Close(); err != nil {
		return fmt.Errorf("failed to close libp2p host: %w", err)
	}
	lp2plog.SetPrimaryCore(zapcore.NewNopCore())
	return nil
}
