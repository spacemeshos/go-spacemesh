package discovery

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/config"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"io/ioutil"
	"os"
	"testing"
)

// assertAddr ensures that the two addresses match. The timestamp is not
// checked as it does not affect uniquely identifying a specific address.
func assertAddr(t *testing.T, got, expected *node.Info) {
	if got.ID != expected.ID {
		t.Fatalf("expected ID %v, got %v", expected.ID.String(), got.ID.String())
	}
	if !got.IP.Equal(expected.IP) {
		t.Fatalf("expected address IP %v, got %v", expected.IP, got.IP)
	}
	if got.DiscoveryPort != expected.DiscoveryPort {
		t.Fatalf("expected address port %d, got %d", expected.DiscoveryPort,
			got.DiscoveryPort)
	}
	if got.ProtocolPort != expected.ProtocolPort {
		t.Fatalf("expected address port %d, got %d", expected.ProtocolPort,
			got.ProtocolPort)
	}
}

// assertAddrs ensures that the manager's address cache matches the given
// expected addresses.
func assertAddrs(t *testing.T, addrMgr *addrBook,
	expectedAddrs map[p2pcrypto.PublicKey]*node.Info) {

	t.Helper()

	addrs := addrMgr.getAddresses()

	if len(addrs) != len(expectedAddrs) {
		t.Fatalf("expected to lookup %d addresses, found %d",
			len(expectedAddrs), len(addrs))
	}

	for _, addr := range addrs {
		addrStr := addr.ID
		expectedAddr, ok := expectedAddrs[addr.PublicKey()]
		if !ok {
			t.Fatalf("expected to lookup address %v", addrStr)
		}

		assertAddr(t, addr, expectedAddr)
	}
}

// TestAddrManagerSerialization ensures that we can properly serialize and
// deserialize the manager's current address cache.
func TestAddrManagerSerialization(t *testing.T) {

	lg := log.New("addrbook_serialize_test", "", "")
	cfg := config.DefaultConfig()

	// We'll start by creating our address manager backed by a temporary
	// directory.
	tempDir, err := ioutil.TempDir("", "addrbook")
	if err != nil {
		t.Fatalf("unable to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	filePath := tempDir + "/" + defaultPeersFileName

	addrMgr := newAddrBook(cfg.SwarmConfig, "", lg)

	// We'll be adding 5 random addresses to the manager.
	const numAddrs = 5

	expectedAddrs := make(map[p2pcrypto.PublicKey]*node.Info, numAddrs)
	for i := 0; i < numAddrs; i++ {
		addr := node.GenerateRandomNodeData()
		expectedAddrs[addr.PublicKey()] = addr
		addrMgr.AddAddress(addr, node.GenerateRandomNodeData())
	}

	// Now that the addresses have been added, we should be able to retrieve
	// them.
	assertAddrs(t, addrMgr, expectedAddrs)
	//
	//// Then, we'll persist these addresses to disk and restart the address
	//// manager.
	addrMgr.savePeers(filePath)
	addrMgr = newAddrBook(cfg.SwarmConfig, "", lg)

	// Finally, we'll read all of the addresses from disk and ensure they
	// match as expected.
	addrMgr.loadPeers(filePath)
	assertAddrs(t, addrMgr, expectedAddrs)
}
