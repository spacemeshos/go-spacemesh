package peerexchange

import (
	"context"
	"testing"

	"github.com/libp2p/go-libp2p/core/host"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/addressbook"
)

func routablePort(h host.Host) (uint16, error) {
	addr, err := routableNetAddress(h)
	if err != nil {
		return 0, err
	}
	return portFromAddress(addr)
}

func TestDiscovery_LearnAddress(t *testing.T) {
	n := 4

	const dnsNode = "/dns4/bootnode.spacemesh.io/tcp/5003/p2p/12D3KooWGQrF3pHrR1W7P6nh8gypYxtFS93SnmvtN6qpyeSo7T2u"
	info, err := addressbook.ParseAddrInfo(dnsNode)
	require.NoError(t, err)

	logger := logtest.New(t)
	mesh, err := mocknet.FullMeshConnected(n)
	require.NoError(t, err)
	protocols := []*peerExchange{}

	for _, h := range mesh.Hosts() {
		require.NoError(t, err)
		book := addressbook.NewAddrBook(addressbook.DefaultAddressBookConfigWithDataDir(""), logger)

		best, err := bestHostAddress(h)
		require.NoError(t, err)
		book.AddAddress(info, best)

		port, err := routablePort(h)
		require.NoError(t, err)
		protocols = append(protocols, newPeerExchange(h, book, port, logger))
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

func checkDNSAddress(addresses []*addressbook.AddrInfo, dns string) bool {
	for _, addr := range addresses {
		if addr.RawAddr == dns {
			return true
		}
	}
	return false
}
