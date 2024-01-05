package hashsync

import (
	"cmp"
	"fmt"
	"math/rand"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

type sampleID string

var _ Ordered = sampleID("")

func (s sampleID) Compare(other Ordered) int {
	return cmp.Compare(s, other.(sampleID))
}

type sampleMonoid struct{}

var _ Monoid = sampleMonoid{}

func (m sampleMonoid) Identity() any         { return "" }
func (m sampleMonoid) Op(a, b any) any       { return a.(string) + b.(string) }
func (m sampleMonoid) Fingerprint(a any) any { return string(a.(sampleID)) }

func sampleCountMonoid() Monoid {
	return CombineMonoids(sampleMonoid{}, CountingMonoid{})
}

func makeStringConcatTree(chars string) MonoidTree {
	ids := make([]sampleID, len(chars))
	for n, c := range chars {
		ids[n] = sampleID(c)
	}
	return MonoidTreeFromSlice[sampleID](sampleCountMonoid(), ids)
}

// dumbAdd inserts the node into the tree without trying to maintain the
// red-black properties
func dumbAdd(mt MonoidTree, k Ordered) {
	mtree := mt.(*monoidTree)
	mtree.root = mtree.insert(mtree.root, k, nil, false, false)
}

// makeDumbTree constructs a binary tree by adding the chars one-by-one without
// trying to maintain the red-black properties
func makeDumbTree(chars string) MonoidTree {
	if len(chars) == 0 {
		panic("empty set")
	}
	mt := NewMonoidTree(sampleCountMonoid())
	for _, c := range chars {
		dumbAdd(mt, sampleID(c))
	}
	return mt
}

func makeRBTree(chars string) MonoidTree {
	mt := NewMonoidTree(sampleCountMonoid())
	for _, c := range chars {
		mt.Add(sampleID(c))
	}
	return mt
}

func gtePos(all string, item string) int {
	n := slices.IndexFunc([]byte(all), func(v byte) bool {
		return v >= item[0]
	})
	if n >= 0 {
		return n
	}
	return len(all)
}

func naiveRange(all, x, y string, stopCount int) (fingerprint, startStr, endStr string) {
	if len(all) == 0 {
		return "", "", ""
	}
	allBytes := []byte(all)
	slices.Sort(allBytes)
	all = string(allBytes)
	start := gtePos(all, x)
	end := gtePos(all, y)
	if x < y {
		if stopCount >= 0 && end-start > stopCount {
			end = start + stopCount
		}
		if end < len(all) {
			endStr = all[end : end+1]
		} else {
			endStr = all[0:1]
		}
		startStr = ""
		if start < len(all) {
			startStr = all[start : start+1]
		} else {
			startStr = all[0:1]
		}
		return all[start:end], startStr, endStr
	} else {
		r := all[start:] + all[:end]
		// fmt.Fprintf(os.Stderr, "QQQQQ: x %q start %d y %q end %d\n", x, start, y, end)
		if len(r) == 0 {
			// fmt.Fprintf(os.Stderr, "QQQQQ: x %q start %d y %q end %d -- ret start\n", x, start, y, end)
			return "", all[0:1], all[0:1]
		}
		if stopCount >= 0 && len(r) > stopCount {
			return r[:stopCount], r[0:1], r[stopCount : stopCount+1]
		}
		if end < len(all) {
			endStr = all[end : end+1]
		} else {
			endStr = all[0:1]
		}
		startStr = ""
		if len(r) != 0 {
			startStr = r[0:1]
		}
		return r, startStr, endStr
	}
}

func TestEmptyTree(t *testing.T) {
	tree := NewMonoidTree(sampleCountMonoid())
	rfp1, startNode, endNode := tree.RangeFingerprint(nil, sampleID("a"), sampleID("a"), nil)
	require.Nil(t, startNode)
	require.Nil(t, endNode)
	rfp2, startNode, endNode := tree.RangeFingerprint(nil, sampleID("a"), sampleID("c"), nil)
	require.Nil(t, startNode)
	require.Nil(t, endNode)
	rfp3, startNode, endNode := tree.RangeFingerprint(nil, sampleID("c"), sampleID("a"), nil)
	require.Nil(t, startNode)
	require.Nil(t, endNode)
	for _, fp := range []any{
		tree.Fingerprint(),
		rfp1,
		rfp2,
		rfp3,
	} {
		require.Equal(t, "", CombinedFirst[string](fp))
		require.Equal(t, 0, CombinedSecond[int](fp))
	}
}

func testMonoidTreeRanges(t *testing.T, tree MonoidTree) {
	all := "abcdefghijklmnopqr"
	for _, tc := range []struct {
		all     string
		x, y    sampleID
		gte     string
		fp      string
		stop    int
		startAt sampleID
		endAt   sampleID
	}{
		// normal ranges: [x, y) (x -> y)
		{x: "0", y: "9", stop: -1, startAt: "a", endAt: "a", fp: ""},
		{x: "x", y: "y", stop: -1, startAt: "a", endAt: "a", fp: ""},
		{x: "a", y: "b", stop: -1, startAt: "a", endAt: "b", fp: "a"},
		{x: "a", y: "d", stop: -1, startAt: "a", endAt: "d", fp: "abc"},
		{x: "f", y: "o", stop: -1, startAt: "f", endAt: "o", fp: "fghijklmn"},
		{x: "0", y: "y", stop: -1, startAt: "a", endAt: "a", fp: "abcdefghijklmnopqr"},
		{x: "a", y: "r", stop: -1, startAt: "a", endAt: "r", fp: "abcdefghijklmnopq"},
		// full rollover range x -> end -> x, or [x, max) + [min, x)
		{x: "a", y: "a", stop: -1, startAt: "a", endAt: "a", fp: "abcdefghijklmnopqr"},
		{x: "l", y: "l", stop: -1, startAt: "l", endAt: "l", fp: "lmnopqrabcdefghijk"},
		// rollover ranges: x -> end -> y, or [x, max), [min, y)
		{x: "l", y: "f", stop: -1, startAt: "l", endAt: "f", fp: "lmnopqrabcde"},
		{x: "l", y: "0", stop: -1, startAt: "l", endAt: "a", fp: "lmnopqr"},
		{x: "y", y: "f", stop: -1, startAt: "a", endAt: "f", fp: "abcde"},
		{x: "y", y: "x", stop: -1, startAt: "a", endAt: "a", fp: "abcdefghijklmnopqr"},
		{x: "9", y: "0", stop: -1, startAt: "a", endAt: "a", fp: "abcdefghijklmnopqr"},
		{x: "s", y: "a", stop: -1, startAt: "a", endAt: "a", fp: ""},
		// normal ranges + stop
		{x: "a", y: "q", stop: 0, startAt: "a", endAt: "a", fp: ""},
		{x: "a", y: "q", stop: 3, startAt: "a", endAt: "d", fp: "abc"},
		{x: "a", y: "q", stop: 5, startAt: "a", endAt: "f", fp: "abcde"},
		{x: "a", y: "q", stop: 7, startAt: "a", endAt: "h", fp: "abcdefg"},
		{x: "a", y: "q", stop: 16, startAt: "a", endAt: "q", fp: "abcdefghijklmnop"},
		// rollover ranges + stop
		{x: "l", y: "f", stop: 3, startAt: "l", endAt: "o", fp: "lmn"},
		{x: "l", y: "f", stop: 8, startAt: "l", endAt: "b", fp: "lmnopqra"},
		{x: "y", y: "x", stop: 5, startAt: "a", endAt: "f", fp: "abcde"},
		// full rollover range + stop
		{x: "a", y: "a", stop: 3, startAt: "a", endAt: "d", fp: "abc"},
		{x: "a", y: "a", stop: 10, startAt: "a", endAt: "k", fp: "abcdefghij"},
		{x: "l", y: "l", stop: 3, startAt: "l", endAt: "o", fp: "lmn"},
	} {
		testName := fmt.Sprintf("%s-%s", tc.x, tc.y)
		if tc.stop >= 0 {
			testName += fmt.Sprintf("-%d", tc.stop)
		}
		t.Run(testName, func(t *testing.T) {
			rootFP := tree.Fingerprint()
			require.Equal(t, all, CombinedFirst[string](rootFP))
			require.Equal(t, len(all), CombinedSecond[int](rootFP))
			stopCounts := []int{tc.stop}
			if tc.stop < 0 {
				// Stop point at the end of the sequence or beyond it
				// should produce the same results as no stop point at all
				stopCounts = append(stopCounts, len(all), len(all)*2)
			}
			for _, stopCount := range stopCounts {
				// make sure naiveRangeWithStopCount works as epxected, even
				// though it is only used for tests
				fpStr, startStr, endStr := naiveRange(all, string(tc.x), string(tc.y), stopCount)
				require.Equal(t, tc.fp, fpStr, "naive fingerprint")
				require.Equal(t, string(tc.startAt), startStr, "naive fingerprint: startAt")
				require.Equal(t, string(tc.endAt), endStr, "naive fingerprint: endAt")

				var stop FingerprintPredicate
				if stopCount >= 0 {
					// stopCount is not used after this iteration
					// so it's ok to have it captured in the closure
					stop = func(fp any) bool {
						count := CombinedSecond[int](fp)
						return count > stopCount
					}
				}
				fp, startNode, endNode := tree.RangeFingerprint(nil, tc.x, tc.y, stop)
				require.Equal(t, tc.fp, CombinedFirst[string](fp), "fingerprint")
				require.Equal(t, len(tc.fp), CombinedSecond[int](fp), "count")
				require.NotNil(t, startNode, "start node")
				require.NotNil(t, endNode, "end node")
				require.Equal(t, tc.startAt, startNode.Key(), "start node key")
				require.Equal(t, tc.endAt, endNode.Key(), "end node key")
			}
		})
	}
}

func TestMonoidTreeRanges(t *testing.T) {
	t.Run("pre-balanced tree", func(t *testing.T) {
		testMonoidTreeRanges(t, makeStringConcatTree("abcdefghijklmnopqr"))
	})
	t.Run("sequential add", func(t *testing.T) {
		testMonoidTreeRanges(t, makeDumbTree("abcdefghijklmnopqr"))
	})
	t.Run("shuffled add", func(t *testing.T) {
		testMonoidTreeRanges(t, makeDumbTree("lodrnifeqacmbhkgjp"))
	})
	t.Run("red-black add", func(t *testing.T) {
		testMonoidTreeRanges(t, makeRBTree("lodrnifeqacmbhkgjp"))
	})
}

func TestAscendingRanges(t *testing.T) {
	all := "abcdefghijklmnopqr"
	tree := makeRBTree(all)
	for _, tc := range []struct {
		name         string
		ranges       []string
		fingerprints []string
	}{
		{
			name:         "normal ranges",
			ranges:       []string{"ac", "cj", "lq", "qr"},
			fingerprints: []string{"ab", "cdefghi", "lmnop", "q"},
		},
		{
			name:         "normal and inverted ranges",
			ranges:       []string{"xc", "cj", "p0"},
			fingerprints: []string{"ab", "cdefghi", "pqr"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var fps []string
			var node MonoidTreePointer
			for n, rng := range tc.ranges {
				x := sampleID(rng[0])
				y := sampleID(rng[1])
				if n > 0 {
					require.NotNil(t, node, "nil starting node for range %s-%s", x, y)
				}
				fpStr, _, _ := naiveRange(all, string(x), string(y), -1)
				var fp any
				fp, _, node = tree.RangeFingerprint(node, x, y, nil)
				actualFP := CombinedFirst[string](fp)
				require.Equal(t, len(actualFP), CombinedSecond[int](fp), "count")
				require.Equal(t, fpStr, actualFP)
				fps = append(fps, actualFP)
			}
			require.Equal(t, tc.fingerprints, fps, "fingerprints")
		})
	}
}

func verifyBinaryTree(t *testing.T, mn *monoidTreeNode) {
	cloned := mn.flags&flagCloned != 0
	if mn.left != nil {
		if !cloned {
			require.Zero(t, mn.left.flags&flagCloned, "cloned left child of a non-cloned node")
		}
		require.Negative(t, mn.left.key.Compare(mn.key))
		// not a "real" pointer (no parent stack), just to get max
		leftMax := &monoidTreePointer{node: mn.left}
		leftMax.max()
		require.Negative(t, leftMax.Key().Compare(mn.key))
		verifyBinaryTree(t, mn.left)
	}

	if mn.right != nil {
		if !cloned {
			require.Zero(t, mn.right.flags&flagCloned, "cloned right child of a non-cloned node")
		}
		require.Positive(t, mn.right.key.Compare(mn.key))
		// not a "real" pointer (no parent stack), just to get min
		rightMin := &monoidTreePointer{node: mn.right}
		rightMin.min()
		require.Positive(t, rightMin.Key().Compare(mn.key))
		verifyBinaryTree(t, mn.right)
	}
}

func verifyRedBlackNode(t *testing.T, mn *monoidTreeNode, blackDepth int) int {
	if mn == nil {
		return blackDepth + 1
	}
	if mn.flags&flagBlack == 0 {
		if mn.left != nil {
			require.Equal(t, flagBlack, mn.left.flags&flagBlack, "left child of a red node is red")
		}
		if mn.right != nil {
			require.Equal(t, flagBlack, mn.right.flags&flagBlack, "right child of a red node is red")
		}
	} else {
		blackDepth++
	}
	bdLeft := verifyRedBlackNode(t, mn.left, blackDepth)
	bdRight := verifyRedBlackNode(t, mn.right, blackDepth)
	require.Equal(t, bdLeft, bdRight, "subtree black depth for node %s", mn.key)
	return bdLeft
}

func verifyRedBlack(t *testing.T, mt *monoidTree) {
	if mt.root == nil {
		return
	}
	require.Equal(t, flagBlack, mt.root.flags&flagBlack, "root node must be black")
	verifyRedBlackNode(t, mt.root, 0)
}

func TestRedBlackTreeInsert(t *testing.T) {
	for i := 0; i < 1000; i++ {
		tree := NewMonoidTree(sampleCountMonoid())
		items := []byte("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz")
		count := rand.Intn(len(items)) + 1
		items = items[:count]
		shuffled := append([]byte(nil), items...)
		rand.Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})

		// items := []byte("0123456789ABCDEFG")
		// shuffled := []byte("0678DF1CG5A9324BE")

		trees := make([]MonoidTree, len(shuffled))
		treeDumps := make([]string, len(shuffled))
		for i := 0; i < len(shuffled); i++ {
			trees[i] = tree.Copy()
			treeDumps[i] = tree.Dump()
			require.Equal(t, treeDumps[i], trees[i].Dump(), "initial tree dump %d", i)
			tree.Add(sampleID(shuffled[i]))
			if i >= 3 && i%3 == 0 {
				// this shouldn't change anything
				trees[i-1].Add(sampleID(shuffled[rand.Intn(i-1)]))
				// cloning should not happen b/c no new nodes are inserted
				require.Zero(t, trees[i-1].(*monoidTree).root.flags&flagCloned)
			}
		}

		for i := 0; i < len(shuffled); i++ {
			require.Equal(t, treeDumps[i], trees[i].Dump(), "tree dump %d after copy", i)
		}

		var actualItems []byte
		n := 0
		// t.Logf("items: %q", string(items))
		// t.Logf("shuffled: %q", string(shuffled))
		// t.Logf("QQQQQ: tree:\n%s", tree.Dump())
		verifyBinaryTree(t, tree.(*monoidTree).root)
		verifyRedBlack(t, tree.(*monoidTree))
		for ptr := tree.Min(); ptr.Key() != nil; ptr.Next() {
			// avoid endless loop due to bugs in the tree impl
			require.Less(t, n, len(items)*2, "got much more items than needed: %q -- %q", actualItems, shuffled)
			n++
			actualItems = append(actualItems, ptr.Key().(sampleID)[0])
		}
		require.Equal(t, items, actualItems)

		fp, startNode, endNode := tree.RangeFingerprint(nil, sampleID(items[0]), sampleID(items[0]), nil)
		fpStr := CombinedFirst[string](fp)
		require.Equal(t, string(items), fpStr, "fingerprint %q", shuffled)
		require.Equal(t, len(fpStr), CombinedSecond[int](fp), "count %q")
		require.Equal(t, sampleID(items[0]), startNode.Key(), "startNode")
		require.Equal(t, sampleID(items[0]), endNode.Key(), "endNode")
	}
}

type makeTestTreeFunc func(chars string) MonoidTree

func testRandomOrderAndRanges(t *testing.T, mktree makeTestTreeFunc) {
	all := "abcdefghijklmnopqr"
	for i := 0; i < 1000; i++ {
		shuffled := []byte(all)
		rand.Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})
		tree := makeDumbTree(string(shuffled))
		x := sampleID(shuffled[rand.Intn(len(shuffled))])
		y := sampleID(shuffled[rand.Intn(len(shuffled))])
		stopCount := rand.Intn(len(shuffled)+2) - 1
		var stop FingerprintPredicate
		if stopCount >= 0 {
			stop = func(fp any) bool {
				return CombinedSecond[int](fp) > stopCount
			}
		}

		verify := func() {
			expFP, expStart, expEnd := naiveRange(all, string(x), string(y), stopCount)
			fp, startNode, endNode := tree.RangeFingerprint(nil, x, y, stop)

			fpStr := CombinedFirst[string](fp)
			curCase := fmt.Sprintf("items %q x %q y %q stopCount %d", shuffled, x, y, stopCount)
			require.Equal(t, expFP, fpStr, "%s: fingerprint", curCase)
			require.Equal(t, len(fpStr), CombinedSecond[int](fp), "%s: count", curCase)

			startStr := ""
			if startNode != nil {
				startStr = string(startNode.Key().(sampleID))
			}
			require.Equal(t, expStart, startStr, "%s: next", curCase)

			endStr := ""
			if endNode != nil {
				endStr = string(endNode.Key().(sampleID))
			}
			require.Equal(t, expEnd, endStr, "%s: next", curCase)
		}
		verify()
		tree1 := tree.Copy()
		tree1.Add(sampleID("s"))
		tree1.Add(sampleID("t"))
		tree1.Add(sampleID("u"))
		verify() // the original tree should be unchanged
		fp, _, _ := tree1.RangeFingerprint(nil, sampleID("a"), sampleID("a"), nil)
		require.Equal(t, "abcdefghijklmnopqrstu", CombinedFirst[string](fp))
		require.Equal(t, len(all)+3, CombinedSecond[int](fp))
	}
}

func TestRandomOrderAndRanges(t *testing.T) {
	t.Run("randomized dumb insert", func(t *testing.T) {
		testRandomOrderAndRanges(t, makeDumbTree)
	})
	t.Run("red-black tree", func(t *testing.T) {
		testRandomOrderAndRanges(t, makeRBTree)
	})
}

func TestTreeValues(t *testing.T) {
	tree := makeRBTree("")
	tree.Add(sampleID("a"))
	tree.Set(sampleID("b"), 123)
	tree.Set(sampleID("d"), 456)
	verifyOrig := func() {
		v, found := tree.Lookup(sampleID("a"))
		require.True(t, found)
		require.Nil(t, v)
		v, found = tree.Lookup(sampleID("b"))
		require.True(t, found)
		require.Equal(t, 123, v)
		v, found = tree.Lookup(sampleID("c"))
		require.False(t, found)
		require.Nil(t, v)
		v, found = tree.Lookup(sampleID("d"))
		require.True(t, found)
		require.Equal(t, 456, v)
	}
	verifyOrig()

	treeDump := tree.Dump()
	tree1 := tree.Copy()

	// flagCloned on the root should be cleared after copy
	// and not set again by Set b/c the value is the same
	tree.Set(sampleID("d"), 456) // nothing changed
	require.Zero(t, tree.(*monoidTree).root.flags&flagCloned)

	tree1.Set(sampleID("b"), 1234)
	tree1.Set(sampleID("c"), 222)
	verifyOrig()
	require.Equal(t, treeDump, tree.Dump())
	v, found := tree1.Lookup(sampleID("a"))
	require.True(t, found)
	require.Nil(t, v)
	v, found = tree1.Lookup(sampleID("b"))
	require.True(t, found)
	require.Equal(t, 1234, v)
	v, found = tree1.Lookup(sampleID("c"))
	require.True(t, found)
	require.Equal(t, 222, v)
	v, found = tree1.Lookup(sampleID("d"))
	require.True(t, found)
	require.Equal(t, 456, v)
}
