package discovery

import (
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func TestSanity(t *testing.T) {
	mock, err := mocknet.FullMeshLinked(4)
	require.NoError(t, err)
	discs := make([]*Discovery, len(mock.Hosts()))
	t.Cleanup(func() {
		for _, disc := range discs {
			disc.Stop()
		}
	})
	boot := mock.Hosts()[0]
	logger := logtest.New(t).Zap()
	bootdisc, err := New(boot,
		WithPeriod(100*time.Microsecond),
		Private(),
		Server(),
		WithLogger(logger),
	)
	require.NoError(t, err)
	bootdisc.Start()
	discs[0] = bootdisc
	require.NoError(t, err)
	for i, h := range mock.Hosts()[1:] {
		disc, err := New(h,
			Private(),
			WithLogger(logger),
			WithBootnodes([]peer.AddrInfo{{ID: boot.ID(), Addrs: boot.Addrs()}}),
		)
		require.NoError(t, err)
		disc.Start()
		discs[1+i] = disc
	}
	require.Eventually(t, func() bool {
		for _, h := range mock.Hosts() {
			if len(h.Network().Peers()) != len(mock.Hosts())-1 {
				return false
			}
		}
		return true
	}, 3*time.Second, 50*time.Microsecond)
}
