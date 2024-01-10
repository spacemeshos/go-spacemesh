package hashsync

import (
	"math/rand"
	"slices"
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/stretchr/testify/assert"
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

type catchTransferTwice struct {
	ItemStore
	t     *testing.T
	added map[types.Hash32]bool
}

func (s *catchTransferTwice) Add(k Ordered) {
	h := k.(types.Hash32)
	_, found := s.added[h]
	assert.False(s.t, found, "hash sent twice")
	s.ItemStore.Add(k)
	if s.added == nil {
		s.added = make(map[types.Hash32]bool)
	}
	s.added[h] = true
}

type xorSyncTestConfig struct {
	maxSendRange    int
	numTestHashes   int
	minNumSpecificA int
	maxNumSpecificA int
	minNumSpecificB int
	maxNumSpecificB int
}

func verifyXORSync(t *testing.T, cfg xorSyncTestConfig, sync func(syncA, syncB *RangeSetReconciler, numSpecific int)) {
	numSpecificA := rand.Intn(cfg.maxNumSpecificA+1-cfg.minNumSpecificA) + cfg.minNumSpecificA
	numSpecificB := rand.Intn(cfg.maxNumSpecificB+1-cfg.minNumSpecificB) + cfg.minNumSpecificB
	src := make([]types.Hash32, cfg.numTestHashes)
	for n := range src {
		src[n] = types.RandomHash()
	}

	sliceA := src[:cfg.numTestHashes-numSpecificB]
	storeA := NewSyncTreeStore(Hash32To12Xor{})
	for _, h := range sliceA {
		storeA.Add(h)
	}
	storeA = &catchTransferTwice{t: t, ItemStore: storeA}
	syncA := NewRangeSetReconciler(storeA, WithMaxSendRange(cfg.maxSendRange))

	sliceB := append([]types.Hash32(nil), src[:cfg.numTestHashes-numSpecificB-numSpecificA]...)
	sliceB = append(sliceB, src[cfg.numTestHashes-numSpecificB:]...)
	storeB := NewSyncTreeStore(Hash32To12Xor{})
	for _, h := range sliceB {
		storeB.Add(h)
	}
	storeB = &catchTransferTwice{t: t, ItemStore: storeB}
	syncB := NewRangeSetReconciler(storeB, WithMaxSendRange(cfg.maxSendRange))

	slices.SortFunc(src, func(a, b types.Hash32) int {
		return a.Compare(b)
	})

	sync(syncA, syncB, numSpecificA+numSpecificB)

	itemsA := collectStoreItems[types.Hash32](storeA)
	itemsB := collectStoreItems[types.Hash32](storeB)
	require.Equal(t, itemsA, itemsB)
	require.Equal(t, src, itemsA)
}

func TestBigSyncHash32(t *testing.T) {
	cfg := xorSyncTestConfig{
		maxSendRange:    1,
		numTestHashes:   100000,
		minNumSpecificA: 4,
		maxNumSpecificA: 100,
		minNumSpecificB: 4,
		maxNumSpecificB: 100,
	}
	verifyXORSync(t, cfg, func(syncA, syncB *RangeSetReconciler, numSpecific int) {
		nRounds, nMsg, nItems := runSync(t, syncA, syncB, 100)
		itemCoef := float64(nItems) / float64(numSpecific)
		t.Logf("numSpecific: %d, nRounds: %d, nMsg: %d, nItems: %d, itemCoef: %.2f",
			numSpecific, nRounds, nMsg, nItems, itemCoef)
	})
}
