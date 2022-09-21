package peerexchange

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/addressbook"
	"github.com/spacemeshos/go-spacemesh/p2p/peerexchange/mocks"
)

func TestDiscovery_CrawlMesh(t *testing.T) {
	var (
		instances = []*Discovery{}
		bootnode  *addressbook.AddrInfo
		n         = 20
		rounds    = 5
	)
	mesh, err := mocknet.FullMeshLinked(n)
	require.NoError(t, err)

	for _, h := range mesh.Hosts() {
		logger := logtest.New(t).Named(h.ID().Pretty())
		cfg := DiscoveryConfig{}

		best, err := bestHostAddress(h)
		require.NoError(t, err)
		if bootnode == nil {
			bootnode = best
		}
		cfg.Bootnodes = append(cfg.Bootnodes, bootnode.RawAddr)
		instance, err := New(logger, h, cfg, t.TempDir())
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
	wait := make(chan struct{})
	go func() {
		wg.Wait()
		close(wait)
	}()
	select {
	case <-time.After(5 * time.Second):
		require.FailNow(t, "bootstrap failed to finish on time")
	case <-wait:
	}
	for _, h := range mesh.Hosts() {
		require.True(t, len(h.Network().Conns()) > 1,
			"expected connections with more then just bootnode")
	}
}

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./discovery_test.go

// AddrProvider provider for multiaddrs.
type AddrProvider interface {
	Addrs() []ma.Multiaddr
}

type patchedHost struct {
	host.Host
	provider AddrProvider
}

func (ph patchedHost) Addrs() []ma.Multiaddr {
	return ph.provider.Addrs()
}

func multiaddrFromString(tb testing.TB, raw string) ma.Multiaddr {
	tb.Helper()

	addr, err := ma.NewMultiaddr(raw)
	require.NoError(tb, err)
	return addr
}

func TestDiscovery_PrefereRoutablePort(t *testing.T) {
	mesh, err := mocknet.WithNPeers(1)
	require.NoError(t, err)
	h := mesh.Hosts()[0]

	ctrl := gomock.NewController(t)
	addrmock := mocks.NewMockAddrProvider(ctrl)
	ph := patchedHost{Host: h, provider: addrmock}

	emitter, err := h.EventBus().Emitter(&event.EvtLocalAddressesUpdated{})
	require.NoError(t, err)

	returned := []ma.Multiaddr{
		multiaddrFromString(t, "/ip4/0.0.0.0/tcp/7777"),
	}
	addrmock.EXPECT().Addrs().DoAndReturn(func() []ma.Multiaddr {
		return returned
	}).AnyTimes()
	discovery, err := New(logtest.New(t), ph, DiscoveryConfig{}, t.TempDir())
	require.NoError(t, err)
	t.Cleanup(discovery.Stop)

	external := uint16(1010)
	returned = append(returned, multiaddrFromString(
		t, fmt.Sprintf("/ip4/110.0.0.0/tcp/%d", external),
	))

	emitter.Emit(event.EvtLocalAddressesUpdated{})
	require.Eventually(t, func() bool {
		return discovery.ExternalPort() == external
	}, 100*time.Millisecond, 10*time.Millisecond)
}
