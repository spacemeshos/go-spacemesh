package lp2p

import (
	"context"

	"github.com/libp2p/go-libp2p-core/host"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/lp2p/bootstrap"
	"github.com/spacemeshos/go-spacemesh/lp2p/handshake"
	"github.com/spacemeshos/go-spacemesh/lp2p/pubsub"
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

// Host is a conveniency wrapper for all p2p related functionality required to run
// a full spacemesh node.
type Host struct {
	cfg    Config
	logger log.Log

	host.Host
	*pubsub.PubSub
	*bootstrap.Peers
	hs        *handshake.Handshake
	bootstrap *bootstrap.Bootstrap
}

// Wrap creates Host instance from host.Host.
func Wrap(h host.Host, opts ...Opt) (*Host, error) {
	fh := &Host{
		cfg:    Default(),
		logger: log.NewNop(),
		Host:   h,
	}
	for _, opt := range opts {
		opt(fh)
	}
	cfg := fh.cfg
	var err error
	fh.PubSub, err = pubsub.New(context.Background(), fh.logger, h, pubsub.Config{
		Flood:          cfg.Flood,
		MaxMessageSize: cfg.MaxMessageSize,
	})
	if err != nil {
		return nil, err
	}
	fh.Peers = bootstrap.StartPeers(h, bootstrap.WithLog(fh.logger))
	fh.bootstrap, err = bootstrap.NewBootstrap(fh.logger, bootstrap.Config{
		DataPath:       cfg.DataPath,
		Bootstrap:      cfg.Bootstrap,
		TargetOutbound: cfg.TargetOutbound,
		Timeout:        cfg.BootstrapTimeout,
	}, h)
	if err != nil {
		return nil, err
	}
	fh.hs = handshake.New(h, cfg.NetworkID, handshake.WithLog(fh.logger))
	return fh, nil
}

// Stop background workers and release external resources.
func (fh *Host) Stop() error {
	fh.bootstrap.Stop()
	fh.Peers.Stop()
	fh.hs.Stop()
	return fh.Host.Close()
}
