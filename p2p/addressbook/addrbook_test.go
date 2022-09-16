package addressbook

import (
	"math/rand"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func Test_Lookup(t *testing.T) {
	n := NewAddrBook(DefaultAddressBookConfigWithDataDir(""), logtest.New(t))
	rng := rand.New(rand.NewSource(1001))

	info := genRandomInfo(t, rng)
	require.Nil(t, n.Lookup(info.ID))

	n.AddAddress(info, info)
	require.Equal(t, info, n.Lookup(info.ID))

	for i := 0; i < 1000; i++ {
		ith := genRandomInfo(t, rng)
		n.AddAddress(ith, info)
		require.Equal(t, ith, n.Lookup(ith.ID))
	}
}

func TestAttempt(t *testing.T) {
	n := NewAddrBook(DefaultAddressBookConfigWithDataDir(""), logtest.New(t))

	info := genRandomInfo(t, rand.New(rand.NewSource(1001)))
	n.AddAddress(info, info)

	ka := n.addrIndex[info.ID]

	if !ka.LastAttempt.IsZero() {
		t.Errorf("Address should not have attempts, but does")
	}

	n.Attempt(info.ID)

	if ka.LastAttempt.IsZero() {
		t.Errorf("Address should have an attempt, but does not")
	}
}

func TestGood(t *testing.T) {
	n := NewAddrBook(DefaultAddressBookConfigWithDataDir(""), logtest.New(t))
	rng := rand.New(rand.NewSource(1001))
	info := genRandomInfo(t, rng)
	addrs := []*AddrInfo{}

	for i := 0; i < 64*64; i++ {
		addrs = append(addrs, genRandomInfo(t, rng))
	}

	n.AddAddresses(addrs, info)
	for _, info := range addrs {
		n.Good(info.ID)
	}

	numAddrs := n.NumAddresses()
	if numAddrs >= len(addrs) {
		t.Errorf("Number of addresses is too many: %d vs %d", numAddrs, len(addrs))
	}
	numCache := len(n.AddressCache())
	if numCache >= numAddrs/4 {
		t.Errorf("Number of addresses in cache: got %d, want %d", numCache, numAddrs/4)
	}
}

func TestAddrBook_AddAddress(t *testing.T) {
	t.Parallel()
	t.Run("add address", func(t *testing.T) {
		t.Parallel()
		n := NewAddrBook(DefaultAddressBookConfigWithDataDir(""), logtest.New(t))
		rng := rand.New(rand.NewSource(1001))
		require.Equal(t, 0, len(n.GetAddresses()))

		info := genRandomInfo(t, rng)
		info2 := genRandomInfo(t, rng)
		info3 := genRandomInfo(t, rng)

		n.AddAddress(info, info)
		require.Equal(t, 1, len(n.GetAddresses()))

		n.AddAddress(info2, info2)
		require.Equal(t, 2, len(n.GetAddresses()))

		n.AddAddress(info3, info3)
		require.Equal(t, 3, len(n.GetAddresses()))
	})
	t.Run("add address to full bucket", func(t *testing.T) {
		t.Parallel()
		conf := DefaultAddressBookConfigWithDataDir("")
		conf.NewBucketCount = 1
		conf.NewBucketSize = 10
		n := NewAddrBook(conf, logtest.New(t))
		rng := rand.New(rand.NewSource(1001))
		require.Equal(t, 0, len(n.GetAddresses()))
		for i := 0; i < conf.NewBucketSize; i++ {
			info := genRandomInfo(t, rng)
			n.AddAddress(info, info)
		}
		require.Equal(t, conf.NewBucketSize, len(n.GetAddresses()))

		info := genRandomInfo(t, rng)
		n.AddAddress(info, info)
		addresses := n.GetAddresses()
		require.Equal(t, conf.NewBucketSize, len(addresses))
		require.Condition(t, func() bool {
			for _, addr := range addresses {
				if addr.ID == info.ID {
					return true
				}
			}
			return false
		}, "address should be added")
	})
	t.Run("update address", func(t *testing.T) {
		t.Parallel()
		n := NewAddrBook(DefaultAddressBookConfigWithDataDir(""), logtest.New(t))
		rng := rand.New(rand.NewSource(1001))
		require.Equal(t, 0, len(n.GetAddresses()))

		info := genRandomInfo(t, rng)
		info2 := genRandomInfo(t, rng)

		n.AddAddress(info, info)
		require.Equal(t, 1, len(n.GetAddresses()))

		n.AddAddress(info2, info2)
		require.Equal(t, 2, len(n.GetAddresses()))

		kaBefore := getKnownAddress(t, n, info2.ID)
		time.Sleep(100 * time.Millisecond)

		n.AddAddress(info2, info2)
		require.Equal(t, 2, len(n.GetAddresses()))

		kaAfter := getKnownAddress(t, n, info2.ID)
		require.True(t, kaBefore.LastSeen.Before(kaAfter.LastSeen), "LastSeen of address should have increased")
	})
	t.Run("add address with different address", func(t *testing.T) {
		t.Parallel()
		n := NewAddrBook(DefaultAddressBookConfigWithDataDir(""), logtest.New(t))
		rng := rand.New(rand.NewSource(1001))
		require.Equal(t, 0, len(n.GetAddresses()))

		info := genRandomInfo(t, rng)
		info2 := genRandomInfo(t, rng)
		info3 := genRandomInfo(t, rng)

		n.AddAddress(info, info)
		require.Equal(t, 1, len(n.GetAddresses()))

		n.AddAddress(info2, info2)
		require.Equal(t, 2, len(n.GetAddresses()))

		info2Mod := *info2
		info2Mod.RawAddr = info3.RawAddr
		n.AddAddress(&info2Mod, info3)
		require.Equal(t, 2, len(n.GetAddresses()))

		kaUpdated := getKnownAddress(t, n, info2.ID)
		require.Equal(t, info3.RawAddr, kaUpdated.Addr.RawAddr)
	})
}

func getKnownAddress(t *testing.T, n *AddrBook, p peer.ID) knownAddress {
	n.mu.Lock()
	defer n.mu.Unlock()
	ka, ok := n.addrIndex[p]
	require.True(t, ok)
	return *ka
}

func TestAddrBook_GetAllAddressesUsedBefore(t *testing.T) {
	t.Parallel()
	n := NewAddrBook(DefaultAddressBookConfigWithDataDir(""), logtest.New(t))
	rng := rand.New(rand.NewSource(1001))

	info := genRandomInfo(t, rng)
	require.Equal(t, 0, len(n.GetAddresses()))
	n.AddAddress(info, info)
	n.Good(info.ID)

	require.Equal(t, 1, len(n.GetAddressesNotConnectedSince(time.Now())))
	require.Equal(t, 0, len(n.GetAddressesNotConnectedSince(time.Now().Add(-1*time.Minute))))

	time.Sleep(10 * time.Millisecond)
	require.Equal(t, 1, len(n.GetAddressesNotConnectedSince(time.Now().Add(-1*time.Millisecond))))
}

func TestAddrBook_RemoveAddress(t *testing.T) {
	n := NewAddrBook(DefaultAddressBookConfigWithDataDir(""), logtest.New(t))
	rng := rand.New(rand.NewSource(1001))
	require.Equal(t, 0, len(n.GetAddresses()))

	info := genRandomInfo(t, rng)
	n.AddAddress(info, info)
	n.Good(info.ID)
	n.Connected(info.ID)

	require.NotNil(t, n.Lookup(info.ID))
	require.Equal(t, info.ID, n.Lookup(info.ID).ID)

	n.RemoveAddress(info.ID)
	require.Nil(t, n.Lookup(info.ID))

	n.RemoveAddress(info.ID)
	require.Nil(t, n.Lookup(info.ID))
}

func TestAddrBook_Connected(t *testing.T) {
	n := NewAddrBook(DefaultAddressBookConfigWithDataDir(""), logtest.New(t))
	rng := rand.New(rand.NewSource(1001))

	info := genRandomInfo(t, rng)
	n.AddAddress(info, info)
	n.Connected(info.ID)

	require.Equal(t, 1, len(n.AddressCache()))
	n.mu.Lock()
	defer n.mu.Unlock()
	require.Equal(t, 1, len(n.anchorPeers))
}

func TestAddrBook_BootstrapAddressCache(t *testing.T) {
	cnf := DefaultAddressBookConfigWithDataDir("")
	cnf.GetAddrPercent = 100
	cnf.AnchorPeersCount = 10
	book := NewAddrBook(cnf, logtest.New(t))
	rng := rand.New(rand.NewSource(time.Now().Unix()))

	expected := make([]*AddrInfo, 0, 50)
	for i := 0; i < 50; i++ {
		info := genRandomInfo(t, rng)
		expected = append(expected, info)
		book.AddAddress(info, info)
	}

	// as addresses append in random way, check that every bootstrap address is from generated list
	require.True(t, checkAddressContains(expected, book.BootstrapAddressCache()), "bootstrap address cache should contain generated addresses")
	var expectedAnchors []*AddrInfo
	book.mu.Lock()
	for i := 0; i < 10; i++ {
		ka := generateKnownAddress(t, rng, nil)
		book.lastAnchorPeers = append(book.lastAnchorPeers, ka)
		expectedAnchors = append(expectedAnchors, ka.Addr)
	}
	book.mu.Unlock()

	bootstrapAddr := book.BootstrapAddressCache()
	var anchorsContains []bool
	for _, ancAddr := range expectedAnchors {
		var exist bool
		for _, addr := range bootstrapAddr {
			if addr.String() == ancAddr.String() {
				exist = true
				break
			}
		}
		if exist {
			anchorsContains = append(anchorsContains, true)
		}
	}
	require.Contains(t, anchorsContains, true, "bootstrap address cache should contain anchors")
}

func checkAddressContains(expected, result []*AddrInfo) bool {
	for _, addr := range result {
		var match bool
		for _, e := range expected {
			if e.String() == addr.String() {
				match = true
			}
		}
		if !match {
			return false
		}
	}
	return true
}

func TestAddrBook_AddressCache(t *testing.T) {
	cnf := DefaultAddressBookConfigWithDataDir("")
	rng := rand.New(rand.NewSource(time.Now().Unix()))
	t.Run("with small number of addresses", func(t *testing.T) {
		book := NewAddrBook(cnf, logtest.New(t))
		patchAddressBook(book, []*knownAddress{
			generateKnownAddress(t, rng, nil),
			generateKnownAddress(t, rng, nil),
		}, []*knownAddress{
			generateKnownAddress(t, rng, nil),
			generateKnownAddress(t, rng, nil),
		})
		addresses := book.AddressCache()
		require.Equal(t, 4, len(addresses), "addresses should contain 4 addresses")
	})
	t.Run("address from new buckets", func(t *testing.T) {
		book := NewAddrBook(cnf, logtest.New(t))
		newAddress := []*knownAddress{
			generateKnownAddress(t, rng, nil),
			generateKnownAddress(t, rng, nil),
			generateKnownAddress(t, rng, nil),
			generateKnownAddress(t, rng, nil),
			generateKnownAddress(t, rng, nil),
			generateKnownAddress(t, rng, nil),
			generateKnownAddress(t, rng, nil),
		}
		patchAddressBook(book, newAddress, []*knownAddress{})
		addresses := book.AddressCache()
		require.Equal(t, 1, len(addresses), "addresses should contain 1 address")
		require.Condition(t, func() bool {
			for _, addr := range newAddress {
				if addr.SrcAddr.RawAddr == addresses[0].RawAddr {
					return true
				}
			}
			return false
		}, "valid address `triedOld` or `newOld`")
	})
	t.Run("address from trieds buckets", func(t *testing.T) {
		book := NewAddrBook(cnf, logtest.New(t))
		triedAddress := []*knownAddress{
			generateKnownAddress(t, rng, nil),
			generateKnownAddress(t, rng, nil),
			generateKnownAddress(t, rng, nil),
			generateKnownAddress(t, rng, nil),
			generateKnownAddress(t, rng, nil),
			generateKnownAddress(t, rng, nil),
			generateKnownAddress(t, rng, nil),
		}
		patchAddressBook(book, []*knownAddress{}, triedAddress)
		addresses := book.AddressCache()
		require.Equal(t, 1, len(addresses), "addresses should contain 1 address")
		require.Condition(t, func() bool {
			for _, addr := range triedAddress {
				if addr.SrcAddr.RawAddr == addresses[0].RawAddr {
					return true
				}
			}
			return false
		}, "valid address `triedOld` or `newOld`")
	})
}

func patchAddressBook(book *AddrBook, new, tried []*knownAddress) {
	for _, ka := range new {
		book.AddAddress(ka.SrcAddr, ka.Addr)
		book.mu.Lock()
		book.addrIndex[ka.SrcAddr.ID].LastAttempt = ka.LastAttempt
		book.addrIndex[ka.SrcAddr.ID].Attempts = ka.Attempts
		book.mu.Unlock()
	}

	for _, ka := range tried {
		book.AddAddress(ka.SrcAddr, ka.Addr)
		book.Good(ka.SrcAddr.ID)
		book.mu.Lock()
		book.addrIndex[ka.SrcAddr.ID].LastAttempt = ka.LastAttempt
		book.addrIndex[ka.SrcAddr.ID].Attempts = ka.Attempts
		book.mu.Unlock()
	}
}

func generateKnownAddress(t *testing.T, rng *rand.Rand, lastAttempt *time.Time) *knownAddress {
	ka := &knownAddress{
		Addr:        genRandomInfo(t, rng),
		SrcAddr:     genRandomInfo(t, rng),
		LastSeen:    time.Now(),
		Attempts:    1,
		LastAttempt: time.Now(),
		LastSuccess: time.Now(),
		tried:       true,
		refs:        1,
	}
	if lastAttempt != nil {
		ka.LastAttempt = *lastAttempt
	}
	return ka
}
