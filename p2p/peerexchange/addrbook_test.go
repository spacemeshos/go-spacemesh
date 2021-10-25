package peerexchange

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func Test_Lookup(t *testing.T) {
	n := newAddrBook(Config{}, logtest.New(t))
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
	n := newAddrBook(Config{}, logtest.New(t))

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
	n := newAddrBook(Config{}, logtest.New(t))
	rng := rand.New(rand.NewSource(1001))
	info := genRandomInfo(t, rng)
	addrs := []*addrInfo{}

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
