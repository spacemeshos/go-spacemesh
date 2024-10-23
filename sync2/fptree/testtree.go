package fptree

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

// checkNode checks that the tree node at the given index is correct and also recursively
// checks its children.
func checkNode(t *testing.T, ft *FPTree, idx nodeIndex, depth int) {
	left := ft.np.left(idx)
	right := ft.np.right(idx)
	if left == noIndex && right == noIndex {
		if ft.np.count(idx) != 1 {
			assert.Equal(t, depth, ft.maxDepth)
		} else if ft.maxDepth == 0 && ft.idStore == nil {
			assert.NotNil(t, ft.np.value(idx), "leaf node must have a value if there's no idStore")
		}
	} else {
		if ft.maxDepth != 0 {
			assert.Less(t, depth, ft.maxDepth)
		}
		var expFP rangesync.Fingerprint
		var expCount uint32
		if left != noIndex {
			checkNode(t, ft, left, depth+1)
			count, fp, _ := ft.np.info(left)
			expFP.Update(fp[:])
			expCount += count
		}
		if right != noIndex {
			checkNode(t, ft, right, depth+1)
			count, fp, _ := ft.np.info(right)
			expFP.Update(fp[:])
			expCount += count
		}
		count, fp, _ := ft.np.info(idx)
		assert.Equal(t, expFP, fp, "node fp at depth %d", depth)
		assert.Equal(t, expCount, count, "node count at depth %d", depth)
	}
}

// CheckTree checks that the tree has correct structure.
func CheckTree(t *testing.T, ft *FPTree) {
	if ft.root != noIndex {
		checkNode(t, ft, ft.root, 0)
	}
}

// analyzeTreeNodeRefs checks that the reference counts in the node pool are correct.
func analyzeTreeNodeRefs(t *testing.T, np *nodePool, trees ...*FPTree) {
	m := make(map[nodeIndex]map[nodeIndex]bool)
	var rec func(*FPTree, nodeIndex, nodeIndex)
	rec = func(ft *FPTree, idx, from nodeIndex) {
		if idx == noIndex {
			return
		}
		if _, ok := m[idx]; !ok {
			m[idx] = make(map[nodeIndex]bool)
		}
		m[idx][from] = true
		rec(ft, np.left(idx), idx)
		rec(ft, np.right(idx), idx)
	}
	for n, ft := range trees {
		treeRef := nodeIndex(-n - 1)
		rec(ft, ft.root, treeRef)
	}
	for n, entry := range np.rcPool.entries {
		if entry.refCount&freeBit != 0 {
			continue
		}
		numTreeRefs := len(m[nodeIndex(n)])
		if numTreeRefs == 0 {
			assert.Fail(t, "analyzeUnref: NOT REACHABLE", "idx: %d", n)
		} else {
			assert.Equal(t, numTreeRefs, int(entry.refCount), "analyzeRef: refCount for %d", n)
		}
	}
}

// AnalyzeTreeNodeRefs checks that the reference counts are correct for the given trees in
// their respective node pools.
func AnalyzeTreeNodeRefs(t *testing.T, trees ...*FPTree) {
	t.Helper()
	// group trees by node pool they use
	nodePools := make(map[*nodePool][]*FPTree)
	for _, ft := range trees {
		nodePools[ft.np] = append(nodePools[ft.np], ft)
	}
	for np, trees := range nodePools {
		analyzeTreeNodeRefs(t, np, trees...)
	}
}
