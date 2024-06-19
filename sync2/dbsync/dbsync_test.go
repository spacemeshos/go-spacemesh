package dbsync

import (
	"bytes"
	"math/bits"
	"math/rand"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/exp/maps"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func TestNodePool(t *testing.T) {
	// TODO: convert to TestRCPool
	var np nodePool
	idx1 := np.add(fingerprint{1, 2, 3}, 1, noIndex, noIndex)
	node1 := np.node(idx1)
	idx2 := np.add(fingerprint{2, 3, 4}, 2, idx1, noIndex)
	node2 := np.node(idx2)
	require.Equal(t, fingerprint{1, 2, 3}, node1.fp)
	require.Equal(t, uint32(1), node1.c)
	require.Equal(t, noIndex, node1.left)
	require.Equal(t, noIndex, node1.right)
	require.Equal(t, fingerprint{2, 3, 4}, node2.fp)
	require.Equal(t, uint32(2), node2.c)
	require.Equal(t, idx1, node2.left)
	require.Equal(t, noIndex, node2.right)
	idx3 := np.add(fingerprint{2, 3, 5}, 1, noIndex, noIndex)
	idx4 := np.add(fingerprint{2, 3, 6}, 1, idx2, idx3)
	require.Equal(t, nodeIndex(3), idx4)
	np.ref(idx4)

	np.release(idx4)
	// not yet released due to an extra ref
	require.Equal(t, nodeIndex(4), np.add(fingerprint{2, 3, 7}, 1, noIndex, noIndex))

	np.release(idx4)
	// idx4 was freed
	require.Equal(t, idx4, np.add(fingerprint{2, 3, 8}, 1, noIndex, noIndex))

	// free item used just once
	require.Equal(t, nodeIndex(5), np.add(fingerprint{2, 3, 9}, 1, noIndex, noIndex))

	// form a free list
	np.release(idx3)
	np.release(idx2)
	np.release(idx1)

	// the free list is LIFO
	require.Equal(t, idx1, np.add(fingerprint{2, 3, 10}, 1, noIndex, noIndex))
	require.Equal(t, idx2, np.add(fingerprint{2, 3, 11}, 1, noIndex, noIndex))
	require.Equal(t, idx3, np.add(fingerprint{2, 3, 12}, 1, noIndex, noIndex))

	// the free list is exhausted
	require.Equal(t, nodeIndex(6), np.add(fingerprint{2, 3, 13}, 1, noIndex, noIndex))
}

func TestPrefix(t *testing.T) {
	for _, tc := range []struct {
		p     prefix
		s     string
		bits  uint64
		len   int
		left  prefix
		right prefix
		shift prefix
	}{
		{
			p:     0,
			s:     "<0>",
			len:   0,
			bits:  0,
			left:  0b0_000001,
			right: 0b1_000001,
		},
		{
			p:     0b0_000001,
			s:     "<1:0>",
			len:   1,
			bits:  0,
			left:  0b00_000010,
			right: 0b01_000010,
			shift: 0,
		},
		{
			p:     0b1_000001,
			s:     "<1:1>",
			len:   1,
			bits:  1,
			left:  0b10_000010,
			right: 0b11_000010,
			shift: 0,
		},
		{
			p:     0b00_000010,
			s:     "<2:00>",
			len:   2,
			bits:  0,
			left:  0b000_000011,
			right: 0b001_000011,
			shift: 0b0_000001,
		},
		{
			p:     0b01_000010,
			s:     "<2:01>",
			len:   2,
			bits:  1,
			left:  0b010_000011,
			right: 0b011_000011,
			shift: 0b1_000001,
		},
		{
			p:     0b10_000010,
			s:     "<2:10>",
			len:   2,
			bits:  2,
			left:  0b100_000011,
			right: 0b101_000011,
			shift: 0b0_000001,
		},
		{
			p:     0b11_000010,
			s:     "<2:11>",
			len:   2,
			bits:  3,
			left:  0b110_000011,
			right: 0b111_000011,
			shift: 0b1_000001,
		},
		{
			p:     0x3fffffd8,
			s:     "<24:111111111111111111111111>",
			len:   24,
			bits:  0xffffff,
			left:  0x7fffff99,
			right: 0x7fffffd9,
			shift: 0x1fffffd7,
		},
		{
			p:     0x7fffff99,
			s:     "<25:1111111111111111111111110>",
			len:   25,
			bits:  0x1fffffe,
			left:  0xffffff1a,
			right: 0xffffff5a,
			shift: 0x3fffff98,
		},
	} {
		require.Equal(t, tc.s, tc.p.String())
		require.Equal(t, tc.bits, tc.p.bits())
		require.Equal(t, tc.len, tc.p.len())
		require.Equal(t, tc.left, tc.p.left())
		require.Equal(t, tc.right, tc.p.right())
		if tc.p != 0 {
			require.Equal(t, tc.shift, tc.p.shift())
		}
	}
}

// func TestHashPrefix(t *testing.T) {
// 	for _, tc := range []struct {
// 		h         string
// 		l         int
// 		p         prefix
// 		preFirst0 prefix
// 		preFirst1 prefix
// 	}{
// 		{
// 			h:         "ABCDEF1234567890000000000000000000000000000000000000000000000000",
// 			l:         0,
// 			p:         0,
// 			preFirst0: 0b1_000001,
// 			preFirst1: 0,
// 		},
// 		{
// 			h:         "ABCDEF1234567890000000000000000000000000000000000000000000000000",
// 			l:         1,
// 			p:         0b1_000001,
// 			preFirst0: 0b1_000001,
// 			preFirst1: 0,
// 		},
// 		{
// 			h:         "2BCDEF1234567890000000000000000000000000000000000000000000000000",
// 			l:         1,
// 			p:         0b0_000001,
// 			preFirst0: 0,
// 			preFirst1: 0b00_000010,
// 		},
// 		{
// 			h:         "ABCDEF1234567890000000000000000000000000000000000000000000000000",
// 			l:         4,
// 			p:         0b1010_000100,
// 			preFirst0: 0b1_000001,
// 			preFirst1: 0,
// 		},
// 		{
// 			h:         "ABCDEF1234567890000000000000000000000000000000000000000000000000",
// 			l:         57,
// 			p:         0x55e6f7891a2b3c79,
// 			preFirst0: 0b1_000001,
// 			preFirst1: 0,
// 		},
// 		{
// 			h:         "ABCDEF1234567890000000000000000000000000000000000000000000000000",
// 			l:         58,
// 			p:         0xabcdef12345678ba,
// 			preFirst0: 0b1_000001,
// 			preFirst1: 0,
// 		},
// 		{
// 			h:         "0000000000000000000000000000000000000000000000000000000000000000",
// 			l:         0,
// 			p:         0,
// 			preFirst0: 0,
// 			preFirst1: 58,
// 		},
// 		{
// 			h:         "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF",
// 			l:         0,
// 			p:         0,
// 			preFirst0: 0xfffffffffffffffa,
// 			preFirst1: 0,
// 		},
// 	} {
// 		h := types.HexToHash32(tc.h)
// 		require.Equal(t, tc.p, hashPrefix(h[:], tc.l), "hash prefix: h %s l %d", tc.h, tc.l)
// 		require.Equal(t, tc.preFirst0, preFirst0(h[:]), "preFirst0: h %s", tc.h)
// 		require.Equal(t, tc.preFirst1, preFirst1(h[:]), "preFirst1: h %s", tc.h)
// 	}
// }

func TestCommonPrefix(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		p    prefix
	}{
		{
			a: "0000000000000000000000000000000000000000000000000000000000000000",
			b: "8000000000000000000000000000000000000000000000000000000000000000",
			p: 0,
		},
		{
			a: "A000000000000000000000000000000000000000000000000000000000000000",
			b: "8000000000000000000000000000000000000000000000000000000000000000",
			p: 0b10_000010,
		},
		{
			a: "A000000000000000000000000000000000000000000000000000000000000000",
			b: "A800000000000000000000000000000000000000000000000000000000000000",
			p: 0b1010_000100,
		},
		{
			a: "ABCDEF1234567890000000000000000000000000000000000000000000000000",
			b: "ABCDEF1234567800000000000000000000000000000000000000000000000000",
			p: 0x2af37bc48d159e38,
		},
		{
			a: "ABCDEF1234567890123456789ABCDEF000000000000000000000000000000000",
			b: "ABCDEF1234567890123456789ABCDEF000000000000000000000000000000000",
			p: 0xabcdef12345678ba,
		},
	} {
		a := types.HexToHash32(tc.a)
		b := types.HexToHash32(tc.b)
		require.Equal(t, tc.p, commonPrefix(a[:], b[:]))
	}
}

const dbFile = "/Users/ivan4th/Library/Application Support/Spacemesh/node-data/7c8cef2b/state.sql"

func TestRmme(t *testing.T) {
	t.Skip("slow tmp test")
	counts := make(map[uint64]uint64)
	prefLens := make(map[int]int)
	db, err := statesql.Open("file:" + dbFile)
	require.NoError(t, err)
	defer db.Close()
	var prev uint64
	first := true
	// where epoch=23
	_, err = db.Exec("select id from atxs order by id", nil, func(stmt *sql.Statement) bool {
		var id types.Hash32
		stmt.ColumnBytes(0, id[:])
		v := load64(id[:])
		counts[v>>40]++
		if first {
			first = false
		} else {
			prefLens[bits.LeadingZeros64(prev^v)]++
		}
		prev = v
		return true
	})
	require.NoError(t, err)
	countFreq := make(map[uint64]int)
	for _, c := range counts {
		countFreq[c]++
	}
	ks := maps.Keys(countFreq)
	slices.Sort(ks)
	for _, c := range ks {
		t.Logf("%d: %d times", c, countFreq[c])
	}
	pls := maps.Keys(prefLens)
	slices.Sort(pls)
	for _, pl := range pls {
		t.Logf("pl %d: %d times", pl, prefLens[pl])
	}
}

func TestInMemFPTree(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ids     []string
		results map[[2]int]fpResult
	}{
		{
			name: "ids1",
			ids: []string{
				"0000000000000000000000000000000000000000000000000000000000000000",
				"123456789ABCDEF0000000000000000000000000000000000000000000000000",
				"5555555555555555555555555555555555555555555555555555555555555555",
				"8888888888888888888888888888888888888888888888888888888888888888",
				"ABCDEF1234567890000000000000000000000000000000000000000000000000",
			},
			results: map[[2]int]fpResult{
				{0, 0}: {
					fp:    hexToFingerprint("642464b773377bbddddddddd"),
					count: 5,
				},
				{4, 4}: {
					fp:    hexToFingerprint("642464b773377bbddddddddd"),
					count: 5,
				},
				{0, 1}: {
					fp:    hexToFingerprint("000000000000000000000000"),
					count: 1,
				},
				{1, 4}: {
					fp:    hexToFingerprint("cfe98ba54761032ddddddddd"),
					count: 3,
				},
			},
		},
		{
			name: "ids2",
			ids: []string{
				"829977b444c8408dcddc1210536f3b3bdc7fd97777426264b9ac8f70b97a7fd1",
				"6e476ca729c3840d0118785496e488124ee7dade1aef0c87c6edc78f72e4904f",
				"a280bcb8123393e0d4a15e5c9850aab5dddffa03d5efa92e59bc96202e8992bc",
				"e93163f908630280c2a8bffd9930aa684be7a3085432035f5c641b0786590d1d",
			},
			results: map[[2]int]fpResult{
				{0, 0}: {
					fp:    hexToFingerprint("a76fc452775b55e0dacd8be5"),
					count: 4,
				},
				{0, 3}: {
					fp:    hexToFingerprint("2019cb0c56fbd36d197d4c4c"),
					count: 2,
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var np nodePool
			mft := newInMemFPTree(&np, 24)
			var hs []types.Hash32
			for _, hex := range tc.ids {
				t.Logf("QQQQQ: ADD: %s", hex)
				h := types.HexToHash32(hex)
				hs = append(hs, h)
				mft.addHash(h[:])
			}

			var sb strings.Builder
			mft.tree.dump(&sb)
			t.Logf("tree:\n%s", sb.String())

			checkTree(t, mft.tree, 24)

			for idRange, fpResult := range tc.results {
				x := hs[idRange[0]]
				y := hs[idRange[1]]
				require.Equal(t, fpResult, mft.aggregateInterval(x[:], y[:]))
			}
		})
	}
}

type hashList []types.Hash32

func (l hashList) findGTE(h types.Hash32) int {
	p, _ := slices.BinarySearchFunc(l, h, func(a, b types.Hash32) int {
		return a.Compare(b)
	})
	return p
}

func checkNode(t *testing.T, ft *fpTree, idx nodeIndex, depth int) {
	node := ft.np.node(idx)
	if node.left == noIndex && node.right == noIndex {
		if node.c != 1 {
			require.Equal(t, depth, ft.maxDepth)
		}
	} else {
		require.Less(t, depth, ft.maxDepth)
		var expFP fingerprint
		var expCount uint32
		if node.left != noIndex {
			checkNode(t, ft, node.left, depth+1)
			left := ft.np.node(node.left)
			expFP.update(left.fp[:])
			expCount += left.c
		}
		if node.right != noIndex {
			checkNode(t, ft, node.right, depth+1)
			right := ft.np.node(node.right)
			expFP.update(right.fp[:])
			expCount += right.c
		}
		require.Equal(t, expFP, node.fp, "node fp at depth %d", depth)
		require.Equal(t, expCount, node.c, "node count at depth %d", depth)
	}
}

func checkTree(t *testing.T, ft *fpTree, maxDepth int) {
	require.Equal(t, maxDepth, ft.maxDepth)
	checkNode(t, ft, ft.root, 0)

}

func testInMemFPTreeManyItems(t *testing.T, randomXY bool) {
	var np nodePool
	const (
		numItems = 1 << 16
		maxDepth = 24
	)
	mft := newInMemFPTree(&np, maxDepth)
	hs := make(hashList, numItems)
	var fp fingerprint
	rmmeMap := make(map[types.Hash32]bool)
	for i := range hs {
		h := types.RandomHash()
		hs[i] = h
		mft.addHash(h[:])
		fp.update(h[:])
		require.False(t, rmmeMap[h])
		rmmeMap[h] = true
	}
	// var sb strings.Builder
	// mft.tree.dump(&sb)
	// t.Logf("QQQQQ: tree:\n%s", sb.String())
	slices.SortFunc(hs, func(a, b types.Hash32) int {
		return a.Compare(b)
	})
	// for i, h := range hs {
	// 	t.Logf("h[%d] = %s", i, h.String())
	// }

	total := 0
	nums := make(map[int]int)
	for _, ids := range mft.ids {
		nums[len(ids)]++
		total += len(ids)
	}
	t.Logf("total %d, numItems %d, nums %#v", total, numItems, nums)

	checkTree(t, mft.tree, maxDepth)

	require.Equal(t, fpResult{fp: fp, count: numItems}, mft.aggregateInterval(hs[0][:], hs[0][:]))
	for i := 0; i < 100; i++ {
		// TBD: allow reverse order
		var x, y types.Hash32
		if randomXY {
			x = types.RandomHash()
			y = types.RandomHash()
		} else {
			x = hs[rand.Intn(numItems)]
			y = hs[rand.Intn(numItems)]
		}
		c := bytes.Compare(x[:], y[:])
		var (
			expFP fingerprint
			expN  uint32
		)
		if c > 0 {
			x, y = y, x
		}
		if c == 0 {
			expFP = fp
			expN = numItems
		} else {
			pX := hs.findGTE(x)
			pY := hs.findGTE(y)
			// t.Logf("x=%s y=%s pX=%d y=%d", x.String(), y.String(), pX, pY)
			for p := pX; p < pY; p++ {
				// t.Logf("XOR %s", hs[p].String())
				expFP.update(hs[p][:])
			}
			expN = uint32(pY - pX)
		}
		require.Equal(t, fpResult{
			fp:    expFP,
			count: expN,
		}, mft.aggregateInterval(x[:], y[:]))
	}
	// TODO: test inverse intervals
}

func TestInMemFPTreeManyItems(t *testing.T) {
	t.Run("bounds from the set", func(t *testing.T) {
		testInMemFPTreeManyItems(t, false)
	})
	t.Run("random bounds", func(t *testing.T) {
		testInMemFPTreeManyItems(t, true)
	})
}
