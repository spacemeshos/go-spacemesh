package node

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/metrics"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/handshake"
)

var relayAddrInfoCh chan peer.AddrInfo // used for testing

func runRelay(ctx context.Context, cfg *config.Config) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	p2pCfg := cfg.P2P
	p2pCfg.DataDir = filepath.Join(cfg.DataDir(), "p2p")
	p2pCfg.DisablePubSub = true
	p2pCfg.Relay = true
	p2pCfg.RelayServer.Enable = true

	lvl, err := decodeLoggerLevel(cfg, P2PLogger)
	if err != nil {
		return fmt.Errorf("failed to decode loggers: %w", err)
	}

	p2pCfg.LogLevel = lvl.Level()
	logger, err := zap.NewProduction(zap.IncreaseLevel(lvl))
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}

	if cfg.CollectMetrics {
		metrics.StartMetricsServer(cfg.MetricsPort)
	}

	types.SetLayersPerEpoch(cfg.LayersPerEpoch)
	prologue := fmt.Sprintf("%x-%v",
		cfg.Genesis.GenesisID(),
		types.GetEffectiveGenesis(),
	)
	// Prevent testnet nodes from working on the mainnet, but
	// don't use the network cookie on mainnet as this technique
	// may be replaced later
	nc := handshake.NoNetworkCookie
	if !onMainNet(cfg) {
		nc = handshake.NetworkCookie(prologue)
	}
	host, err := p2p.New(ctx, logger.Named(P2PLogger), p2pCfg, []byte(prologue), nc)
	if err != nil {
		return fmt.Errorf("initialize p2p host: %w", err)
	}
	if err := host.Start(); err != nil {
		return fmt.Errorf("error starting P2P host: %w", err)
	}
	if relayAddrInfoCh != nil {
		select {
		case relayAddrInfoCh <- peer.AddrInfo{
			ID:    host.ID(),
			Addrs: host.Addrs(),
		}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	defer host.Stop()

	<-ctx.Done()

	return nil
}
