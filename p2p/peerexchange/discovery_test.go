package peerexchange

import (
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
	"github.com/spacemeshos/go-spacemesh/p2p/peerexchange/mocks"
)

func TestDiscovery_CrawlMesh(t *testing.T) {
	var (
		bootnode ma.Multiaddr
		n        = 20
	)
	mesh, err := mocknet.FullMeshLinked(n)
	require.NoError(t, err)

	for _, h := range mesh.Hosts() {
		logger := logtest.New(t).Named(h.ID().Pretty())
		cfg := Config{FastCrawl: time.Second, SlowCrawl: 10 * time.Second, MinPeers: 2}

		if bootnode == nil {
			require.NotEmpty(t, h.Addrs())
			p2p, err := ma.NewComponent("p2p", h.ID().String())
			require.NoError(t, err)
			bootnode = h.Addrs()[0].Encapsulate(p2p)
		}
		cfg.Bootnodes = append(cfg.Bootnodes, bootnode.String())
		instance, err := New(logger, h, cfg)
		require.NoError(t, err)
		t.Cleanup(instance.Stop)
	}

	require.NotEmpty(t, mesh.Hosts())
	require.Eventually(t, func() bool {
		for _, h := range mesh.Hosts() {
			if len(h.Network().Conns()) <= 1 {
				return false
			}
		}
		return true
	}, 3*time.Second, 200*time.Millisecond)
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

	var mu sync.Mutex
	returned := []ma.Multiaddr{
		multiaddrFromString(t, "/ip4/0.0.0.0/tcp/7777"),
	}
	addrmock.EXPECT().Addrs().DoAndReturn(func() []ma.Multiaddr {
		mu.Lock()
		defer mu.Unlock()
		return returned
	}).AnyTimes()
	discovery, err := New(logtest.New(t), ph, Config{})
	require.NoError(t, err)
	t.Cleanup(discovery.Stop)

	port := uint16(1010)
	advertised := ma.StringCast(fmt.Sprintf("/tcp/%d", port))
	mu.Lock()
	returned = append(returned, multiaddrFromString(
		t, fmt.Sprintf("/ip4/110.0.0.0/tcp/%d", port),
	))
	mu.Unlock()

	emitter.Emit(event.EvtLocalAddressesUpdated{})
	require.Eventually(t, func() bool {
		return discovery.AdvertisedAddress().Equal(advertised)
	}, 100*time.Millisecond, 10*time.Millisecond)
}
