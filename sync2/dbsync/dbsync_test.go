package dbsync

import (
	"bytes"
	"fmt"
	"math/rand"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
)

func TestPrefix(t *testing.T) {
	for _, tc := range []struct {
		p     prefix
		s     string
		bits  uint64
		len   int
		left  prefix
		right prefix
		shift prefix
		minID string
		maxID string
	}{
		{
			p:     0,
			s:     "<0>",
			len:   0,
			bits:  0,
			left:  0b0_000001,
			right: 0b1_000001,
			minID: "0000000000000000000000000000000000000000000000000000000000000000",
			maxID: "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF",
		},
		{
			p:     0b0_000001,
			s:     "<1:0>",
			len:   1,
			bits:  0,
			left:  0b00_000010,
			right: 0b01_000010,
			shift: 0,
			minID: "0000000000000000000000000000000000000000000000000000000000000000",
			maxID: "7FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF",
		},
		{
			p:     0b1_000001,
			s:     "<1:1>",
			len:   1,
			bits:  1,
			left:  0b10_000010,
			right: 0b11_000010,
			shift: 0,
			minID: "8000000000000000000000000000000000000000000000000000000000000000",
			maxID: "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF",
		},
		{
			p:     0b00_000010,
			s:     "<2:00>",
			len:   2,
			bits:  0,
			left:  0b000_000011,
			right: 0b001_000011,
			shift: 0b0_000001,
			minID: "0000000000000000000000000000000000000000000000000000000000000000",
			maxID: "3FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF",
		},
		{
			p:     0b01_000010,
			s:     "<2:01>",
			len:   2,
			bits:  1,
			left:  0b010_000011,
			right: 0b011_000011,
			shift: 0b1_000001,
			minID: "4000000000000000000000000000000000000000000000000000000000000000",
			maxID: "7FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF",
		},
		{
			p:     0b10_000010,
			s:     "<2:10>",
			len:   2,
			bits:  2,
			left:  0b100_000011,
			right: 0b101_000011,
			shift: 0b0_000001,
			minID: "8000000000000000000000000000000000000000000000000000000000000000",
			maxID: "BFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF",
		},
		{
			p:     0b11_000010,
			s:     "<2:11>",
			len:   2,
			bits:  3,
			left:  0b110_000011,
			right: 0b111_000011,
			shift: 0b1_000001,
			minID: "C000000000000000000000000000000000000000000000000000000000000000",
			maxID: "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF",
		},
		{
			p:     0x3fffffd8,
			s:     "<24:111111111111111111111111>",
			len:   24,
			bits:  0xffffff,
			left:  0x7fffff99,
			right: 0x7fffffd9,
			shift: 0x1fffffd7,
			minID: "FFFFFF0000000000000000000000000000000000000000000000000000000000",
			maxID: "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF",
		},
		{
			p:     0x7fffff99,
			s:     "<25:1111111111111111111111110>",
			len:   25,
			bits:  0x1fffffe,
			left:  0xffffff1a,
			right: 0xffffff5a,
			shift: 0x3fffff98,
			minID: "FFFFFF0000000000000000000000000000000000000000000000000000000000",
			maxID: "FFFFFF7FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF",
		},
	} {
		t.Run(fmt.Sprint(tc.p), func(t *testing.T) {
			require.Equal(t, tc.s, tc.p.String())
			require.Equal(t, tc.bits, tc.p.bits())
			require.Equal(t, tc.len, tc.p.len())
			require.Equal(t, tc.left, tc.p.left())
			require.Equal(t, tc.right, tc.p.right())
			if tc.p != 0 {
				require.Equal(t, tc.shift, tc.p.shift())
			}

			expMinID := types.HexToHash32(tc.minID)
			var minID types.Hash32
			tc.p.minID(minID[:])
			require.Equal(t, expMinID, minID)

			expMaxID := types.HexToHash32(tc.maxID)
			var maxID types.Hash32
			tc.p.maxID(maxID[:])
			require.Equal(t, expMaxID, maxID)
		})
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

type fakeATXStore struct {
	db sql.StateDatabase
	*sqlIDStore
}

func newFakeATXIDStore(db sql.StateDatabase) *fakeATXStore {
	return &fakeATXStore{db: db, sqlIDStore: newSQLIDStore(db)}
}

func (s *fakeATXStore) registerHash(h []byte, maxDepth int) error {
	if err := s.sqlIDStore.registerHash(h, maxDepth); err != nil {
		return err
	}
	_, err := s.db.Exec(`
		insert into atxs (id, epoch, effective_num_units, received)
                values (?, 1, 1, 0)`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, h)
		}, nil)
	return err
}

func testFPTree(t *testing.T, idStore idStore) {
	for _, tc := range []struct {
		name    string
		ids     []string
		results map[[2]int]fpResult
	}{
		{
			name: "ids1",
			ids: []string{
				"0000000000000000000000000000000000000000000000000000000000000000",
				"123456789abcdef0000000000000000000000000000000000000000000000000",
				"5555555555555555555555555555555555555555555555555555555555555555",
				"8888888888888888888888888888888888888888888888888888888888888888",
				"abcdef1234567890000000000000000000000000000000000000000000000000",
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
				{1, 0}: {
					fp:    hexToFingerprint("642464b773377bbddddddddd"),
					count: 4,
				},
				{2, 0}: {
					fp:    hexToFingerprint("761032cfe98ba54ddddddddd"),
					count: 3,
				},
				{3, 1}: {
					fp:    hexToFingerprint("2345679abcdef01888888888"),
					count: 3,
				},
				{3, 2}: {
					fp:    hexToFingerprint("317131e226622ee888888888"),
					count: 4,
				},
			},
		},
		{
			name: "ids2",
			ids: []string{
				"6e476ca729c3840d0118785496e488124ee7dade1aef0c87c6edc78f72e4904f",
				"829977b444c8408dcddc1210536f3b3bdc7fd97777426264b9ac8f70b97a7fd1",
				"a280bcb8123393e0d4a15e5c9850aab5dddffa03d5efa92e59bc96202e8992bc",
				"e93163f908630280c2a8bffd9930aa684be7a3085432035f5c641b0786590d1d",
			},
			results: map[[2]int]fpResult{
				{0, 0}: {
					fp:    hexToFingerprint("a76fc452775b55e0dacd8be5"),
					count: 4,
				},
				{0, 3}: {
					fp:    hexToFingerprint("4e5ea7ab7f38576018653418"),
					count: 3,
				},
				{3, 1}: {
					fp:    hexToFingerprint("87760f5e21a0868dc3b0c7a9"),
					count: 2,
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var np nodePool
			ft := newFPTree(&np, idStore, 24)
			var hs []types.Hash32
			for _, hex := range tc.ids {
				t.Logf("add: %s", hex)
				h := types.HexToHash32(hex)
				hs = append(hs, h)
				ft.addHash(h[:])
			}

			var sb strings.Builder
			ft.dump(&sb)
			t.Logf("tree:\n%s", sb.String())

			checkTree(t, ft, 24)

			for idRange, expResult := range tc.results {
				x := hs[idRange[0]]
				y := hs[idRange[1]]
				fpr, err := ft.fingerprintInterval(x[:], y[:])
				require.NoError(t, err)
				require.Equal(t, expResult, fpr)
			}
		})
	}
}

func TestFPTree(t *testing.T) {
	t.Run("in-memory id store", func(t *testing.T) {
		testFPTree(t, &memIDStore{})
	})
	t.Run("fake ATX store", func(t *testing.T) {
		db := statesql.InMemory()
		defer db.Close()
		testFPTree(t, newFakeATXIDStore(db))
	})
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

func testInMemFPTreeManyItems(t *testing.T, idStore idStore, randomXY bool) {
	var np nodePool
	const (
		numItems = 1 << 16
		maxDepth = 24
	)
	ft := newFPTree(&np, idStore, maxDepth)
	hs := make(hashList, numItems)
	var fp fingerprint
	for i := range hs {
		h := types.RandomHash()
		hs[i] = h
		ft.addHash(h[:])
		fp.update(h[:])
	}
	slices.SortFunc(hs, func(a, b types.Hash32) int {
		return a.Compare(b)
	})

	checkTree(t, ft, maxDepth)

	fpr, err := ft.fingerprintInterval(hs[0][:], hs[0][:])
	require.NoError(t, err)
	require.Equal(t, fpResult{fp: fp, count: numItems}, fpr)
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
		var (
			expFP fingerprint
			expN  uint32
		)
		switch bytes.Compare(x[:], y[:]) {
		case -1:
			pX := hs.findGTE(x)
			pY := hs.findGTE(y)
			// t.Logf("x=%s y=%s pX=%d y=%d", x.String(), y.String(), pX, pY)
			for p := pX; p < pY; p++ {
				// t.Logf("XOR %s", hs[p].String())
				expFP.update(hs[p][:])
			}
			expN = uint32(pY - pX)
		case 1:
			pX := hs.findGTE(x)
			pY := hs.findGTE(y)
			for p := 0; p < pY; p++ {
				expFP.update(hs[p][:])
			}
			for p := pX; p < len(hs); p++ {
				expFP.update(hs[p][:])
			}
			expN = uint32(pY + len(hs) - pX)
		default:
			expFP = fp
			expN = numItems
		}
		fpr, err := ft.fingerprintInterval(x[:], y[:])
		require.NoError(t, err)
		require.Equal(t, fpResult{
			fp:    expFP,
			count: expN,
		}, fpr)
	}
}

func TestInMemFPTreeManyItems(t *testing.T) {
	t.Run("bounds from the set", func(t *testing.T) {
		var idStore memIDStore
		testInMemFPTreeManyItems(t, &idStore, false)
		total := 0
		nums := make(map[int]int)
		for _, ids := range idStore.ids {
			nums[len(ids)]++
			total += len(ids)
		}
		t.Logf("total %d, nums %#v", total, nums)

	})
	t.Run("random bounds", func(t *testing.T) {
		testInMemFPTreeManyItems(t, &memIDStore{}, true)
	})
	t.Run("SQL, bounds from the set", func(t *testing.T) {
		db := statesql.InMemory()
		defer db.Close()
		testInMemFPTreeManyItems(t, newFakeATXIDStore(db), false)

	})
	t.Run("SQL, random bounds", func(t *testing.T) {
		db := statesql.InMemory()
		defer db.Close()
		testInMemFPTreeManyItems(t, newFakeATXIDStore(db), true)
	})
}

const dbFile = "/Users/ivan4th/Library/Application Support/Spacemesh/node-data/7c8cef2b/state.sql"

func dumbAggATXs(t *testing.T, db sql.StateDatabase, x, y types.Hash32) fpResult {
	var fp fingerprint
	ts := time.Now()
	nRows, err := db.Exec(
		// BETWEEN is faster than >= and <
		"select id from atxs where id between ? and ?",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, x[:])
			stmt.BindBytes(2, y[:])
		},
		func(stmt *sql.Statement) bool {
			var id types.Hash32
			stmt.ColumnBytes(0, id[:])
			if id != y {
				fp.update(id[:])
			}
			return true
		},
	)
	require.NoError(t, err)
	t.Logf("QQQQQ: %v: dumb fp between %s and %s", time.Now().Sub(ts), x.String(), y.String())
	return fpResult{
		fp:    fp,
		count: uint32(nRows),
	}
}

func testFP(t *testing.T, maxDepth int) {
	runtime.GC()
	var stats1 runtime.MemStats
	runtime.ReadMemStats(&stats1)
	// t.Skip("slow tmp test")
	// counts := make(map[uint64]uint64)
	// prefLens := make(map[int]int)
	db, err := statesql.Open("file:" + dbFile)
	require.NoError(t, err)
	defer db.Close()
	// _, err = db.Exec("PRAGMA cache_size = -2000000", nil, nil)
	// require.NoError(t, err)
	// var prev uint64
	// first := true
	// where epoch=23
	store := newSQLIDStore(db)
	var np nodePool
	ft := newFPTree(&np, store, maxDepth)
	t.Logf("loading IDs")
	_, err = db.Exec("select id from atxs order by id", nil, func(stmt *sql.Statement) bool {
		var id types.Hash32
		stmt.ColumnBytes(0, id[:])
		ft.addHash(id[:])
		// v := load64(id[:])
		// counts[v>>40]++
		// if first {
		// 	first = false
		// } else {
		// 	prefLens[bits.LeadingZeros64(prev^v)]++
		// }
		// prev = v
		return true
	})
	require.NoError(t, err)
	// countFreq := make(map[uint64]int)
	// for _, c := range counts {
	// 	countFreq[c]++
	// }
	// ks := maps.Keys(countFreq)
	// slices.Sort(ks)
	// for _, c := range ks {
	// 	t.Logf("%d: %d times", c, countFreq[c])
	// }
	// pls := maps.Keys(prefLens)
	// slices.Sort(pls)
	// for _, pl := range pls {
	// 	t.Logf("pl %d: %d times", pl, prefLens[pl])
	// }

	t.Logf("benchmarking ranges")
	ts := time.Now()
	const numIter = 20000
	for n := 0; n < numIter; n++ {
		x := types.RandomHash()
		y := types.RandomHash()
		ft.fingerprintInterval(x[:], y[:])
	}
	elapsed := time.Now().Sub(ts)

	runtime.GC()
	var stats2 runtime.MemStats
	runtime.ReadMemStats(&stats2)
	t.Logf("range benchmark for maxDepth %d: %v per range, %f ranges/s, heap diff %d",
		// it's important to use ft pointer here so it doesn't get freed
		// before we read the mem stats
		ft.maxDepth,
		elapsed/numIter,
		float64(numIter)/elapsed.Seconds(),
		stats2.HeapInuse-stats1.HeapInuse)

	// TBD: restore !!!!
	// t.Logf("testing ranges")
	// for n := 0; n < 10; n++ {
	// 	x := types.RandomHash()
	// 	y := types.RandomHash()
	// 	// TBD: QQQQQ: dumb rev / full intervals
	// 	if x == y {
	// 		continue
	// 	}
	// 	if x.Compare(y) > 0 {
	// 		x, y = y, x
	// 	}
	// 	expFPResult := dumbAggATXs(t, db, x, y)
	// 	fpr, err := ft.fingerprintInterval(x[:], y[:])
	// 	require.NoError(t, err)
	// 	require.Equal(t, expFPResult, fpr)
	// }
}

func TestFP(t *testing.T) {
	t.Skip("slow test")
	for maxDepth := 15; maxDepth <= 23; maxDepth++ {
		for i := 0; i < 3; i++ {
			testFP(t, maxDepth)
		}
	}
}

// benchmarks

// maxDepth 18: 94.739µs per range, 10555.290991 ranges/s, heap diff 16621568
// maxDepth 18: 95.837µs per range, 10434.316922 ranges/s, heap diff 16564224
// maxDepth 18: 95.312µs per range, 10491.834238 ranges/s, heap diff 16588800
// maxDepth 19: 60.822µs per range, 16441.200726 ranges/s, heap diff 32317440
// maxDepth 19: 57.86µs per range, 17283.084675 ranges/s, heap diff 32333824
// maxDepth 19: 58.183µs per range, 17187.139809 ranges/s, heap diff 32342016
// maxDepth 20: 41.582µs per range, 24048.516680 ranges/s, heap diff 63094784
// maxDepth 20: 41.384µs per range, 24163.830753 ranges/s, heap diff 63102976
// maxDepth 20: 42.003µs per range, 23807.631953 ranges/s, heap diff 63053824
// maxDepth 21: 31.996µs per range, 31253.349138 ranges/s, heap diff 123289600
// maxDepth 21: 31.926µs per range, 31321.766830 ranges/s, heap diff 123256832
// maxDepth 21: 31.839µs per range, 31407.657854 ranges/s, heap diff 123256832
// maxDepth 22: 27.829µs per range, 35933.122150 ranges/s, heap diff 240689152
// maxDepth 22: 27.524µs per range, 36330.976995 ranges/s, heap diff 240689152
// maxDepth 22: 27.386µs per range, 36514.410406 ranges/s, heap diff 240689152
// maxDepth 23: 24.378µs per range, 41020.262869 ranges/s, heap diff 470024192
// maxDepth 23: 24.605µs per range, 40641.096389 ranges/s, heap diff 470056960
// maxDepth 23: 24.51µs per range, 40799.444720 ranges/s, heap diff 470040576

// maxDepth 18: 94.518µs per range, 10579.885738 ranges/s, heap diff 16621568
// maxDepth 18: 95.144µs per range, 10510.332936 ranges/s, heap diff 16572416
// maxDepth 18: 94.55µs per range, 10576.359829 ranges/s, heap diff 16588800
// maxDepth 19: 60.463µs per range, 16538.974879 ranges/s, heap diff 32325632
// maxDepth 19: 60.47µs per range, 16537.108181 ranges/s, heap diff 32358400
// maxDepth 19: 60.441µs per range, 16544.939001 ranges/s, heap diff 32333824
// maxDepth 20: 41.131µs per range, 24311.982297 ranges/s, heap diff 63078400
// maxDepth 20: 41.621µs per range, 24026.119996 ranges/s, heap diff 63086592
// maxDepth 20: 41.568µs per range, 24056.912641 ranges/s, heap diff 63094784
// maxDepth 21: 32.234µs per range, 31022.459566 ranges/s, heap diff 123256832
// maxDepth 21: 30.856µs per range, 32408.240119 ranges/s, heap diff 123248640
// maxDepth 21: 30.774µs per range, 32494.318758 ranges/s, heap diff 123224064
// maxDepth 22: 27.476µs per range, 36394.375781 ranges/s, heap diff 240689152
// maxDepth 22: 27.707µs per range, 36091.188900 ranges/s, heap diff 240705536
// maxDepth 22: 27.281µs per range, 36654.794863 ranges/s, heap diff 240705536
// maxDepth 23: 24.394µs per range, 40992.220132 ranges/s, heap diff 470048768
// maxDepth 23: 24.697µs per range, 40489.695824 ranges/s, heap diff 470040576
// maxDepth 23: 24.436µs per range, 40923.081488 ranges/s, heap diff 470032384

// maxDepth 15: 529.513µs per range, 1888.524885 ranges/s, heap diff 2293760
// maxDepth 15: 528.783µs per range, 1891.132520 ranges/s, heap diff 2244608
// maxDepth 15: 529.458µs per range, 1888.723450 ranges/s, heap diff 2252800
// maxDepth 16: 281.809µs per range, 3548.498801 ranges/s, heap diff 4390912
// maxDepth 16: 280.159µs per range, 3569.389929 ranges/s, heap diff 4382720
// maxDepth 16: 280.449µs per range, 3565.709031 ranges/s, heap diff 4390912
// maxDepth 17: 157.429µs per range, 6352.037713 ranges/s, heap diff 8527872
// maxDepth 17: 156.569µs per range, 6386.942961 ranges/s, heap diff 8527872
// maxDepth 17: 157.158µs per range, 6362.998907 ranges/s, heap diff 8527872
// maxDepth 18: 94.689µs per range, 10560.886016 ranges/s, heap diff 16547840
// maxDepth 18: 95.995µs per range, 10417.191145 ranges/s, heap diff 16564224
// maxDepth 18: 94.469µs per range, 10585.428908 ranges/s, heap diff 16515072
// maxDepth 19: 61.218µs per range, 16334.822475 ranges/s, heap diff 32342016
// maxDepth 19: 61.733µs per range, 16198.549404 ranges/s, heap diff 32350208
// maxDepth 19: 61.269µs per range, 16321.226214 ranges/s, heap diff 32309248
// maxDepth 20: 42.336µs per range, 23620.054892 ranges/s, heap diff 63053824
// maxDepth 20: 41.906µs per range, 23862.511368 ranges/s, heap diff 63094784
// maxDepth 20: 41.647µs per range, 24011.273302 ranges/s, heap diff 63086592
// maxDepth 21: 32.895µs per range, 30399.444906 ranges/s, heap diff 123256832
// maxDepth 21: 31.798µs per range, 31447.748207 ranges/s, heap diff 123256832
// maxDepth 21: 32.008µs per range, 31241.248008 ranges/s, heap diff 123265024
// maxDepth 22: 27.014µs per range, 37017.223157 ranges/s, heap diff 240689152
// maxDepth 22: 26.764µs per range, 37363.422097 ranges/s, heap diff 240664576
// maxDepth 22: 26.938µs per range, 37121.580267 ranges/s, heap diff 240664576
// maxDepth 23: 24.457µs per range, 40887.173321 ranges/s, heap diff 470040576
// maxDepth 23: 24.997µs per range, 40003.930386 ranges/s, heap diff 470040576
// maxDepth 23: 24.741µs per range, 40418.462446 ranges/s, heap diff 470040576

// TBD: ensure short prefix problem is not a bug!!!
