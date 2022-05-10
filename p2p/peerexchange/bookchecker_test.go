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
	var (
		instances = []*Discovery{}
		bootnode  *addrInfo
		n         = 20
		rounds    = 5
	)
	mesh, err := mocknet.FullMeshConnected(context.TODO(), n)
	require.NoError(t, err)

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

	totalAddressesBefore := 0
	for _, instance := range instances {
		list := instance.book.getAddresses()
		totalAddressesBefore += len(list)
	}

	// disconnect all peers
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

	// run checkbook
	for _, instance := range instances {
		wg.Add(1)
		go func(inst *Discovery) {
			defer wg.Done()
			inst.checkPeers()
		}(instance)
	}
	wg.Wait()

	totalAddressesAfter := 0
	for _, instance := range instances {
		list := instance.book.getAddresses()
		totalAddressesAfter += len(list)
	}

	require.NotEqual(t, totalAddressesBefore, totalAddressesAfter, "total addresses should be different")
	require.True(t, totalAddressesBefore > totalAddressesAfter, "total addresses after disconect should be smaller")
}
