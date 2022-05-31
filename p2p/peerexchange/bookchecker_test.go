package peerexchange

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	connmgr "github.com/libp2p/go-libp2p-connmgr"
	"github.com/libp2p/go-libp2p-core/host"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func TestDiscovery_CheckBook(t *testing.T) {
	t.Parallel()
	t.Run("network broken with big number of nodes", func(t *testing.T) {
		t.Parallel()
		mesh, instances := setupOverMockNet(t, 20)

		totalAddressesBefore := calcTotalAddresses(instances)
		totalPeerAddressesBefore := calcTotalAddressesPeerStore(instances)

		brokeMesh(t, mesh)
		time.Sleep(3 * time.Second)
		checkBooks(instances)

		totalAddressesAfter := calcTotalAddresses(instances)
		totalPeerAddressesAfter := calcTotalAddressesPeerStore(instances)

		require.NotEqual(t, totalAddressesBefore, totalAddressesAfter, "total addresses should be different")
		require.NotEqual(t, totalPeerAddressesBefore, totalPeerAddressesAfter, "total addresses should be different")
		require.True(t, totalAddressesBefore > totalAddressesAfter, "total addresses after break should be smaller")
		require.True(t, totalPeerAddressesBefore > totalPeerAddressesAfter, "total addresses after break should be smaller")
	})
	t.Run("with small number of nodes", func(t *testing.T) {
		t.Parallel()
		mesh, instances := setupOverMockNet(t, 5)

		totalAddressesBefore := calcTotalAddresses(instances)
		totalPeerAddressesBefore := calcTotalAddressesPeerStore(instances)
		require.True(t, totalAddressesBefore > 0, "total addresses before break should be greater than 0")
		require.True(t, totalPeerAddressesBefore > 0, "total addresses before break should be greater than 0")

		brokeMesh(t, mesh)
		time.Sleep(3 * time.Second)
		checkBooks(instances)

		totalAddressesAfter := calcTotalAddresses(instances)
		totalPeerAddressesAfter := calcTotalAddressesPeerStore(instances)
		require.Equal(t, 0, totalAddressesAfter, "total addresses after break should be zero")
		require.Equal(t, 0, totalPeerAddressesAfter, "total addresses after break should be zero")
	})
}

func TestDiscovery_GetRandomPeers(t *testing.T) {
	t.Parallel()

	hosts, instances := setupOverNodes(t, 2)
	d := instances[0]

	bootNodeAddress, err := parseAddrInfo("/dns4/sample.spacemesh.io/tcp/5003/p2p/12D3KooWGQrF3pHrR1W7P6nh8gypYxtFS93SnmvtN6qpyeSo7T2u")
	require.NoError(t, err)
	sampleNode, err := parseAddrInfo("/dns4/sample.spacemesh.io/tcp/5003/p2p/12D3KooWBdbwmiMhLDzAbfY3Vy5RDGaumUEgHe2P1pL5G3dhhWMb")
	require.NoError(t, err)
	oldNode, err := parseAddrInfo("/dns4/sample.spacemesh.io/tcp/5003/p2p/12D3KooWJSLApvoWiX9q3oKkYQTXpxQC7qAKt6mTERCJuwCFT98d")
	require.NoError(t, err)
	hostConnected := hosts[1].Network().ListenAddresses()[0].String() + "/p2p/" + hosts[1].ID().String()
	addr, err := ma.NewMultiaddr(hostConnected)
	require.NoError(t, err)
	connectedNodeAddress := &addrInfo{ID: hosts[1].ID(), RawAddr: hostConnected, addr: addr}

	t.Run("empty peers", func(t *testing.T) {
		time.Sleep(100 * time.Millisecond)
		require.Equal(t, 0, d.book.NumAddresses())
		res := d.GetRandomPeers(10)
		require.Equal(t, 0, len(res), "should return 2 peers")
	})

	best, err := bestHostAddress(d.host)
	require.NoError(t, err)
	// populate address book with some addresses.
	d.book.AddAddress(bootNodeAddress, best)
	d.book.AddAddress(sampleNode, best)
	d.book.AddAddress(oldNode, best)
	d.book.AddAddress(connectedNodeAddress, best)

	t.Run("check all nodes in book", func(t *testing.T) {
		time.Sleep(3 * time.Second)
		res := d.GetRandomPeers(10)
		require.Equal(t, 4, len(res), "should return 4 peers")
	})

	wrapDiscovery(instances)

	t.Run("return all except connected node", func(t *testing.T) {
		time.Sleep(3 * time.Second)
		require.Equal(t, 4, d.book.NumAddresses())
		res := d.GetRandomPeers(10)
		require.Equal(t, 3, len(res), "should return 3 peers")
		require.NotContains(t, res, connectedNodeAddress, "should not return connected node")
	})

	t.Run("return all except connected node and bootnode", func(t *testing.T) {
		d.host.ConnManager().Protect(bootNodeAddress.ID, BootNodeTag)
		defer d.host.ConnManager().Unprotect(bootNodeAddress.ID, BootNodeTag)
		time.Sleep(3 * time.Second)
		require.Equal(t, 4, d.book.NumAddresses())
		res := d.GetRandomPeers(10)
		require.Equal(t, 2, len(res), "should return 2 peers")
		require.NotContains(t, res, connectedNodeAddress, "should not return connected node")
		require.NotContains(t, res, bootNodeAddress, "should not return bootnode")
	})

	t.Run("check last usage address", func(t *testing.T) {
		time.Sleep(3 * time.Second)
		// trigger update last usage date
		d.book.AddAddress(oldNode, best)
		require.Equal(t, 4, d.book.NumAddresses())
		time.Sleep(100 * time.Millisecond)
		res := d.GetRandomPeers(10)
		require.Equal(t, 2, len(res), "should return 3 peers")
		require.NotContains(t, res, oldNode, "should not return connected node")

		time.Sleep(3 * time.Second)
		res = d.GetRandomPeers(10)
		require.Contains(t, res, oldNode, "should not return old node")
		require.Equal(t, 3, len(res), "should return 3 peers")
	})
}

func setupOverNodes(t *testing.T, nodesCount int) ([]host.Host, []*Discovery) {
	hosts := make([]host.Host, 0, nodesCount)
	for i := 0; i < nodesCount; i++ {
		cm := connmgr.NewConnManager(40, 100, 30*time.Second)
		h, err := libp2p.New(context.Background(), libp2p.ConnectionManager(cm))
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, h.Close())
		})
		hosts = append(hosts, h)
	}
	instances := setup(t, hosts)
	return hosts, instances
}

func setupOverMockNet(t *testing.T, nodesCount int) (mocknet.Mocknet, []*Discovery) {
	mesh, err := mocknet.FullMeshConnected(context.TODO(), nodesCount)
	require.NoError(t, err)
	instances := setup(t, mesh.Hosts())
	wrapDiscovery(instances)
	return mesh, instances
}

func setup(t *testing.T, hosts []host.Host) []*Discovery {
	var (
		instances []*Discovery
		bootnode  *addrInfo
	)

	for _, h := range hosts {
		logger := logtest.New(t).Named(h.ID().Pretty())
		cfg := Config{
			CheckPeersUsedBefore: 2 * time.Second,
			CheckTimeout:         30 * time.Second,
			CheckPeersNumber:     10,
		}

		best, err := bestHostAddress(h)
		require.NoError(t, err)
		if bootnode == nil {
			bootnode = best
		} else {
			cfg.Bootnodes = append(cfg.Bootnodes, bootnode.RawAddr)
		}

		instance, err := New(logger, h, cfg)
		require.NoError(t, err)
		t.Cleanup(instance.Stop)
		instances = append(instances, instance)
	}

	return instances
}

func wrapDiscovery(instances []*Discovery) {
	rounds := 5
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	for _, instance := range instances {
		wg.Add(1)
		go func(instance *Discovery) {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				if err := instance.Bootstrap(ctx); errors.Is(err, context.Canceled) {
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
		}(instance)
	}
	wg.Wait()
}

// brokeMesh simulate that all peers are disconnected and the mesh is broken.
func brokeMesh(t *testing.T, mesh mocknet.Mocknet) {
	for _, h := range mesh.Hosts() {
		for _, hh := range mesh.Hosts() {
			if h.ID() == hh.ID() {
				continue
			}
			require.NoError(t, mesh.UnlinkPeers(h.ID(), hh.ID()))
			require.NoError(t, mesh.DisconnectPeers(h.ID(), hh.ID()))
			// require.NoError(t, mesh.UnlinkNets(h.Network(), hh.Network()))
			// require.NoError(t, mesh.DisconnectNets(h.Network(), hh.Network()))
		}
	}
}

// checkBook trigger book check in all instances.
func checkBooks(instances []*Discovery) {
	var wg sync.WaitGroup
	// run checkbook
	for _, instance := range instances {
		wg.Add(1)
		go func(inst *Discovery) {
			defer wg.Done()
			inst.CheckPeers(context.Background())
		}(instance)
	}
	wg.Wait()
}

func calcTotalAddresses(instances []*Discovery) int {
	totalAddresses := 0
	for _, instance := range instances {
		totalAddresses += len(instance.book.getAddresses())
	}
	return totalAddresses
}

func calcTotalAddressesPeerStore(instances []*Discovery) int {
	totalAddresses := 0
	for _, instance := range instances {
		for _, p := range instance.host.Peerstore().Peers() {
			if p.String() == instance.host.ID().String() {
				continue
			}
			totalAddresses += len(instance.host.Peerstore().Addrs(p))
		}
	}
	return totalAddresses
}
