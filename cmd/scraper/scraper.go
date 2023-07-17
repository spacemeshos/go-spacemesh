package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/peerexchange"
	"go.uber.org/zap"
)

var (
	dir      = flag.String("dir", "", "directory with peers.txt")
	prologue = flag.String("prologue", "9eebff023abb17ccb775c602daade8ed708f0a50-8063", "prologue is generated from genesis id and effective genesis")
	level    = zap.LevelFlag("level", zap.WarnLevel, "set level for logger")
)

func main() {
	flag.Parse()

	logger := log.NewWithLevel("scraper", zap.NewAtomicLevelAt(*level))
	cfg := p2p.DefaultConfig()
	cfg.DataDir = *dir
	cfg.DisableNatPort = true
	cfg.DisableReusePort = true
	h, err := p2p.New(context.Background(), logger, cfg, []byte(*prologue))
	if err != nil {
		fmt.Printf("p2p host: %s\n", err)
		os.Exit(1)
	}
	disccfg := peerexchange.Config{
		DataDir:        *dir,
		FastCrawl:      1 * time.Second,
		SlowCrawl:      1 * time.Second,
		SlowConcurrent: 100,
		FastConcurrent: 100,
	}
	disc, err := peerexchange.New(logger, h, disccfg)
	if err != nil {
		fmt.Printf("discovery: %s\n", err)
		os.Exit(1)
	}
	disc.StartScan()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	ticker := time.NewTicker(5 * time.Second)
	for {
		select {
		case <-ctx.Done():
			disc.Stop()
			stats := disc.Stats()
			fmt.Printf("%+v\n", stats)
			return
		case <-ticker.C:
			stats := disc.Stats()
			fmt.Printf("%+v\n", stats)
		}
	}
}
