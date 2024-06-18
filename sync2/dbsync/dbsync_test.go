package dbsync

import (
	"bytes"
	"math/bits"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/exp/maps"
	// "golang.org/x/exp/rand"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func TestPrefix(t *testing.T) {
	for _, tc := range []struct {
		p             prefix
		s             string
		bits          uint64
		len           int
		left          prefix
		right         prefix
		gotCacheIndex bool
		cacheIndex    cacheIndex
	}{
		{
			p:             0,
			s:             "<0>",
			len:           0,
			bits:          0,
			left:          0b0_000001,
			right:         0b1_000001,
			gotCacheIndex: true,
			cacheIndex:    0,
		},
		{
			p:             0b0_000001,
			s:             "<1:0>",
			len:           1,
			bits:          0,
			left:          0b00_000010,
			right:         0b01_000010,
			gotCacheIndex: true,
			cacheIndex:    1,
		},
		{
			p:             0b1_000001,
			s:             "<1:1>",
			len:           1,
			bits:          1,
			left:          0b10_000010,
			right:         0b11_000010,
			gotCacheIndex: true,
			cacheIndex:    2,
		},
		{
			p:             0b00_000010,
			s:             "<2:00>",
			len:           2,
			bits:          0,
			left:          0b000_000011,
			right:         0b001_000011,
			gotCacheIndex: true,
			cacheIndex:    3,
		},
		{
			p:             0b01_000010,
			s:             "<2:01>",
			len:           2,
			bits:          1,
			left:          0b010_000011,
			right:         0b011_000011,
			gotCacheIndex: true,
			cacheIndex:    4,
		},
		{
			p:             0b10_000010,
			s:             "<2:10>",
			len:           2,
			bits:          2,
			left:          0b100_000011,
			right:         0b101_000011,
			gotCacheIndex: true,
			cacheIndex:    5,
		},
		{
			p:             0b11_000010,
			s:             "<2:11>",
			len:           2,
			bits:          3,
			left:          0b110_000011,
			right:         0b111_000011,
			gotCacheIndex: true,
			cacheIndex:    6,
		},
		{
			p:             0x3fffffd8,
			s:             "<24:111111111111111111111111>",
			len:           24,
			bits:          0xffffff,
			left:          0x7fffff99,
			right:         0x7fffffd9,
			gotCacheIndex: true,
			cacheIndex:    0x1fffffe,
		},
		{
			p:             0x7fffff99,
			s:             "<25:1111111111111111111111110>",
			len:           25,
			bits:          0x1fffffe,
			left:          0xffffff1a,
			right:         0xffffff5a,
			gotCacheIndex: false, // len > 24
		},
	} {
		require.Equal(t, tc.s, tc.p.String())
		require.Equal(t, tc.bits, tc.p.bits())
		require.Equal(t, tc.len, tc.p.len())
		require.Equal(t, tc.left, tc.p.left())
		require.Equal(t, tc.right, tc.p.right())
		idx, gotIdx := tc.p.cacheIndex()
		require.Equal(t, tc.gotCacheIndex, gotIdx)
		if gotIdx {
			require.Equal(t, tc.cacheIndex, idx)
		}
	}
}

func TestHashPrefix(t *testing.T) {
	for _, tc := range []struct {
		h         string
		l         int
		p         prefix
		preFirst0 prefix
		preFirst1 prefix
	}{
		{
			h:         "ABCDEF1234567890000000000000000000000000000000000000000000000000",
			l:         0,
			p:         0,
			preFirst0: 0b1_000001,
			preFirst1: 0,
		},
		{
			h:         "ABCDEF1234567890000000000000000000000000000000000000000000000000",
			l:         1,
			p:         0b1_000001,
			preFirst0: 0b1_000001,
			preFirst1: 0,
		},
		{
			h:         "2BCDEF1234567890000000000000000000000000000000000000000000000000",
			l:         1,
			p:         0b0_000001,
			preFirst0: 0,
			preFirst1: 0b00_000010,
		},
		{
			h:         "ABCDEF1234567890000000000000000000000000000000000000000000000000",
			l:         4,
			p:         0b1010_000100,
			preFirst0: 0b1_000001,
			preFirst1: 0,
		},
		{
			h:         "ABCDEF1234567890000000000000000000000000000000000000000000000000",
			l:         57,
			p:         0x55e6f7891a2b3c79,
			preFirst0: 0b1_000001,
			preFirst1: 0,
		},
		{
			h:         "ABCDEF1234567890000000000000000000000000000000000000000000000000",
			l:         58,
			p:         0xabcdef12345678ba,
			preFirst0: 0b1_000001,
			preFirst1: 0,
		},
		{
			h:         "0000000000000000000000000000000000000000000000000000000000000000",
			l:         0,
			p:         0,
			preFirst0: 0,
			preFirst1: 58,
		},
		{
			h:         "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF",
			l:         0,
			p:         0,
			preFirst0: 0xfffffffffffffffa,
			preFirst1: 0,
		},
	} {
		h := types.HexToHash32(tc.h)
		require.Equal(t, tc.p, hashPrefix(h[:], tc.l), "hash prefix: h %s l %d", tc.h, tc.l)
		require.Equal(t, tc.preFirst0, preFirst0(h[:]), "preFirst0: h %s", tc.h)
		require.Equal(t, tc.preFirst1, preFirst1(h[:]), "preFirst1: h %s", tc.h)
	}
}

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
	var mft inMemFPTree
	var hs []types.Hash32
	for _, hex := range []string{
		"0000000000000000000000000000000000000000000000000000000000000000",
		"123456789ABCDEF0000000000000000000000000000000000000000000000000",
		"5555555555555555555555555555555555555555555555555555555555555555",
		"8888888888888888888888888888888888888888888888888888888888888888",
		"ABCDEF1234567890000000000000000000000000000000000000000000000000",
	} {
		t.Logf("QQQQQ: ADD: %s", hex)
		h := types.HexToHash32(hex)
		hs = append(hs, h)
		mft.addHash(h[:])
	}
	var sb strings.Builder
	mft.tree.dump(&sb)
	t.Logf("QQQQQ: tree:\n%s", sb.String())
	require.Equal(t, hexToFingerprint("642464b773377bbddddddddd"), mft.tree.nodes[0].fp)
	require.Equal(t, fpResult{
		fp:    hexToFingerprint("642464b773377bbddddddddd"),
		count: 5,
	}, mft.aggregateInterval(hs[0][:], hs[0][:]))
	require.Equal(t, fpResult{
		fp:    hexToFingerprint("642464b773377bbddddddddd"),
		count: 5,
	}, mft.aggregateInterval(hs[4][:], hs[4][:]))
	require.Equal(t, fpResult{
		fp:    hexToFingerprint("000000000000000000000000"),
		count: 1,
	}, mft.aggregateInterval(hs[0][:], hs[1][:]))
	require.Equal(t, fpResult{
		fp:    hexToFingerprint("cfe98ba54761032ddddddddd"),
		count: 3,
	}, mft.aggregateInterval(hs[1][:], hs[4][:]))
	// TBD: test reverse range
}

func TestInMemFPTreeRmme1(t *testing.T) {
	var mft inMemFPTree
	var hs []types.Hash32
	for _, hex := range []string{
		"829977b444c8408dcddc1210536f3b3bdc7fd97777426264b9ac8f70b97a7fd1",
		"6e476ca729c3840d0118785496e488124ee7dade1aef0c87c6edc78f72e4904f",
		"a280bcb8123393e0d4a15e5c9850aab5dddffa03d5efa92e59bc96202e8992bc",
		"e93163f908630280c2a8bffd9930aa684be7a3085432035f5c641b0786590d1d",
	} {
		t.Logf("QQQQQ: ADD: %s", hex)
		h := types.HexToHash32(hex)
		hs = append(hs, h)
		mft.addHash(h[:])
	}
	var sb strings.Builder
	mft.tree.dump(&sb)
	t.Logf("QQQQQ: tree:\n%s", sb.String())
	require.Equal(t, hexToFingerprint("a76fc452775b55e0dacd8be5"), mft.tree.nodes[0].fp)
	require.Equal(t, fpResult{
		fp:    hexToFingerprint("2019cb0c56fbd36d197d4c4c"),
		count: 2,
	}, mft.aggregateInterval(hs[0][:], hs[3][:]))
}

type hashList []types.Hash32

func (l hashList) findGTE(h types.Hash32) int {
	p, _ := slices.BinarySearchFunc(l, h, func(a, b types.Hash32) int {
		return a.Compare(b)
	})
	return p
}

func TestInMemFPTreeManyItems(t *testing.T) {
	var mft inMemFPTree
	const numItems = 1 << 20
	hs := make(hashList, numItems)
	var fp fingerprint
	for i := range hs {
		h := types.RandomHash()
		hs[i] = h
		mft.addHash(h[:])
		fp.update(h[:])
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
	require.Equal(t, fp, mft.tree.nodes[0].fp)
	for i := 0; i < 100; i++ {
		// TBD: allow reverse order
		// TBD: pick some intervals from the hashes
		x := types.RandomHash()
		y := types.RandomHash()
		// x := hs[rand.Intn(numItems)]
		// y := hs[rand.Intn(numItems)]
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
}
