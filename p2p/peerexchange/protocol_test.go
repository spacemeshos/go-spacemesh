package peerexchange

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/addressbook"
	"github.com/spacemeshos/go-spacemesh/p2p/book"
)

func routablePort(h host.Host) (uint16, error) {
	addr, err := routableNetAddress(h)
	if err != nil {
		return 0, err
	}
	return portFromAddress(addr)
}

const dnsNode = "/dns4/bootnode.spacemesh.io/tcp/5003/p2p/12D3KooWGQrF3pHrR1W7P6nh8gypYxtFS93SnmvtN6qpyeSo7T2u"

func buildPeer(tb testing.TB, l log.Log, h host.Host, config PeerExchangeConfig) *peerExchange {
	tb.Helper()
	port, err := routablePort(h)
	require.NoError(tb, err)
	addr, err := ma.NewComponent("tcp", strconv.Itoa(int(port)))
	require.NoError(tb, err)
	return buildPeerWithAddress(tb, l, h, addr, config)
}

func buildPeerWithAddress(tb testing.TB, l log.Log, h host.Host, addr ma.Multiaddr, config PeerExchangeConfig) *peerExchange {
	tb.Helper()
	return newPeerExchange(h, book.New(), addr, l, config)
}

func contains[T any](array []T, object T) bool {
	for _, elem := range array {
		if assert.ObjectsAreEqual(object, elem) {
			return true
		}
	}
	return false
}

func TestDiscovery_LearnAddress(t *testing.T) {
	n := 4

	info, err := addressbook.ParseAddrInfo(dnsNode)
	require.NoError(t, err)

	logger := logtest.New(t)
	mesh, err := mocknet.FullMeshConnected(n)
	require.NoError(t, err)
	protocols := []*peerExchange{}

	for _, h := range mesh.Hosts() {
		peer := buildPeer(t, logger, h, DefaultPeerExchangeConfig())

		best, err := bestHostAddress(h)
		require.NoError(t, err)
		peer.book.AddAddress(info, best)

		protocols = append(protocols, peer)
	}
	for _, proto := range protocols {
		for _, proto2 := range protocols {
			if proto.h.ID() == proto2.h.ID() {
				continue
			}
			_, err := proto.Request(context.TODO(), proto2.h.ID())
			require.NoError(t, err)
			best, err := bestHostAddress(proto.h)
			require.NoError(t, err)
			found := proto2.book.Lookup(proto.h.ID())
			require.Equal(t, best, found)

			require.True(t, checkDNSAddress(proto.book.GetAddresses(), dnsNode))
			require.True(t, checkDNSAddress(proto2.book.GetAddresses(), dnsNode))
		}
	}
}

func TestDiscovery_AdvertiseDNS(t *testing.T) {
	logger := logtest.New(t)
	mesh, err := mocknet.FullMeshConnected(2)
	require.NoError(t, err)

	addr := ma.StringCast("/dns4/bootnode.spacemesh.io/tcp/5003")

	sender := buildPeerWithAddress(t, logger, mesh.Hosts()[0], addr, DefaultPeerExchangeConfig())
	receiver := buildPeer(t, logger, mesh.Hosts()[1], DefaultPeerExchangeConfig())

	_, err = sender.Request(context.Background(), receiver.h.ID())
	require.NoError(t, err)

	id, err := ma.NewComponent("p2p", sender.h.ID().String())
	require.NoError(t, err)
	added := receiver.book.Lookup(sender.h.ID()).Addr().Decapsulate(id)
	require.True(t, addr.Equal(added))
}

// Test if peer exchange protocol handler properly
// filters returned addresses.
func TestDiscovery_FilteringAddresses(t *testing.T) {
	logger := logtest.New(t)
	mesh, err := mocknet.FullMeshConnected(2)
	require.NoError(t, err)

	config := PeerExchangeConfig{
		stalePeerTimeout: 10 * time.Millisecond,
	}

	peerA := buildPeer(t, logger, mesh.Hosts()[0], config)
	peerB := buildPeer(t, logger, mesh.Hosts()[1], config)
	best, err := bestHostAddress(peerB.h)
	require.NoError(t, err)

	info, err := addressbook.ParseAddrInfo(dnsNode)
	require.NoError(t, err)
	peerB.book.AddAddress(info, best)

	// Check if never attempted address is eventually returned
	// The returned addresses are randomly picked so try in a
	// tight loop.
	require.Eventually(t, func() bool {
		addresses, err := peerA.Request(context.TODO(), peerB.h.ID())
		require.NoError(t, err)
		return contains(addresses, info)
	}, time.Second, time.Nanosecond)

	peerB.book.Good(info.ID)
	// Wait for `info` to become stale
	time.Sleep(10 * time.Millisecond)

	// Check if stale address is "never" returned
	for i := 1; i <= 10; i++ {
		addresses, err := peerA.Request(context.TODO(), peerB.h.ID())
		require.NoError(t, err)
		assert.NotContains(t, addresses, info)
	}
}

func checkDNSAddress(addresses []*addressbook.AddrInfo, dns string) bool {
	for _, addr := range addresses {
		if addr.RawAddr == dns {
			return true
		}
	}
	return false
}
