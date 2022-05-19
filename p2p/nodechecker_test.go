package p2p

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
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
	blockAddressesMu *sync.RWMutex
	blockAddresses   []string
	closed           bool
	mark             chan struct{}
}

func (t *addressFactory) addBlockList(blockAddress string) {
	t.blockAddressesMu.Lock()
	t.blockAddresses = append(t.blockAddresses, blockAddress)
	t.blockAddressesMu.Unlock()
}

func (t *addressFactory) addressFactory(addrs []ma.Multiaddr) []ma.Multiaddr {
	t.blockAddressesMu.RLock()
	defer t.blockAddressesMu.RUnlock()
	result := make([]ma.Multiaddr, 0)
	for _, addr := range addrs {
		blocked := false
		for _, blockAddr := range t.blockAddresses {
			if addr.String() == blockAddr {
				blocked = true
			}
		}
		if !blocked {
			result = append(result, addr)
		}
	}
	if len(t.blockAddresses) > 0 {
		if !t.closed {
			close(t.mark)
			t.closed = true
		}
	}
	return result
}

func TestHost_LocalAddressChange(t *testing.T) {
	t.Parallel()
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
	for _, tc := range table {
		testCase := tc
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
					nodeA.transport.addBlockList(addrAOld.String())
				case newAddr:
					nodeA.transport.addBlockList(addrANew.String())
				}
			}

			if len(testCase.block) > 0 {
				<-nodeA.transport.mark // wait until nodes internal ticker update addresses.
			}
			require.NoError(t, nodeA.host.Network().ClosePeer(nodeB.host.ID()), "nodeA should be able to close nodeB")
			require.NoError(t, nodeB.host.Network().ClosePeer(nodeA.host.ID()), "nodeB should be able to close nodeA")
			time.Sleep(100 * time.Millisecond)

			nodeB.host.discovery.CheckPeers(context.Background())
			time.Sleep(100 * time.Millisecond)

			addressBookAddresses := nodeB.host.discovery.GetAddresses()
			if len(testCase.expected) == 2 {
				require.True(t, len(addressBookAddresses) > 0, "nodeB should have at least 1 address")
			} else {
				peerStoreAddressList := nodeB.host.Peerstore().Addrs(nodeA.host.ID())
				require.Equal(t, len(testCase.expected), len(peerStoreAddressList), "nodeB should have %d addresses", len(testCase.expected))
				require.Equal(t, len(testCase.expected), len(addressBookAddresses), "nodeB should have %d addresses", len(testCase.expected))
				for _, addr := range testCase.expected {
					var expectAddress string
					switch addr {
					case oldAddr:
						expectAddress = addrAOld.String()
					case newAddr:
						expectAddress = addrANew.String()
					}
					require.Equal(t, expectAddress, peerStoreAddressList[0].String(), "nodeB should have correct address of nodeA")
					require.Equal(t, expectAddress+"/p2p/"+nodeA.host.ID().String(), addressBookAddresses[0].String(), "nodeB should have correct address of nodeA")
				}
			}
		})
	}
}

func generateAddressV4(t *testing.T) ma.Multiaddr {
	blackholeIP6 := net.ParseIP("100::")
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
	addrFactory := &addressFactory{
		blockAddressesMu: &sync.RWMutex{},
		blockAddresses:   []string{},
		mark:             make(chan struct{}),
	}

	node, err := libp2p.New(context.Background(),
		libp2p.ListenAddrs(addr),
		libp2p.DisableRelay(),
		libp2p.NATPortMap(),
		libp2p.AddrsFactory(addrFactory.addressFactory),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, node.Close())
	})
	cnf := DefaultConfig()
	cnf.CheckPeersUsedBefore = time.Millisecond * 1
	if bootNode != "" {
		cnf.Bootnodes = append(cnf.Bootnodes, bootNode)
	}
	host, err := Upgrade(node, WithConfig(cnf))
	require.NoError(t, err)
	return &hostWrapper{
		host:      host,
		transport: addrFactory,
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
