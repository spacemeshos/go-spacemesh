package p2p

import (
	"testing"

	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
)

func TestGater(t *testing.T) {
	cfg := DefaultConfig()
	h, err := mocknet.New().GenPeer()
	require.NoError(t, err)
	gater, err := newGater(cfg)
	require.NoError(t, err)
	gater.updateHost(h)
	for _, tc := range []struct {
		address string
		allowed bool
	}{
		{address: "/ip4/127.0.0.1/tcp/8000"},
		{address: "/ip4/192.168.0.3/tcp/8000"},
		{address: "/ip6/fe80::42:44ff:fe2a:87c5/tcp/8000"},
		{address: "/ip6/fe80::42:44ff:fe2a:87c5/tcp/8000"},
		{address: "/ip4/95.217.200.84/tcp/8000", allowed: true},
	} {
		t.Run(tc.address, func(t *testing.T) {
			addr, err := ma.NewMultiaddr(tc.address)
			require.NoError(t, err)
			require.Equal(t, tc.allowed, gater.InterceptAddrDial("", addr))
		})
	}
}
