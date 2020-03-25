package discovery

import (
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/stretchr/testify/require"

	"testing"
)

func testAddrBook(name string) *addrBook {
	book := newAddrBook(config.DefaultConfig().SwarmConfig, "", GetTestLogger(name))
	book.localAddresses = append(book.localAddresses, generateDiscNode())
	return book
}

func TestStartStop(t *testing.T) {
	n := newAddrBook(config.DefaultConfig().SwarmConfig, "", GetTestLogger("starttest"))
	n.Start()
	n.Stop()
}

func TestAttempt(t *testing.T) {
	n := testAddrBook("attemptest")

	nd := generateDiscNode()
	// Add a new address and get it
	n.AddAddress(nd, nd)

	ka := n.GetAddress()

	if !ka.LastAttempt().IsZero() {
		t.Errorf("Address should not have attempts, but does")
	}

	na := ka.na
	n.Attempt(na.PublicKey())

	if ka.LastAttempt().IsZero() {
		t.Errorf("Address should have an attempt, but does not")
	}
}

func TestGood(t *testing.T) {
	n := testAddrBook("testgood")
	addrsToAdd := 64 * 64

	addrs := generateDiscNodes(addrsToAdd)

	n.AddAddresses(addrs, n.localAddresses[0])
	for _, addr := range addrs {
		n.Good(addr.PublicKey())
	}

	numAddrs := n.NumAddresses()
	if numAddrs >= addrsToAdd {
		t.Errorf("Number of addresses is too many: %d vs %d", numAddrs, addrsToAdd)
	}

	numCache := len(n.AddressCache())
	if numCache >= numAddrs/4 {
		t.Errorf("Number of addresses in cache: got %d, want %d", numCache, numAddrs/4)
	}
}

func TestGetAddress(t *testing.T) {
	n := testAddrBook("getaddress")

	// Get an address from an empty set (should error)
	if rv := n.GetAddress(); rv != nil {
		t.Errorf("GetAddress failed: got: %v want: %v\n", rv, nil)
	}
	n2 := generateDiscNode()

	// Add a new address and get it
	n.AddAddress(n2, n.localAddresses[0])

	ka := n.GetAddress()
	if ka == nil {
		t.Fatalf("Did not get an address where there is one in the pool")
	}
	if !ka.na.IP.Equal(n2.IP) {
		t.Errorf("Wrong IP: got %v, want %v", ka.na.IP.String(), n2.IP.String())
	}

	// Mark this as a good address and get it
	n.Good(ka.na.PublicKey())
	ka = n.GetAddress()
	if ka == nil {
		t.Fatalf("Did not get an address where there is one in the pool")
	}
	if !ka.na.IP.Equal(n2.IP) {
		t.Errorf("Wrong IP: got %v, want %v", ka.na.IP.String(), n2.IP.String())
	}

	numAddrs := n.NumAddresses()
	if numAddrs != 1 {
		t.Errorf("Wrong number of addresses: got %d, want %d", numAddrs, 1)
	}
}

func Test_Lookup(t *testing.T) {
	n := testAddrBook("lookup")

	n2 := generateDiscNode()

	c, err := n.Lookup(n2.PublicKey())
	require.Error(t, err)
	require.Nil(t, c)

	// Add a new address and get it
	n.AddAddress(n2, n.localAddresses[0])

	ka := n.GetAddress()
	if ka == nil {
		t.Fatalf("Did not get an address where there is one in the pool")
	}
	if !ka.na.IP.Equal(n2.IP) {
		t.Errorf("Wrong IP: got %v, want %v", ka.na.IP.String(), n2.IP.String())
	}

	n.AddAddresses(generateDiscNodes(100), n.localAddresses[0])

	got, err := n.Lookup(n2.PublicKey())
	require.NoError(t, err)
	require.Equal(t, got, n2)
}

func Test_LocalAddreses(t *testing.T) {
	n := testAddrBook(t.Name())

	addr := n.localAddresses[0]
	addr2 := generateDiscNode()
	n.AddLocalAddress(addr2)

	require.True(t, n.IsLocalAddress(addr2))
	require.True(t, n.IsLocalAddress(addr))

	n.AddAddress(addr2, addr)
	n.AddAddress(addr, addr2)

	_, err := n.Lookup(addr.PublicKey())
	require.Error(t, err)
	_, err = n.Lookup(addr2.PublicKey())
	require.Error(t, err)

	addr3 := generateDiscNode()
	n.AddAddress(addr3, addr)
	nd, err := n.Lookup(addr3.PublicKey())
	require.NoError(t, err)
	require.NotNil(t, nd)
	require.Equal(t, nd.ID, addr3.ID)
}
