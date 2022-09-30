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
	p2putil "github.com/libp2p/go-libp2p-testing/netutil"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/transport"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/addressbook"
	"github.com/spacemeshos/go-spacemesh/p2p/peerexchange"
)

type hostWrapper struct {
	host      *Host
	addr      ma.Multiaddr
	transport *addressFactory
}

// addressFactory is a mock implementation of the addressFactory interface. In real app autorelaly manage it.
type addressFactory struct {
	blockAddressesMu *sync.RWMutex
	blockAddresses   []string
	closed           bool
	mark             chan struct{}
	traceAddr        map[string]struct{} // keep addresses which factory receive
	traceAddrMu      *sync.RWMutex
}

func (t *addressFactory) addBlockList(blockAddress string) {
	t.blockAddressesMu.Lock()
	t.blockAddresses = append(t.blockAddresses, blockAddress)
	t.blockAddressesMu.Unlock()
}

func (t *addressFactory) traceAddresses() []string {
	t.traceAddrMu.RLock()
	defer t.traceAddrMu.RUnlock()
	result := make([]string, 0)
	for addr := range t.traceAddr {
		result = append(result, addr)
	}
	return result
}

func (t *addressFactory) addressFactory(addrs []ma.Multiaddr) []ma.Multiaddr {
	t.blockAddressesMu.RLock()
	defer t.blockAddressesMu.RUnlock()
	result := make([]ma.Multiaddr, 0)
	for _, addr := range addrs {
		t.traceAddrMu.Lock()
		t.traceAddr[addr.String()] = struct{}{}
		t.traceAddrMu.Unlock()
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

type hostAddressResolver struct {
	createdAddresses   []ma.Multiaddr
	createdAddressesMu *sync.RWMutex
}

func (h *hostAddressResolver) getAddress(t *testing.T) (addr ma.Multiaddr) {
	for {
		addr = generateAddressV4(t)
		if !h.checkAddressExist(addr) {
			break
		}
	}

	h.createdAddressesMu.Lock()
	defer h.createdAddressesMu.Unlock()
	h.createdAddresses = append(h.createdAddresses, addr)
	return addr
}

func (h *hostAddressResolver) checkAddressExist(checkAddr ma.Multiaddr) bool {
	h.createdAddressesMu.RLock()
	defer h.createdAddressesMu.RUnlock()
	for _, addr := range h.createdAddresses {
		if addr.Equal(checkAddr) {
			return true
		}
	}
	return false
}

var addrResolve = hostAddressResolver{
	createdAddresses:   make([]ma.Multiaddr, 0),
	createdAddressesMu: &sync.RWMutex{},
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

			nodeA := generateNode(t, "")
			nodeB := generateNode(t, nodeA.host.Addrs()[0].String()+"/p2p/"+nodeA.host.ID().String())

			require.Eventually(t, func() bool {
				discoveryBootstrap(nodeB.host.discovery)
				return len(nodeB.host.Peerstore().Addrs(nodeA.host.ID())) == 1
			}, 4*time.Second, 100*time.Millisecond, "nodeB should have 1 address of nodeA")

			require.Eventually(t, func() bool {
				discoveryBootstrap(nodeA.host.discovery)
				return len(nodeA.host.Peerstore().Addrs(nodeB.host.ID())) == 1
			}, 4*time.Second, 100*time.Millisecond, "nodeA should have 1 address of nodeB")

			// assing new address to nodeA, expect nodeB to be notified.
			addrANew := addrResolve.getAddress(t)
			require.NoError(t, nodeA.host.Network().Listen(addrANew), "nodeA should be able to listen on new address")

			// wait for nodeB to be notified
			require.Eventually(t, func() bool {
				return len(nodeB.host.Peerstore().Addrs(nodeA.host.ID())) == 2
			}, 4*time.Second, 100*time.Millisecond, "nodeB should have 2 addresses of nodeA")
			for _, addr := range nodeB.host.Peerstore().Addrs(nodeA.host.ID()) {
				require.True(t, addr.String() == addrANew.String() || addr.String() == nodeA.addr.String())
			}

			for _, addr := range testCase.block {
				switch addr {
				case oldAddr:
					nodeA.transport.addBlockList(nodeA.addr.String())
				case newAddr:
					nodeA.transport.addBlockList(addrANew.String())
				}
			}

			if len(testCase.block) > 0 {
				<-nodeA.transport.mark // wait until nodes internal ticker update addresses.
			}
			require.Eventually(t, func() bool {
				return len(nodeA.transport.traceAddresses()) == 2
			}, 4*time.Second, 100*time.Millisecond, "factory should have 2 addresses")

			require.NoError(t, nodeA.host.Network().ClosePeer(nodeB.host.ID()), "nodeA should be able to close nodeB")
			require.NoError(t, nodeB.host.Network().ClosePeer(nodeA.host.ID()), "nodeB should be able to close nodeA")

			nodeB.host.discovery.CheckPeers(context.Background())

			if len(testCase.expected) == 2 {
				require.Eventually(t, func() bool {
					return len(nodeB.host.discovery.GetAddresses()) > 0
				}, 4*time.Second, 100*time.Millisecond, "nodeB should have at least 1 address")
			} else {
				peersNum := len(nodeB.host.Peerstore().Addrs(nodeA.host.ID()))
				require.Eventually(t, func() bool {
					return len(testCase.expected) == peersNum
				}, 4*time.Second, 100*time.Millisecond, "nodeB peerstore should have %d addresses, got %d", len(testCase.expected), peersNum)

				require.Eventually(t, func() bool {
					return len(testCase.expected) == len(nodeB.host.discovery.GetAddresses())
				}, 4*time.Second, 100*time.Millisecond, "nodeB should have %d addresses", len(testCase.expected))

				for _, addr := range testCase.expected {
					var expectAddress string
					switch addr {
					case oldAddr:
						expectAddress = nodeA.addr.String()
					case newAddr:
						expectAddress = addrANew.String()
					}
					addressBookAddresses := nodeB.host.discovery.GetAddresses()
					peerStoreAddressList := nodeB.host.Peerstore().Addrs(nodeA.host.ID())
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

func generateNode(t *testing.T, bootNode string) *hostWrapper {
	addrFactory := &addressFactory{
		blockAddressesMu: &sync.RWMutex{},
		traceAddrMu:      &sync.RWMutex{},
		blockAddresses:   []string{},
		mark:             make(chan struct{}),
		traceAddr:        make(map[string]struct{}),
	}

	var (
		node host.Host
		err  error
		addr ma.Multiaddr
	)

	// try to create node on address several times.
	// by default, node can start with other node on same port.
	// when we run multiply tests in parallel on same machine - this can generate flaky tests
	for i := 0; i < 5; i++ {
		addr = addrResolve.getAddress(t)
		node, err = libp2p.New(
			libp2p.ListenAddrs(addr),
			libp2p.DisableRelay(),
			libp2p.NATPortMap(),
			libp2p.Transport(func(upgrader transport.Upgrader, rcmgr network.ResourceManager, opts ...tcp.Option) (*tcp.TcpTransport, error) {
				opts = append(opts, tcp.DisableReuseport())
				return tcp.NewTCPTransport(upgrader, rcmgr, opts...)
			}),
			libp2p.AddrsFactory(addrFactory.addressFactory),
		)
		if err == nil {
			break
		}
	}
	if node == nil {
		t.Fatalf("can't create node after retry: %s", err)
	}

	cnf := DefaultConfig()
	cnf.CheckPeersUsedBefore = time.Millisecond * 1
	if bootNode != "" {
		// adding address to bootstrap list is has random part.
		// for make it more predictable - create book object, set address as connected and persist peers.
		// next time book will restore conf from this file and return this address for bootstrap.
		cnf.DataDir = t.TempDir()
		addrBookConf := addressbook.DefaultAddressBookConfigWithDataDir(cnf.DataDir)
		book := addressbook.NewAddrBook(addrBookConf, logtest.New(t))
		bootAddr, err := addressbook.ParseAddrInfo(bootNode)
		require.NoError(t, err)
		book.AddAddress(bootAddr, bootAddr)
		book.Connected(bootAddr.ID)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		book.Persist(ctx)
		cnf.Bootnodes = append(cnf.Bootnodes, bootNode)
	}
	genesisID := types.Hash20{1}
	h, err := Upgrade(node, genesisID, WithConfig(cnf))
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, node.Close())
		require.NoError(t, h.Stop())
	})
	println("start node on address:", addr.String())
	return &hostWrapper{
		host:      h,
		transport: addrFactory,
		addr:      addr,
	}
}

func discoveryBootstrap(disc *peerexchange.Discovery) {
	for i := 0; i < 5; i++ {
		if err := disc.Bootstrap(context.Background()); errors.Is(err, context.Canceled) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}
