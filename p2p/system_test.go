package p2p

import (
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"testing"
)

func TestBasicP2P(t *testing.T) {
	bootcfg := config.DefaultConfig()
	bootcfg.SwarmConfig.Bootstrap = false
	bootcfg.SwarmConfig.Gossip = false
	bootNodesApp := NewP2PApp(t, 3, bootcfg)
	bootNodesApp.Start()

	cfg := config.DefaultConfig()
	cfg.SwarmConfig.Bootstrap = true
	cfg.SwarmConfig.Gossip = false
	cfg.SwarmConfig.BootstrapNodes = bootNodesApp.StringIdentifiers()
	p2pApp := NewP2PApp(t, 20, cfg)
	p2pApp.Start()
	p2pApp.WaitForBoot()
}