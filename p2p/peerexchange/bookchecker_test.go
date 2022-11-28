package peerexchange

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/addressbook"
)

func TestDiscovery_CheckBook(t *testing.T) {
	t.Parallel()
	t.Run("network broken with big number of nodes", func(t *testing.T) {
		t.Parallel()
		mesh, instances := setupOverMockNet(t, 20)

		totalAddressesBefore := calcTotalAddresses(instances)
		totalPeerAddressesBefore := calcTotalAddressesPeerStore(instances)

		brokeMesh(t, mesh, instances)
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

		brokeMesh(t, mesh, instances)
		checkBooks(instances)

		totalAddressesAfter := calcTotalAddresses(instances)
		totalPeerAddressesAfter := calcTotalAddressesPeerStore(instances)
		require.Equal(t, 0, totalAddressesAfter, "total addresses after break should be zero")
		require.Equal(t, 0, totalPeerAddressesAfter, "total addresses after break should be zero")
	})
}

func TestDiscovery_GetRandomPeers(t *testing.T) {
	hosts, instances := setupOverNodes(t, 2)
	d := instances[0]

	bootNodeAddress, err := addressbook.ParseAddrInfo("/dns4/sample.spacemesh.io/tcp/5003/p2p/12D3KooWGQrF3pHrR1W7P6nh8gypYxtFS93SnmvtN6qpyeSo7T2u")
	require.NoError(t, err)
	sampleNode, err := addressbook.ParseAddrInfo("/dns4/sample.spacemesh.io/tcp/5003/p2p/12D3KooWBdbwmiMhLDzAbfY3Vy5RDGaumUEgHe2P1pL5G3dhhWMb")
	require.NoError(t, err)
	oldNode, err := addressbook.ParseAddrInfo("/dns4/sample.spacemesh.io/tcp/5003/p2p/12D3KooWJSLApvoWiX9q3oKkYQTXpxQC7qAKt6mTERCJuwCFT98d")
	require.NoError(t, err)
	hostConnected := hosts[1].Network().ListenAddresses()[0].String() + "/p2p/" + hosts[1].ID().String()
	addr, err := ma.NewMultiaddr(hostConnected)
	require.NoError(t, err)
	connectedNodeAddress := &addressbook.AddrInfo{ID: hosts[1].ID(), RawAddr: hostConnected}
	connectedNodeAddress.SetAddr(addr)

	t.Run("empty peers", func(t *testing.T) {
		require.Eventually(t, func() bool {
			return d.book.NumAddresses() == 0
		}, 2*time.Second, 100*time.Millisecond)
		require.Equal(t, 0, len(d.GetRandomPeers(10)), "should return zero peers")
	})

	best, err := bestHostAddress(d.host)
	require.NoError(t, err)
	// populate address book with some addresses.
	d.book.AddAddress(bootNodeAddress, best)
	d.book.AddAddress(sampleNode, best)
	d.book.AddAddress(oldNode, best)
	d.book.AddAddress(connectedNodeAddress, best)

	t.Run("check all nodes in book", func(t *testing.T) {
		require.Eventually(t, func() bool {
			return d.book.NumAddresses() == 4
		}, 4*time.Second, 100*time.Millisecond, "should return 4 peers")
	})

	wrapDiscovery(instances)

	t.Run("return all except connected node", func(t *testing.T) {
		require.Eventually(t, func() bool {
			return d.book.NumAddresses() == 4
		}, 4*time.Second, 100*time.Millisecond)

		require.Eventually(t, func() bool {
			return len(wrapperGetRandomPeers(d, 3)) == 3
		}, 4*time.Second, 100*time.Millisecond, "should return 3 peers")
		require.NotContains(t, d.GetRandomPeers(10), connectedNodeAddress, "should not return connected node")
	})

	t.Run("return all except connected node and bootnode", func(t *testing.T) {
		d.host.ConnManager().Protect(bootNodeAddress.ID, BootNodeTag)
		defer d.host.ConnManager().Unprotect(bootNodeAddress.ID, BootNodeTag)
		require.Eventually(t, func() bool {
			return d.book.NumAddresses() == 4
		}, 4*time.Second, 100*time.Millisecond)

		require.Eventually(t, func() bool {
			return len(wrapperGetRandomPeers(d, 2)) == 2
		}, 4*time.Second, 100*time.Millisecond, "should return 2 peers")
		res := d.GetRandomPeers(10)
		require.NotContains(t, res, connectedNodeAddress, "should not return connected node")
		require.NotContains(t, res, bootNodeAddress, "should not return bootnode")
	})

	t.Run("check last usage address", func(t *testing.T) {
		// trigger update last usage date
		d.book.AddAddress(oldNode, best)
		d.book.Good(oldNode.ID)
		require.Eventually(t, func() bool {
			return d.book.NumAddresses() == 4
		}, 4*time.Second, 100*time.Millisecond)

		require.Eventually(t, func() bool {
			return len(wrapperGetRandomPeers(d, 2)) == 2
		}, 4*time.Second, 100*time.Millisecond, "should return 2 peers")

		res := d.GetRandomPeers(10)
		require.NotContains(t, res, oldNode, "should not return connected node")

		require.Eventually(t, func() bool {
			res = wrapperGetRandomPeers(d, 3)
			if len(res) != 3 {
				return false
			}
			for _, peer := range res {
				if peer.ID.String() == oldNode.ID.String() {
					return true
				}
			}
			return false
		}, 4*time.Second, 100*time.Millisecond, "should return 3 peers and contain old node")
	})
}

func wrapperGetRandomPeers(d *Discovery, expectedNum int) (res []*addressbook.AddrInfo) {
	for i := 0; i < 10; i++ {
		res = d.GetRandomPeers(expectedNum)
		if len(res) == expectedNum {
			return res
		}
	}
	return
}

func TestHost_ReceiveAddressesOnCheck(t *testing.T) {
	extraNodesCount := 100

	_, instances := setupOverNodes(t, 2)
	nodeA, nodeB := instances[0], instances[1]
	wrapDiscovery(instances)

	// check that after bootstrap, nodeB has only address of nodeA and nodeA has only address of nodeB
	require.Eventually(t, func() bool {
		return nodeA.book.NumAddresses() == 1 && nodeB.book.NumAddresses() == 1
	}, 4*time.Second, 100*time.Millisecond, "nodeA and nodeB should have only 1 address")
	require.Equal(t, nodeA.book.GetAddresses()[0].ID, nodeB.host.ID(), "nodeA should have only address of nodeB")
	require.Equal(t, nodeB.book.GetAddresses()[0].ID, nodeA.host.ID(), "nodeB should have only address of nodeA")

	// disconnect nodeA and node
	require.NoError(t, nodeB.host.Network().ClosePeer(nodeA.host.ID()))
	// wait for addresses become outdated and ready for check
	require.Eventually(t, func() bool {
		return len(nodeB.GetRandomPeers(nodeB.cfg.CheckPeersNumber)) > 0
	}, 3*time.Second, 100*time.Millisecond, "nodeB should return addresses to check")

	// add new nodes to network
	rng := rand.New(rand.NewSource(1001))
	var addrs []*addressbook.AddrInfo
	var ids []peer.ID
	for i := 0; i < extraNodesCount; i++ {
		tmpAddr := genRandomInfo(t, rng)
		addrs = append(addrs, tmpAddr)
		ids = append(ids, tmpAddr.ID)
	}

	best, err := bestHostAddress(nodeA.host)
	require.NoError(t, err)
	nodeA.book.AddAddresses(addrs, best)

	require.Equal(t, extraNodesCount+1, nodeA.book.NumAddresses(), "nodeA should have all extra addresses")
	require.Equal(t, 1, nodeB.book.NumAddresses(), "nodeB should still have 1 address")

	nodeB.CheckPeers(context.Background())

	// we can't know exact nodes count, cause on addrExchange handler we randomly return addresses from buckets
	addresses := nodeB.book.NumAddresses()
	require.True(t, addresses > 1 && addresses <= extraNodesCount+1, "nodeB should have some of additional addresses")

	ids = append(ids, nodeA.host.ID())
	for _, addr := range nodeB.book.GetAddresses() {
		require.Subset(t, ids, []peer.ID{addr.ID}, "address should be one of known")
	}
}

func setupOverNodes(t *testing.T, nodesCount int) ([]host.Host, []*Discovery) {
	hosts := make([]host.Host, 0, nodesCount)
	for i := 0; i < nodesCount; i++ {
		cm, err := connmgr.NewConnManager(40, 100, connmgr.WithGracePeriod(30*time.Second))
		require.NoError(t, err)
		h, err := libp2p.New(libp2p.ConnectionManager(cm))
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
	mesh, err := mocknet.FullMeshConnected(nodesCount)
	require.NoError(t, err)
	instances := setup(t, mesh.Hosts())
	wrapDiscovery(instances)
	return mesh, instances
}

func setup(t *testing.T, hosts []host.Host) []*Discovery {
	var (
		instances []*Discovery
		bootnode  *addressbook.AddrInfo
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
			}
		}(instance)
	}
	wg.Wait()
}

// brokeMesh simulate that all peers are disconnected and the mesh is broken.
func brokeMesh(t *testing.T, mesh mocknet.Mocknet, instances []*Discovery) {
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
	// wait until address ussage will expire and will check again
	require.Eventually(t, func() bool {
		for _, instance := range instances {
			if len(instance.GetRandomPeers(10)) == 0 {
				return false
			}
		}
		return true
	}, 4*time.Second, 100*time.Millisecond)
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
		totalAddresses += len(instance.book.GetAddresses())
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

// genRandomInfo generates addrInfo with valid multiaddr.
func genRandomInfo(tb testing.TB, rng *rand.Rand) *addressbook.AddrInfo {
	tb.Helper()
	pk, _, err := crypto.GenerateEd25519Key(rng)
	require.NoError(tb, err)
	bytes, err := pk.Raw()
	require.NoError(tb, err)

	ip := net.IPv4(169, 255, bytes[0], bytes[1])
	port := binary.BigEndian.Uint64(bytes) % 65535
	id, err := peer.IDFromPrivateKey(pk)
	require.NoError(tb, err)

	raw := fmt.Sprintf("/ip4/%s/tcp/%d/p2p/%s", ip, port, id)
	addr, err := ma.NewMultiaddr(raw)
	require.NoError(tb, err)
	result := &addressbook.AddrInfo{IP: ip, ID: id, RawAddr: raw}
	result.SetAddr(addr)
	return result
}
