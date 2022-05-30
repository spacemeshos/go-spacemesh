package addressbook

import (
	"math/rand"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p-core/peer"
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

func TestAddrBook_BootstrapAddressCache(t *testing.T) {
	cnf := DefaultAddressBookConfigWithDataDir("")
	cnf.GetAddrPercent = 100
	cnf.AnchorPeersCount = 10
	book := NewAddrBook(cnf, logtest.New(t))
	rng := rand.New(rand.NewSource(time.Now().Unix()))

	expected := map[peer.ID]*AddrInfo{}
	for i := 0; i < 50; i++ {
		info := genRandomInfo(t, rng)
		expected[info.ID] = info
		book.AddAddress(info, info)
	}

	require.Equal(t, 50, len(book.BootstrapAddressCache()))

	book.mu.Lock()
	for i := 0; i < 10; i++ {
		book.lastAnchorPeers = append(book.lastAnchorPeers, generateKnownAddress(t, rng, nil))
	}
	book.mu.Unlock()

	addresses := book.BootstrapAddressCache()
	require.Equal(t, 60, len(addresses))
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
