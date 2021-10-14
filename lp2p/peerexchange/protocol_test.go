package peerexchange

import (
	"context"
	"testing"

	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func TestDiscovery_LearnAddress(t *testing.T) {
	n := 4
	logger := logtest.New(t)
	mesh, err := mocknet.FullMeshConnected(context.TODO(), n)
	require.NoError(t, err)
	protocols := []*peerExchange{}

	for _, h := range mesh.Hosts() {
		require.NoError(t, err)
		book := newAddrBook(Config{}, logger)
		port, err := portFromAddress(h)
		require.NoError(t, err)
		protocols = append(protocols, newPeerExchange(h, book, port, logger))
	}
	for _, proto := range protocols {
		for _, proto2 := range protocols {
			if proto.h.ID() != proto2.h.ID() {
				_, err := proto.Request(context.TODO(), proto2.h.ID())
				require.NoError(t, err)
				best, err := bestHostAddress(proto.h)
				require.NoError(t, err)
				found := proto2.book.Lookup(proto.h.ID())
				require.Equal(t, best, found)
			}
		}
	}
}
