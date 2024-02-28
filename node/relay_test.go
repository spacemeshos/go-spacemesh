package node

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	relayclient "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

const relayConf = `{
  "p2p":{
    "listen": "/ip4/127.0.0.1/tcp/0",
    "bootnodes": [],
    "p2p-reachability": "public"
  }
}`

func TestRelay(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(configPath, []byte(relayConf), 0o600))

	relayAddrInfoCh = make(chan peer.AddrInfo)
	t.Cleanup(func() { relayAddrInfoCh = nil })

	cmd := GetCommand()
	cmd.SetArgs([]string{"relay", "--config", configPath})
	var eg errgroup.Group
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eg.Go(func() error {
		defer cancel()
		return cmd.ExecuteContext(ctx)
	})

	var addrInfo peer.AddrInfo
	select {
	case addrInfo = <-relayAddrInfoCh:
	case <-ctx.Done():
		err := eg.Wait()
		require.Error(t, err)
		t.Fatalf("stopped with error: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatalf("timed out waiting for addresses")
	}

	p2pCfg := p2p.DefaultConfig()
	p2pCfg.DataDir = t.TempDir()
	p2pCfg.Listen = p2p.MustParseAddresses("/ip4/127.0.0.1/tcp/0")
	p2pCfg.Relay = true
	p2pCfg.IP4Blocklist = nil
	p2pCfg.ForceReachability = "private"
	// mainnet Noise prologue
	prologue := []byte("9eebff023abb17ccb775c602daade8ed708f0a50-8063")
	host, err := p2p.New(context.Background(), logtest.New(t), p2pCfg, prologue, nil)
	require.NoError(t, err)
	t.Cleanup(func() { host.Stop() })

	require.NoError(t, host.Connect(ctx, addrInfo))
	_, err = relayclient.Reserve(ctx, host, addrInfo)
	require.NoError(t, err)

	cancel()
	require.NoError(t, eg.Wait())
}
