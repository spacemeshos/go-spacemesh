package p2p

import (
	"context"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
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
			h1, err := New(context.Background(), logtest.New(t), cfg1, nc1, nc1)
			require.NoError(t, err)
			t.Cleanup(func() { h1.Stop() })

			nc2 := []byte("blue")
			h2, err := New(context.Background(), logtest.New(t), cfg2, nc2, nc2)
			require.NoError(t, err)
			t.Cleanup(func() { h2.Stop() })

			nc3 := []byte("red")
			h3, err := New(context.Background(), logtest.New(t), cfg3, nc3, nc3)
			require.NoError(t, err)
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
			desc:    "local IPv6 disallowed",
			address: "/ip6/::/tcp/0",
			errStr:  "gater disallows connection to peer",
		},
		{
			desc:           "local IPv4 allowed",
			address:        "/ip4/127.0.0.1/tcp/0",
			clearBlocklist: true,
		},
		{
			desc:           "local IPv4 allowed",
			address:        "/ip6/::/tcp/0",
			clearBlocklist: true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.desc, func(t *testing.T) {
			cfg1 := DefaultConfig()
			cfg1.DataDir = t.TempDir()
			cfg1.Listen = MustParseAddresses(tc.address)
			if tc.clearBlocklist {
				cfg1.IP4Blocklist = nil
				cfg1.IP6Blocklist = nil
			}
			h1, err := New(context.Background(), logtest.New(t), cfg1, nil, nil)
			require.NoError(t, err)
			t.Cleanup(func() { h1.Stop() })

			cfg2 := DefaultConfig()
			cfg2.DataDir = t.TempDir()
			cfg2.Listen = MustParseAddresses(tc.address)
			if tc.clearBlocklist {
				cfg2.IP4Blocklist = nil
				cfg2.IP6Blocklist = nil
			}
			h2, err := New(context.Background(), logtest.New(t), cfg2, nil, nil)
			require.NoError(t, err)
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
