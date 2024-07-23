package skiplist

import (
	"bytes"
	crand "crypto/rand"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// checkSanity is based on test from https://github.com/sean-public/fast-skiplist
func checkSanity(t *testing.T, sl *SkipList) {
	// each level must be correctly ordered
	for l, next := range sl.head.nextNodes {
		if next == nil {
			continue
		}

		if l > len(next.nextNodes) {
			t.Fatal("first node's level must be no less than current level")
		}

		for next.nextNodes[l] != nil {
			require.Positive(t, bytes.Compare(next.nextNodes[l].key, next.key),
				"next key value must be greater than prev key value. [next:%v] [prev:%v]",
				next.nextNodes[l].key, next.key)
			require.GreaterOrEqual(t, len(next.nextNodes), l,
				"node's level must be >= it's predecessor level. [cur:%v] [nextNodes:%v]",
				l, next.nextNodes)

			next = next.nextNodes[l]
		}
	}
}

func enumerate(sl *SkipList) [][]byte {
	var r [][]byte
	for node := sl.First(); node != nil; node = node.Next() {
		r = append(r, node.Key())
	}
	return r
}

func dumpSkipList(t *testing.T, sl *SkipList) {
	nodeIndices := make(map[*Node]int)
	n := 0
	for node := sl.First(); node != nil; node = node.Next() {
		nodeIndices[node] = n
		n++
	}
	for l, next := range sl.head.nextNodes {
		var b strings.Builder
		for next != nil {
			fmt.Fprintf(&b, " --> %d", nodeIndices[next])
			next = next.nextNodes[l]
		}
		t.Logf("Level %02d:%s", l, b.String())
	}

	for node := sl.First(); node != nil; node = node.Next() {
		t.Logf("Node %d: %v (height %d)", nodeIndices[node], node.Key(), node.height())
	}
}

func TestSkipList(t *testing.T) {
	sl := New(4)
	require.Nil(t, sl.First())
	require.Nil(t, sl.FindGTENode([]byte{0, 0, 0, 0}))
	require.Nil(t, sl.FindGTENode([]byte{1, 2, 3, 4}))

	for _, v := range [][]byte{
		{0, 0, 0, 0},
		{1, 2, 3, 4},
		{5, 6, 7, 9},
		{100, 200, 120, 1},
		{50, 10, 1, 9},
		{11, 33, 54, 22},
		{8, 3, 9, 5},
	} {
		t.Logf("add: %v", v)
		sl.Add(v)
		// dumpSkipList(t, sl)
		checkSanity(t, sl)
	}
	checkSanity(t, sl)
	require.Equal(t, [][]byte{
		{0, 0, 0, 0},
		{1, 2, 3, 4},
		{5, 6, 7, 9},
		{8, 3, 9, 5},
		{11, 33, 54, 22},
		{50, 10, 1, 9},
		{100, 200, 120, 1},
	}, enumerate(sl))
	require.Equal(t, sl.First(), sl.FindGTENode([]byte{0, 0, 0, 0}))
	require.Equal(t, []byte{1, 2, 3, 4}, sl.FindGTENode([]byte{1, 2, 3, 4}).Key())
	require.Equal(t, []byte{1, 2, 3, 4}, sl.FindGTENode([]byte{1, 2, 3, 0}).Key())
	require.Equal(t, []byte{50, 10, 1, 9}, sl.FindGTENode([]byte{50, 10, 1, 9}).Key())
	require.Equal(t, []byte{50, 10, 1, 9}, sl.FindGTENode([]byte{50, 0, 0, 0}).Key())
	require.Equal(t, []byte{100, 200, 120, 1}, sl.FindGTENode([]byte{100, 200, 120, 1}).Key())
	require.Equal(t, []byte{100, 200, 120, 1}, sl.FindGTENode([]byte{99, 0, 0, 1}).Key())
	require.Nil(t, sl.FindGTENode([]byte{101, 0, 0, 0}))

	for _, v := range [][]byte{
		{5, 5, 5, 5},
		{100, 200, 120, 1},
		{7, 8, 9, 10},
		{11, 12, 13, 15},
	} {
		t.Logf("add: %v", v)
		sl.Add(v)
		// dumpSkipList(t, sl)
		checkSanity(t, sl)
	}
	checkSanity(t, sl)
	require.Equal(t, [][]byte{
		{0, 0, 0, 0},
		{1, 2, 3, 4},
		{5, 5, 5, 5},
		{5, 6, 7, 9},
		{7, 8, 9, 10},
		{8, 3, 9, 5},
		{11, 12, 13, 15},
		{11, 33, 54, 22},
		{50, 10, 1, 9},
		{100, 200, 120, 1},
	}, enumerate(sl))
	require.Equal(t, []byte{11, 12, 13, 15}, sl.FindGTENode([]byte{11, 12, 13, 15}).Key())
	require.Equal(t, []byte{11, 12, 13, 15}, sl.FindGTENode([]byte{11, 10, 1, 1}).Key())
}

func TestRandomSkipList(t *testing.T) {
	for i := 0; i < 100; i++ {
		sl := New(4)
		n := rand.IntN(10000) + 1
		expect := make([][]byte, n)
		generated := make(map[[4]byte]struct{})
		for j := range expect {
			var b [4]byte
			for {
				_, err := crand.Read(b[:])
				require.NoError(t, err)
				if _, ok := generated[b]; !ok {
					generated[b] = struct{}{}
					break
				}
			}
			expect[j] = slices.Clone(b[:])
			sl.Add(b[:])
		}
		checkSanity(t, sl)
		slices.SortFunc(expect, func(a, b []byte) int { return bytes.Compare(a, b) })
		require.Equal(t, expect, enumerate(sl))
		for i := 0; i < min(10, n); i++ {
			key := expect[rand.IntN(len(expect))]
			node := sl.FindGTENode(key)
			require.NotNil(t, node)
			require.Equal(t, key, node.Key())
		}
	}
}

// TBD: benchmark
