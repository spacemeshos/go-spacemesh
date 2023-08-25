package p2p

import (
	"testing"

	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/multiformats/go-multiaddr"
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
			addr, err := multiaddr.NewMultiaddr(tc.address)
			require.NoError(t, err)
			require.Equal(t, tc.allowed, gater.InterceptAddrDial("", addr))
		})
	}
}

type remote struct {
	multiaddr.Multiaddr
}

func (remote) LocalMultiaddr() multiaddr.Multiaddr {
	return nil
}

func (r remote) RemoteMultiaddr() multiaddr.Multiaddr {
	return r.Multiaddr
}

func TestGaterIPLimit(t *testing.T) {
	type (
		conn    string
		disconn string
	)

	cfg := DefaultConfig()
	cfg.IPLimit = 2
	h, err := mocknet.New().GenPeer()
	require.NoError(t, err)

	for _, tc := range []struct {
		events  []any
		address string
		allowed bool
	}{
		{
			events: []any{
				conn("/ip4/95.217.200.84/tcp/8001"),
				conn("/ip4/95.217.200.84/tcp/8002"),
			},
			address: "/ip4/95.217.200.84/tcp/8000",
		},
		{
			events: []any{
				conn("/ip4/95.217.200.84/tcp/8001"),
				disconn("/ip4/95.217.200.84/tcp/8001"),
				conn("/ip4/95.217.200.84/tcp/8002"),
			},
			address: "/ip4/95.217.200.84/tcp/8000",
			allowed: true,
		},
		{
			events: []any{
				conn("/ip4/95.217.201.84/tcp/8001"),
				conn("/ip4/95.217.201.84/tcp/8002"),
			},
			address: "/ip4/95.217.200.84/tcp/8000",
			allowed: true,
		},
		{
			events: []any{
				conn("/ip6/fe80::42:44ff:fe2a:87c5/tcp/8001"),
				conn("/ip6/fe80::42:44ff:fe2a:87c5/tcp/8002"),
			},
			address: "/ip6/fe80::42:44ff:fe2a:87c5/tcp/8000",
		},
		{
			events: []any{
				conn("/dns4/anyname/tcp/8001"),
			},
			address: "/dns4/anyname/tcp/8001",
			allowed: true,
		},
		{
			events: []any{
				disconn("/ip6/fe80::42:44ff:fe2a:87c5/tcp/8000"),
				disconn("/ip6/fe80::42:44ff:fe2a:87c5/tcp/8000"),
				conn("/ip6/fe80::42:44ff:fe2a:87c5/tcp/8000"),
				conn("/ip6/fe80::42:44ff:fe2a:87c5/tcp/8000"),
			},
			address: "/ip6/fe80::42:44ff:fe2a:87c5/tcp/8000",
		},
	} {
		t.Run(tc.address, func(t *testing.T) {
			gater, err := newGater(cfg)
			require.NoError(t, err)
			gater.updateHost(h)

			for _, ev := range tc.events {
				switch v := ev.(type) {
				case conn:
					addr, err := multiaddr.NewMultiaddr(string(v))
					require.NoError(t, err)
					gater.OnConnected(addr)
				case disconn:
					addr, err := multiaddr.NewMultiaddr(string(v))
					require.NoError(t, err)
					gater.OnDisconnected(addr)
				}
			}

			addr, err := multiaddr.NewMultiaddr(tc.address)
			require.NoError(t, err)
			require.Equal(t, tc.allowed, gater.InterceptAccept(remote{addr}))
			require.Equal(t, tc.allowed, gater.InterceptAddrDial("", addr))
		})
	}
}
