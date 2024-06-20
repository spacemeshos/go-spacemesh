package hashsync

import (
	"context"
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

func collectStoreItems[K Ordered](is ItemStore) (r []K) {
	it, err := is.Min()
	if err != nil {
		panic("store min error")
	}
	if it == nil {
		return nil
	}
	endAt, err := is.Min()
	if err != nil {
		panic("store min error")
	}
	for {
		r = append(r, it.Key().(K))
		if err := it.Next(); err != nil {
			panic("iterator error")
		}
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

func (s *catchTransferTwice) Add(ctx context.Context, k Ordered) error {
	h := k.(types.Hash32)
	_, found := s.added[h]
	assert.False(s.t, found, "hash sent twice")
	if err := s.ItemStore.Add(ctx, k); err != nil {
		return err
	}
	if s.added == nil {
		s.added = make(map[types.Hash32]bool)
	}
	s.added[h] = true
	return nil
}

type xorSyncTestConfig struct {
	maxSendRange    int
	numTestHashes   int
	minNumSpecificA int
	maxNumSpecificA int
	minNumSpecificB int
	maxNumSpecificB int
}

func verifyXORSync(t *testing.T, cfg xorSyncTestConfig, sync func(storeA, storeB ItemStore, numSpecific int, opts []RangeSetReconcilerOption) bool) {
	opts := []RangeSetReconcilerOption{
		WithMaxSendRange(cfg.maxSendRange),
	}
	numSpecificA := rand.Intn(cfg.maxNumSpecificA+1-cfg.minNumSpecificA) + cfg.minNumSpecificA
	numSpecificB := rand.Intn(cfg.maxNumSpecificB+1-cfg.minNumSpecificB) + cfg.minNumSpecificB
	src := make([]types.Hash32, cfg.numTestHashes)
	for n := range src {
		src[n] = types.RandomHash()
	}

	sliceA := src[:cfg.numTestHashes-numSpecificB]
	storeA := NewSyncTreeStore(Hash32To12Xor{})
	for _, h := range sliceA {
		require.NoError(t, storeA.Add(context.Background(), h))
	}
	storeA = &catchTransferTwice{t: t, ItemStore: storeA}

	sliceB := append([]types.Hash32(nil), src[:cfg.numTestHashes-numSpecificB-numSpecificA]...)
	sliceB = append(sliceB, src[cfg.numTestHashes-numSpecificB:]...)
	storeB := NewSyncTreeStore(Hash32To12Xor{})
	for _, h := range sliceB {
		require.NoError(t, storeB.Add(context.Background(), h))
	}
	storeB = &catchTransferTwice{t: t, ItemStore: storeB}

	slices.SortFunc(src, func(a, b types.Hash32) int {
		return a.Compare(b)
	})

	if sync(storeA, storeB, numSpecificA+numSpecificB, opts) {
		itemsA := collectStoreItems[types.Hash32](storeA)
		itemsB := collectStoreItems[types.Hash32](storeB)
		require.Equal(t, itemsA, itemsB)
		srcKeys := make([]types.Hash32, len(src))
		for n, h := range src {
			srcKeys[n] = h
		}
		require.Equal(t, srcKeys, itemsA)
	}
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
	verifyXORSync(t, cfg, func(storeA, storeB ItemStore, numSpecific int, opts []RangeSetReconcilerOption) bool {
		syncA := NewRangeSetReconciler(storeA, opts...)
		syncB := NewRangeSetReconciler(storeB, opts...)
		nRounds, nMsg, nItems := runSync(t, syncA, syncB, 100)
		itemCoef := float64(nItems) / float64(numSpecific)
		t.Logf("numSpecific: %d, nRounds: %d, nMsg: %d, nItems: %d, itemCoef: %.2f",
			numSpecific, nRounds, nMsg, nItems, itemCoef)
		return true
	})
}
