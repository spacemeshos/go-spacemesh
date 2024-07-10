package p2p

import (
	"context"
	"net"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestPrologue(t *testing.T) {
	type testcase struct {
		desc       string
		listen     AddressList
		enableQUIC bool
		errStr     string
	}
	testcases := []testcase{
		{
			desc:   "tcp",
			listen: MustParseAddresses("/ip4/127.0.0.1/tcp/0"),
			errStr: "failed to negotiate security protocol",
		},
		{
			desc:       "quic",
			listen:     MustParseAddresses("/ip4/0.0.0.0/udp/0/quic-v1"),
			enableQUIC: true,
			errStr:     "cookie mismatch",
		},
	}
	for _, tc := range testcases {
		t.Run(tc.desc, func(t *testing.T) {
			logger := zaptest.NewLogger(t)
			cfg1 := DefaultConfig()
			cfg1.DataDir = t.TempDir()
			cfg1.Listen = tc.listen
			cfg1.EnableQUICTransport = tc.enableQUIC
			cfg1.IP4Blocklist = nil
			cfg2 := DefaultConfig()
			cfg2.DataDir = t.TempDir()
			cfg2.Listen = tc.listen
			cfg2.EnableQUICTransport = tc.enableQUIC
			cfg2.IP4Blocklist = nil
			cfg3 := DefaultConfig()
			cfg3.DataDir = t.TempDir()
			cfg3.Listen = tc.listen
			cfg3.EnableQUICTransport = tc.enableQUIC
			cfg3.IP4Blocklist = nil

			nc1 := []byte("red")
			h1, err := New(logger.Named("host-1"), cfg1, nc1, nc1)
			require.NoError(t, err)
			t.Cleanup(func() { h1.Stop() })

			nc2 := []byte("blue")
			h2, err := New(logger.Named("host-2"), cfg2, nc2, nc2)
			require.NoError(t, err)
			require.NoError(t, h2.Start())
			t.Cleanup(func() { h2.Stop() })

			nc3 := []byte("red")
			h3, err := New(logger.Named("host-3"), cfg3, nc3, nc3)
			require.NoError(t, err)
			require.NoError(t, h3.Start())
			t.Cleanup(func() { h3.Stop() })

			err = h1.Connect(context.Background(), peer.AddrInfo{
				ID:    h2.ID(),
				Addrs: h2.Addrs(),
			})
			require.ErrorContains(t, err, tc.errStr)

			err = h1.Connect(context.Background(), peer.AddrInfo{
				ID:    h3.ID(),
				Addrs: h3.Addrs(),
			})
			require.NoError(t, err)
		})
	}
}

func TestBlocklist(t *testing.T) {
	type testcase struct {
		desc           string
		address        string
		requireIPv6    bool
		clearBlocklist bool
		errStr         string
	}

	testcases := []testcase{
		{
			desc:    "local IPv4 disallowed",
			address: "/ip4/127.0.0.1/tcp/0",
			errStr:  "gater disallows connection to peer",
		},
		{
			desc:        "local IPv6 disallowed",
			address:     "/ip6/::/tcp/0",
			requireIPv6: true,
			errStr:      "gater disallows connection to peer",
		},
		{
			desc:           "local IPv4 allowed",
			address:        "/ip4/127.0.0.1/tcp/0",
			clearBlocklist: true,
		},
		{
			desc:           "local IPv6 allowed",
			address:        "/ip6/::/tcp/0",
			requireIPv6:    true,
			clearBlocklist: true,
		},
	}

	ipv6Available := false
	ifs, err := net.InterfaceAddrs()
	require.NoError(t, err)

	// check if at least one interface has an IPv6 address
	for _, ifa := range ifs {
		if ipnet, ok := ifa.(*net.IPNet); ok && ipnet.IP.To4() == nil {
			ipv6Available = true
			break
		}
	}

	for _, tc := range testcases {
		t.Run(tc.desc, func(t *testing.T) {
			logger := zaptest.NewLogger(t)
			if tc.requireIPv6 && !ipv6Available {
				t.Skip("IPv6 not available")
			}

			cfg1 := DefaultConfig()
			cfg1.DataDir = t.TempDir()
			cfg1.Listen = MustParseAddresses(tc.address)
			if tc.clearBlocklist {
				cfg1.IP4Blocklist = nil
				cfg1.IP6Blocklist = nil
			}
			h1, err := New(logger.Named("host-1"), cfg1, nil, nil)
			require.NoError(t, err)
			t.Cleanup(func() { h1.Stop() })

			cfg2 := DefaultConfig()
			cfg2.DataDir = t.TempDir()
			cfg2.Listen = MustParseAddresses(tc.address)
			if tc.clearBlocklist {
				cfg2.IP4Blocklist = nil
				cfg2.IP6Blocklist = nil
			}
			h2, err := New(logger.Named("host-2"), cfg2, nil, nil)
			require.NoError(t, err)
			require.NoError(t, h2.Start())
			t.Cleanup(func() { h2.Stop() })

			err = h1.Connect(context.Background(), peer.AddrInfo{
				ID:    h2.ID(),
				Addrs: h2.Addrs(),
			})
			if tc.errStr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.errStr)
			}
		})
	}
}
