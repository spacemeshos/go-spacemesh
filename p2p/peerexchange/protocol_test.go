package peerexchange

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
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

func buildPeer(tb testing.TB, l log.Log, h host.Host) *peerExchange {
	tb.Helper()
	port, err := routablePort(h)
	require.NoError(tb, err)
	addr, err := ma.NewComponent("tcp", strconv.Itoa(int(port)))
	require.NoError(tb, err)
	return buildPeerWithAddress(tb, l, h, addr)
}

func buildPeerWithAddress(tb testing.TB, l log.Log, h host.Host, addr ma.Multiaddr) *peerExchange {
	tb.Helper()
	return newPeerExchange(h, book.New(), addr, l)
}

func contains[T any](array []T, object T) bool {
	for _, elem := range array {
		if assert.ObjectsAreEqual(object, elem) {
			return true
		}
	}
	return false
}

func TestDiscovery_AdvertiseDNS(t *testing.T) {
	logger := logtest.New(t)
	mesh, err := mocknet.FullMeshConnected(2)
	require.NoError(t, err)

	addr := ma.StringCast("/dns4/bootnode.spacemesh.io/tcp/5003")

	sender := buildPeerWithAddress(t, logger, mesh.Hosts()[0], addr)
	receiver := buildPeer(t, logger, mesh.Hosts()[1])

	_, err = sender.Request(context.Background(), receiver.h.ID())
	require.NoError(t, err)

	id, err := ma.NewComponent("p2p", sender.h.ID().String())
	require.NoError(t, err)
	added := receiver.book.DrainQueue(1)
	require.Len(t, added, 1)
	require.True(t, addr.Encapsulate(id).Equal(added[0]))
}

// Test if peer exchange protocol handler properly
// filters returned addresses.
func TestDiscovery_FilteringAddresses(t *testing.T) {
	logger := logtest.New(t)
	mesh, err := mocknet.FullMeshConnected(2)
	require.NoError(t, err)

	peerA := buildPeer(t, logger, mesh.Hosts()[0])
	peerB := buildPeer(t, logger, mesh.Hosts()[1])

	maddr, err := ma.NewMultiaddr(dnsNode)
	require.NoError(t, err)
	_, id := peer.SplitAddr(maddr)
	require.NotEmpty(t, id)
	peerB.book.Add(book.SELF, id.String(), maddr)

	// Check if never attempted address is eventually returned
	// The returned addresses are randomly picked so try in a
	// tight loop.
	require.Eventually(t, func() bool {
		addresses, err := peerA.Request(context.TODO(), peerB.h.ID())
		require.NoError(t, err)
		return contains(addresses, dnsNode)
	}, time.Second, time.Nanosecond)

	peerB.book.Update(id.String(), book.Fail)

	// Check if stale address is "never" returned
	for i := 1; i <= 10; i++ {
		addresses, err := peerA.Request(context.TODO(), peerB.h.ID())
		require.NoError(t, err)
		assert.NotContains(t, addresses, dnsNode)
	}
}
