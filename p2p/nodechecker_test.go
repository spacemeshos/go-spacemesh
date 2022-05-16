package p2p

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/peer"
	p2putil "github.com/libp2p/go-libp2p-netutil"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/p2p/peerexchange"
)

type hostWrapper struct {
	host      *Host
	transport *addressFactory
}

// addressFactory is a mock implementation of the addressFactory interface. In real app autorelaly manage it.
type addressFactory struct {
	blockAddresses []string
}

func (t *addressFactory) addressFactory(addrs []ma.Multiaddr) []ma.Multiaddr {
	result := make([]ma.Multiaddr, 0)
	var ts []string
	for _, addr := range addrs {
		blocked := false
		for _, blockAddr := range t.blockAddresses {
			if addr.String() == blockAddr {
				blocked = true
			}
		}
		if !blocked {
			result = append(result, addr)
			ts = append(ts, addr.String())
		}
	}
	return result
}

func TestHost_LocalAddressChange(t *testing.T) {
	const (
		oldAddr = iota
		newAddr
	)
	table := []struct {
		title    string
		block    []int
		expected []int
	}{
		{
			title:    "change local address of node and all not acceptable",
			expected: []int{},
			block:    []int{oldAddr, newAddr},
		},
		{
			title:    "change local address of node and old not acceptable",
			expected: []int{newAddr},
			block:    []int{oldAddr},
		},
		{
			title:    "change local address of node and new not acceptable",
			expected: []int{oldAddr},
			block:    []int{newAddr},
		},
		{
			title:    "change local address of node and all are acceptable",
			expected: []int{newAddr, oldAddr},
			block:    []int{},
		},
	}
	for _, testCase := range table {
		t.Run(testCase.title, func(t *testing.T) {
			t.Parallel()
			addrAOld := generateAddressV4(t)
			addrANew := generateAddressV4(t)
			addrBOld := generateAddressV4(t)

			nodeA := generateNode(t, addrAOld, "")
			nodeB := generateNode(t, addrBOld, nodeA.host.Addrs()[0].String()+"/p2p/"+nodeA.host.ID().String())

			discoveryBootstrap(nodeA.host.discovery)
			discoveryBootstrap(nodeB.host.discovery)

			require.Equal(t, 1, len(nodeB.host.Peerstore().Addrs(nodeA.host.ID())), "nodeB should have 1 address of nodeA")

			// assing new address to nodeA, expect nodeB to be notified.
			require.NoError(t, nodeA.host.Network().Listen(addrANew), "nodeA should be able to listen on new address")

			time.Sleep(100 * time.Millisecond)
			for _, addr := range nodeB.host.Peerstore().Addrs(nodeA.host.ID()) {
				require.Condition(t, func() bool {
					return addr.String() == addrANew.String() || addr.String() == addrAOld.String()
				}, "nodeB should have address of nodeA")
			}
			require.Equal(t, 2, len(nodeB.host.Peerstore().Addrs(nodeA.host.ID())), "nodeB should have 2 addresses of nodeA")

			for _, addr := range testCase.block {
				switch addr {
				case oldAddr:
					nodeB.transport.blockAddresses = append(nodeB.transport.blockAddresses, addrAOld.String())
					nodeA.transport.blockAddresses = append(nodeA.transport.blockAddresses, addrAOld.String())
				case newAddr:
					nodeA.transport.blockAddresses = append(nodeA.transport.blockAddresses, addrANew.String())
					nodeB.transport.blockAddresses = append(nodeB.transport.blockAddresses, addrANew.String())
				}
			}

			//nodeB.host.discovery.CheckPeers()

			resultAddresses := nodeB.host.discovery.GetAddresses()
			if len(testCase.expected) == 2 {
				require.True(t, len(resultAddresses) > 0, "nodeB should have at least 1 address")
			} else {
				require.Equal(t, len(testCase.expected), len(nodeB.host.Peerstore().Addrs(nodeA.host.ID())), "nodeB should have %d addresses", len(testCase.expected))
				for _, addr := range testCase.expected {
					switch addr {
					case oldAddr:
						addrL := nodeB.host.Peerstore().Addrs(nodeA.host.ID())
						require.Equal(t, addrAOld.String(), addrL[0].String(), "nodeB should have new address of nodeA")
						require.Equal(t, len(testCase.expected), len(nodeB.host.Peerstore().Addrs(nodeA.host.ID())), "nodeB should have %d addresses", len(testCase.expected))

					case newAddr:
						addrL := nodeB.host.Peerstore().Addrs(nodeA.host.ID())
						require.Equal(t, addrANew.String(), addrL[0].String(), "nodeB should have new address of nodeA")
						require.Equal(t, len(testCase.expected), len(nodeB.host.Peerstore().Addrs(nodeA.host.ID())), "nodeB should have %d addresses", len(testCase.expected))
					}
				}
			}
		})
	}
}

func generateAddressV4(t *testing.T) ma.Multiaddr {
	var blackholeIP6 = net.ParseIP("100::")
	sk, err := p2putil.RandTestBogusPrivateKey()
	require.NoError(t, err)

	id, err := peer.IDFromPrivateKey(sk)
	require.NoError(t, err)

	suffix := id
	if len(id) > 8 {
		suffix = id[len(id)-8:]
	}
	ip := append(net.IP{}, blackholeIP6...)
	copy(ip[net.IPv6len-len(suffix):], suffix)
	port, err := getFreePort()
	require.NoError(t, err)

	a, err := ma.NewMultiaddr(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", port))
	require.NoError(t, err)
	return a
}

func getFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func generateNode(t *testing.T, addr ma.Multiaddr, bootNode string) *hostWrapper {
	tstTcp := &addressFactory{blockAddresses: []string{}}

	node, err := libp2p.New(context.Background(),
		libp2p.ListenAddrs(addr),
		libp2p.DisableRelay(),
		libp2p.NATPortMap(),
		libp2p.AddrsFactory(tstTcp.addressFactory),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, node.Close())
	})
	cnf := DefaultConfig()
	if bootNode != "" {
		cnf.Bootnodes = append(cnf.Bootnodes, bootNode)
	}
	host, err := Upgrade(node, WithConfig(cnf))
	require.NoError(t, err)
	return &hostWrapper{
		host:      host,
		transport: tstTcp,
	}
}

func discoveryBootstrap(disc *peerexchange.Discovery) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for i := 0; i < 5; i++ {
		if err := disc.Bootstrap(ctx); errors.Is(err, context.Canceled) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}
