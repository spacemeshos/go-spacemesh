package p2p

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func TestPing(t *testing.T) {
	type testcase struct {
		desc       string
		listen     AddressList
		enableQUIC bool
	}

	testcases := []testcase{
		{
			desc:   "tcp",
			listen: MustParseAddresses("/ip4/127.0.0.1/tcp/0"),
		},
		{
			desc:       "quic",
			listen:     MustParseAddresses("/ip4/0.0.0.0/udp/0/quic-v1"),
			enableQUIC: true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.desc, func(t *testing.T) {
			cfg1 := DefaultConfig()
			cfg1.DataDir = t.TempDir()
			cfg1.Listen = tc.listen
			cfg1.EnableQUICTransport = tc.enableQUIC
			nc := []byte("foobar")
			h1, err := New(context.Background(), logtest.New(t), cfg1, nc, nc)
			require.NoError(t, err)
			h1.discovery.Start()
			t.Cleanup(func() { h1.Stop() })

			cfg2 := DefaultConfig()
			cfg2.DataDir = t.TempDir()
			cfg2.Listen = tc.listen
			cfg2.EnableQUICTransport = tc.enableQUIC
			cfg2.PingPeers = []string{h1.ID().String()}
			h2, err := New(context.Background(), logtest.New(t), cfg2, nc, nc)
			require.NoError(t, err)
			h2.discovery.Start()
			t.Cleanup(func() { h2.Stop() })

			err = h2.Connect(context.Background(), peer.AddrInfo{
				ID:    h1.ID(),
				Addrs: h1.Addrs(),
			})
			require.NoError(t, err)

			h2.Ping().Start()
			t.Cleanup(h2.Ping().Stop)

			require.Eventually(t, func() bool {
				numSuccess, numFail := h2.Ping().Stats(h1.ID())
				t.Logf("ping stats: success %d fail %d", numSuccess, numFail)
				return numSuccess > 1
			}, 10*time.Second, 100*time.Millisecond, "waiting for ping to succeed")
		})
	}
}
