package p2p

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/require"
)

func TestIsBootnode(t *testing.T) {
	cm, err := connmgr.NewConnManager(40, 100, connmgr.WithGracePeriod(30*time.Second))
	require.NoError(t, err)
	h, err := libp2p.New(libp2p.ConnectionManager(cm))
	require.NoError(t, err)
	bootnodes := []string{
		"/dns4/sample.spacemesh.io/tcp/5004/p2p/12D3KooWDS4mbE2Cqysjf6GBMtWnhcaoBYC6M3FNkTeZqCNFCNkf",
		"/dns4/sample.spacemesh.io/tcp/5005/p2p/12D3KooWRN5Jv6U2CbNZRFCHbGrfQ2m8tZkN8nxpBDNPu4cHRvJw",
		"/dns4/sample.spacemesh.io/tcp/5006/p2p/12D3KooWJEBZqrws8VSKSChtNMH4aYpiRxZ3D2mRaJWCUFPJCf3v",
	}

	tcs := []struct {
		desc      string
		bootnodes []string
		isBoot    bool
	}{
		{
			desc:      "bootnode",
			bootnodes: append(bootnodes, fmt.Sprintf("/dns4/sample.spacemesh.io/tcp/5007/p2p/%s", h.ID())),
			isBoot:    true,
		},
		{
			desc:      "not_bootnode",
			bootnodes: bootnodes,
			isBoot:    false,
		},
	}
	for _, tc := range tcs {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			got, err := isBootnode(h, tc.bootnodes)
			require.NoError(t, err)
			require.Equal(t, tc.isBoot, got)
		})
	}
}

func TestConnectionsNotifier(t *testing.T) {
	const n = 3
	mesh, err := mocknet.FullMeshLinked(n)
	require.NoError(t, err)
	counter := [n]atomic.Uint32{}
	// we count events - not peers
	for i, host := range mesh.Hosts() {
		i := i
		_, err := Upgrade(host, WithNodeReporter(func() { counter[i].Add(1) }))
		require.NoError(t, err)
	}

	mesh.ConnectPeers(mesh.Hosts()[0].ID(), mesh.Hosts()[1].ID())
	mesh.ConnectPeers(mesh.Hosts()[2].ID(), mesh.Hosts()[0].ID())
	require.Eventually(t, func() bool {
		return counter[0].Load() >= 2 && counter[1].Load() >= 1 && counter[2].Load() >= 1
	}, time.Second, 10*time.Millisecond)

	mesh.DisconnectPeers(mesh.Hosts()[0].ID(), mesh.Hosts()[1].ID())
	require.Eventually(t, func() bool {
		return counter[0].Load() >= 3 && counter[1].Load() >= 2
	}, time.Second, 10*time.Millisecond)
}
