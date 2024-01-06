package hashsync

import (
	"math/rand"
	"slices"
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/stretchr/testify/require"
)

func TestHash32To12Xor(t *testing.T) {
	var m Hash32To12Xor
	require.Equal(t, m.Identity(), m.Op(m.Identity(), m.Identity()))
	hash1 := types.CalcHash32([]byte("foo"))
	fp1 := m.Fingerprint(hash1)
	hash2 := types.CalcHash32([]byte("bar"))
	fp2 := m.Fingerprint(hash2)
	hash3 := types.CalcHash32([]byte("baz"))
	fp3 := m.Fingerprint(hash3)
	require.Equal(t, fp1, m.Op(m.Identity(), fp1))
	require.Equal(t, fp2, m.Op(fp2, m.Identity()))
	require.NotEqual(t, fp1, fp2)
	require.NotEqual(t, fp1, fp3)
	require.NotEqual(t, fp1, m.Op(fp1, fp2))
	require.NotEqual(t, fp2, m.Op(fp1, fp2))
	require.NotEqual(t, m.Identity(), m.Op(fp1, fp2))
	require.Equal(t, m.Op(m.Op(fp1, fp2), fp3), m.Op(fp1, m.Op(fp2, fp3)))
}

func collectStoreItems[T Ordered](is ItemStore) (r []T) {
	it := is.Min()
	if it == nil {
		return nil
	}
	endAt := is.Min()
	for {
		r = append(r, it.Key().(T))
		it.Next()
		if it.Equal(endAt) {
			return r
		}
	}
}

const numTestHashes = 100000

// const numTestHashes = 100

type catchTransferTwice struct {
	ItemStore
	t     *testing.T
	added map[types.Hash32]bool
}

func (s *catchTransferTwice) Add(k Ordered) {
	h := k.(types.Hash32)
	_, found := s.added[h]
	require.False(s.t, found, "hash sent twice")
	s.ItemStore.Add(k)
	if s.added == nil {
		s.added = make(map[types.Hash32]bool)
	}
	s.added[h] = true
}

const xorTestMaxSendRange = 1

func TestBigSyncHash32(t *testing.T) {
	numSpecificA := rand.Intn(96) + 4
	numSpecificB := rand.Intn(96) + 4
	// numSpecificA := rand.Intn(6) + 4
	// numSpecificB := rand.Intn(6) + 4
	src := make([]types.Hash32, numTestHashes)
	for n := range src {
		src[n] = types.RandomHash()
	}

	sliceA := src[:numTestHashes-numSpecificB]
	storeA := NewMonoidTreeStore(Hash32To12Xor{})
	for _, h := range sliceA {
		storeA.Add(h)
	}
	storeA = &catchTransferTwice{t: t, ItemStore: storeA}
	syncA := NewRangeSetReconciler(storeA, WithMaxSendRange(xorTestMaxSendRange))

	sliceB := append([]types.Hash32(nil), src[:numTestHashes-numSpecificB-numSpecificA]...)
	sliceB = append(sliceB, src[numTestHashes-numSpecificB:]...)
	storeB := NewMonoidTreeStore(Hash32To12Xor{})
	for _, h := range sliceB {
		storeB.Add(h)
	}
	storeB = &catchTransferTwice{t: t, ItemStore: storeB}
	syncB := NewRangeSetReconciler(storeB, WithMaxSendRange(xorTestMaxSendRange))

	nRounds, nMsg, nItems := runSync(t, syncA, syncB, 100)
	excess := float64(nItems-numSpecificA-numSpecificB) / float64(numSpecificA+numSpecificB)
	t.Logf("numSpecificA: %d, numSpecificB: %d, nRounds: %d, nMsg: %d, nItems: %d, excess: %.2f",
		numSpecificA, numSpecificB, nRounds, nMsg, nItems, excess)

	slices.SortFunc(src, func(a, b types.Hash32) int {
		return a.Compare(b)
	})
	itemsA := collectStoreItems[types.Hash32](storeA)
	itemsB := collectStoreItems[types.Hash32](storeB)
	require.Equal(t, itemsA, itemsB)
	require.Equal(t, src, itemsA)
}

// TODO: try catching items sent twice in a simpler test
// TODO: check why insertion takes so long (1000000 items => too long wait)
// TODO: number of items transferred is unreasonable for 100k total / 1 range size:
// xorsync_test.go:56: numSpecificA: 141, numSpecificB: 784, nRounds: 11, nMsg: 13987, nItems: 3553
