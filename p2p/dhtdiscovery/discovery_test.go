package discovery

import (
	"fmt"
	"testing"
	"time"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

type discHost struct {
	host.Host
	needPeerDiscovery bool
	haveRelay         bool
}

var _ DiscoveryHost = &discHost{}

func makeDiscHost(h host.Host, needPeerDiscovery, haveRelay bool) *discHost {
	return &discHost{
		Host:              h,
		needPeerDiscovery: needPeerDiscovery,
		haveRelay:         haveRelay,
	}
}

func (h *discHost) NeedPeerDiscovery() bool { return h.needPeerDiscovery }
func (h *discHost) HaveRelay() bool         { return h.haveRelay }

func TestSanity(t *testing.T) {
	mock, err := mocknet.FullMeshLinked(7)
	for n, h := range mock.Hosts() {
		t.Logf("Host %d: %s", n, h.ID())
	}
	require.NoError(t, err)
	discs := make([]*Discovery, len(mock.Hosts()))
	t.Cleanup(func() {
		for _, disc := range discs {
			disc.Stop()
		}
	})
	// Let the bootnode have suspended peer discovery.
	// It knows all the nodes anyway. Also, do not advertise
	// it for routing discovery as all the nodes know about
	// their bootnode, too.
	boot := makeDiscHost(mock.Hosts()[0], false, false)
	logger := logtest.New(t).Zap()
	bootdisc, err := New(boot,
		WithPeriod(100*time.Microsecond),
		EnableRoutingDiscovery(),
		Private(),
		WithLogger(logger),
		WithMode(dht.ModeServer),
		WithFindPeersRetryDelay(1*time.Second),
	)
	require.NoError(t, err)
	bootdisc.Start()
	defer bootdisc.Stop()
	discs[0] = bootdisc
	require.NoError(t, err)
	relayChans := make([]chan peer.AddrInfo, len(mock.Hosts()))
	nodeOpts := []struct {
		// Nodes that run DHT in client mode (/.../kad/... protocol not supported)
		// are much harder to come by without enabling routing discovery
		dhtMode       dht.ModeOpt
		relayService  bool
		lookForRelays bool
	}{
		{
			// 1
			dhtMode:      dht.ModeServer,
			relayService: true,
		},
		{
			// 2
			dhtMode:       dht.ModeServer,
			lookForRelays: true,
		},
		{
			// 3
			dhtMode:       dht.ModeServer,
			lookForRelays: true,
		},
		{
			// 4
			dhtMode:       dht.ModeServer,
			lookForRelays: true,
		},
		{
			// 5
			dhtMode:       dht.ModeClient,
			lookForRelays: true,
		},
		{
			// 6
			dhtMode:       dht.ModeClient,
			lookForRelays: true,
		},
	}
	for i, h := range mock.Hosts()[1:] {
		opts := []Opt{
			Private(),
			WithLogger(logger),
			WithBootnodes([]peer.AddrInfo{{ID: boot.ID(), Addrs: boot.Addrs()}}),
			EnableRoutingDiscovery(),
			AdvertiseForPeerDiscovery(),
			WithMode(nodeOpts[i].dhtMode),
			WithAdvertiseDelay(10 * time.Millisecond),
			WithAdvertiseInterval(1 * time.Minute),
			WithAdvertiseRetryDelay(1 * time.Second),
			WithFindPeersRetryDelay(1 * time.Second),
		}
		if nodeOpts[i].lookForRelays {
			relayCh := make(chan peer.AddrInfo)
			relayChans[1+i] = relayCh
			opts = append(opts, WithRelayCandidateChannel(relayCh))
		}
		disc, err := New(makeDiscHost(h, true, nodeOpts[i].relayService), opts...)
		require.NoError(t, err)
		disc.Start()
		defer disc.Stop()
		discs[1+i] = disc
	}
	require.Eventually(t, func() bool {
		for _, h := range mock.Hosts() {
			if len(h.Network().Peers()) != len(mock.Hosts())-1 {
				return false
			}
		}
		return true
	}, 15*time.Second, 100*time.Millisecond)

	// Wait till every node looking for relays have discovered
	// the node 1 which has relay service enabled
	var eg errgroup.Group
	for n, ch := range relayChans {
		if ch == nil {
			continue
		}
		eg.Go(func() error {
			select {
			case p := <-ch:
				require.Equal(t, mock.Hosts()[1].ID(), p.ID)
			case <-time.After(3 * time.Second):
				return fmt.Errorf("timed out waiting for relay id for peer %d", n)
			}
			return nil
		})
	}

	require.NoError(t, eg.Wait())
}
