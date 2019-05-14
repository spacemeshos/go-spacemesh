package discovery

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/stretchr/testify/require"

	"testing"
)

func testAddrBook(name string) *addrBook {
	return NewAddrBook(generateDiscNode(), config.DefaultConfig().SwarmConfig, GetTestLogger(name))
}

func TestStartStop(t *testing.T) {
	n := NewAddrBook(generateDiscNode(), config.DefaultConfig().SwarmConfig, GetTestLogger("starttest"))
	n.Start()
	err := n.Stop()
	if err != nil {
		t.Fatalf("Address Manager failed to stop: %v", err)
	}
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
	n.Attempt(na)

	if ka.LastAttempt().IsZero() {
		t.Errorf("Address should have an attempt, but does not")
	}
}

func TestNeedMoreAddresses(t *testing.T) {
	n := testAddrBook("needmoreaddr")
	addrsToAdd := 1500
	b := n.NeedMoreAddresses()
	if !b {
		t.Errorf("Expected that we need more addresses")
	}

	//randnode := generateDiscNode()
	//rds := NodeInfoFromNode(randnode.Node173, 144, 173, 111), 8333

	n.AddAddresses(generateDiscNodesFakeIPs(1500), n.localAddress)
	numAddrs := n.NumAddresses()
	if numAddrs > addrsToAdd {
		t.Errorf("Number of addresses is too many %d vs %d", numAddrs, addrsToAdd)
	}

	fmt.Println(numAddrs)

	b = n.NeedMoreAddresses()
	if b {
		t.Errorf("Expected that we don't need more addresses")
	}
}

func TestGood(t *testing.T) {
	n := testAddrBook("testgood")
	addrsToAdd := 64 * 64

	addrs := generateDiscNodes(addrsToAdd)

	n.AddAddresses(addrs, n.localAddress)
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
	n.AddAddress(n2, n.localAddress)

	ka := n.GetAddress()
	if ka == nil {
		t.Fatalf("Did not get an address where there is one in the pool")
	}
	if ka.na.parsedIP.String() != n2.parsedIP.String() {
		t.Errorf("Wrong IP: got %v, want %v", ka.na.parsedIP.String(), n2.parsedIP.String())
	}

	// Mark this as a good address and get it
	n.Good(ka.na.PublicKey())
	ka = n.GetAddress()
	if ka == nil {
		t.Fatalf("Did not get an address where there is one in the pool")
	}
	if ka.na.parsedIP.String() != n2.parsedIP.String() {
		t.Errorf("Wrong IP: got %v, want %v", ka.na.parsedIP.String(), n2.parsedIP.String())
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
	require.Equal(t, c, emptyNodeInfo)

	// Add a new address and get it
	n.AddAddress(n2, n.localAddress)

	ka := n.GetAddress()
	if ka == nil {
		t.Fatalf("Did not get an address where there is one in the pool")
	}
	if ka.na.parsedIP.String() != n2.parsedIP.String() {
		t.Errorf("Wrong IP: got %v, want %v", ka.na.parsedIP.String(), n2.parsedIP.String())
	}

	n.AddAddresses(generateDiscNodes(100), n.localAddress)

	got, err := n.Lookup(n2.PublicKey())
	require.NoError(t, err)
	require.Equal(t, got, n2)
}
