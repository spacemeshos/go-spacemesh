package peerexchange

import (
	"context"
	"sync"
	"testing"
	"time"

	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func TestDiscovery_CheckBook(t *testing.T) {
	t.Run("network broken with big number of nodes", func(t *testing.T) {
		mesh, instances := setup(t, 20)

		totalAddressesBefore := calcTotalAddresses(instances)
		totalPeerAddressesBefore := calcTotalAddressesPeerStore(instances)

		brokeMesh(t, mesh)
		checkBooks(instances)

		totalAddressesAfter := calcTotalAddresses(instances)
		totalPeerAddressesAfter := calcTotalAddressesPeerStore(instances)

		require.NotEqual(t, totalAddressesBefore, totalAddressesAfter, "total addresses should be different")
		require.NotEqual(t, totalPeerAddressesBefore, totalPeerAddressesAfter, "total addresses should be different")
		require.True(t, totalAddressesBefore > totalAddressesAfter, "total addresses after break should be smaller")
		require.True(t, totalPeerAddressesBefore > totalPeerAddressesAfter, "total addresses after break should be smaller")
	})
	t.Run("with small number of nodes", func(t *testing.T) {
		mesh, instances := setup(t, 5)

		totalAddressesBefore := calcTotalAddresses(instances)
		totalPeerAddressesBefore := calcTotalAddressesPeerStore(instances)
		require.True(t, totalAddressesBefore > 0, "total addresses before break should be greater than 0")
		require.True(t, totalPeerAddressesBefore > 0, "total addresses before break should be greater than 0")

		brokeMesh(t, mesh)
		checkBooks(instances)

		totalAddressesAfter := calcTotalAddresses(instances)
		totalPeerAddressesAfter := calcTotalAddressesPeerStore(instances)
		require.Equal(t, 0, totalAddressesAfter, "total addresses after break should be zero")
		require.Equal(t, 0, totalPeerAddressesAfter, "total addresses after break should be zero")
	})
}

func setup(t *testing.T, nodesCount int) (mocknet.Mocknet, []*Discovery) {
	mesh, err := mocknet.FullMeshConnected(context.TODO(), nodesCount)
	require.NoError(t, err)
	var (
		instances = []*Discovery{}
		bootnode  *addrInfo
		rounds    = 5
	)

	for _, h := range mesh.Hosts() {
		logger := logtest.New(t).Named(h.ID().Pretty())
		cfg := Config{}

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
	return mesh, instances
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
			inst.CheckPeers()
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
