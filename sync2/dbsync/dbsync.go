package dbsync

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"math/bits"
	"slices"
	"strconv"
)

const (
	fingerprintBytes = 12
	// cachedBits       = 24
	// cachedSize       = 1 << cachedBits
	// cacheMask        = cachedSize - 1
	maxIDBytes = 32
	bit63      = 1 << 63
)

type fingerprint [fingerprintBytes]byte

func (fp fingerprint) Compare(other fingerprint) int {
	return bytes.Compare(fp[:], other[:])
}

func (fp fingerprint) String() string {
	return hex.EncodeToString(fp[:])
}

func (fp *fingerprint) update(h []byte) {
	for n := range *fp {
		(*fp)[n] ^= h[n]
	}
}

func (fp *fingerprint) bitFromLeft(n int) bool {
	if n > fingerprintBytes*8 {
		panic("BUG: bad fingerprint bit index")
	}
	return (fp[n>>3]>>(7-n&0x7))&1 != 0
}

func hexToFingerprint(s string) fingerprint {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic("bad hex fingerprint: " + err.Error())
	}
	var fp fingerprint
	if len(b) != len(fp) {
		panic("bad hex fingerprint")
	}
	copy(fp[:], b)
	return fp
}

// const (
// 	nodeFlagLeaf = 1 << 31
// 	nodeFlagMask = nodeFlagLeaf
// )

// NOTE: all leafs are on the last level

// type nodeIndex uint32

// const noIndex nodeIndex = ^nodeIndex(0)

// // TODO: nodePool limiting
// type nodePool struct {
// 	mtx   sync.Mutex
// 	nodes []node
// 	// freeList is 1-based so that nodePool doesn't need a constructor
// 	freeList nodeIndex
// }

// func (np *nodePool) node(idx nodeIndex) node {
// 	np.mtx.Lock()
// 	defer np.mtx.Unlock()
// 	return np.nodeUnlocked(idx)
// }

// func (np *nodePool) nodeUnlocked(idx nodeIndex) node {
// 	node := &np.nodes[idx]
// 	refs := node.refCount
// 	if refs < 0 {
// 		panic("BUG: negative nodePool entry refcount")
// 	} else if refs == 0 {
// 		panic("BUG: referencing a free nodePool entry")
// 	}
// 	return *node
// }

// func (np *nodePool) add(fp fingerprint, c uint32, left, right nodeIndex) nodeIndex {
// 	np.mtx.Lock()
// 	defer np.mtx.Unlock()
// 	var idx nodeIndex
// 	// validate indices
// 	if left != noIndex {
// 		np.nodeUnlocked(left)
// 	}
// 	if right != noIndex {
// 		np.nodeUnlocked(right)
// 	}
// 	if np.freeList != 0 {
// 		idx = nodeIndex(np.freeList - 1)
// 		np.freeList = np.nodes[idx].left
// 		np.nodes[idx].refCount++
// 		if np.nodes[idx].refCount != 1 {
// 			panic("BUG: refCount != 1 for a node taken from the freelist")
// 		}
// 	} else {
// 		idx = nodeIndex(len(np.nodes))
// 		np.nodes = append(np.nodes, node{refCount: 1})
// 	}
// 	node := &np.nodes[idx]
// 	node.fp = fp
// 	node.c = c
// 	node.left = left
// 	node.right = right
// 	return idx
// }

// func (np *nodePool) release(idx nodeIndex) {
// 	np.mtx.Lock()
// 	defer np.mtx.Unlock()
// 	node := &np.nodes[idx]
// 	if node.refCount <= 0 {
// 		panic("BUG: negative nodePool entry refcount")
// 	}
// 	node.refCount--
// 	if node.refCount == 0 {
// 		node.left = np.freeList
// 		np.freeList = idx + 1
// 	}
// }

// func (np *nodePool) ref(idx nodeIndex) {
// 	np.mtx.Lock()
// 	np.nodes[idx].refCount++
// 	np.mtx.Unlock()
// }

type nodeIndex uint32

const noIndex = ^nodeIndex(0)

type nodePool struct {
	rcPool[node, nodeIndex]
}

func (np *nodePool) add(fp fingerprint, c uint32, left, right nodeIndex) nodeIndex {
	return np.rcPool.add(node{fp: fp, c: c, left: left, right: right})
}

func (np *nodePool) node(idx nodeIndex) node {
	return np.rcPool.item(idx)
}

// fpTree node.
// The nodes are immutable except for refCount field, which should
// only be used directly by nodePool methods
type node struct {
	fp          fingerprint
	c           uint32
	left, right nodeIndex
}

func (n node) leaf() bool {
	return n.left == noIndex && n.right == noIndex
}

// type node struct {
// 	// 16-byte structure with alignment
// 	// The cache is 512 MiB per 1<<24 (16777216) IDs
// 	fp fingerprint
// 	c  uint32
// }

// func (node *node) empty() bool {
// 	return node.c == 0
// }

// func (node *node) leaf() bool {
// 	return node.c&nodeFlagLeaf != 0
// }

// func (node *node) count() uint32 {
// 	if node.leaf() {
// 		return 1
// 	}
// 	return node.c
// }

const (
	prefixLenBits = 6
	prefixLenMask = 1<<prefixLenBits - 1
	prefixBitMask = ^uint64(prefixLenMask)
	maxPrefixLen  = 64 - prefixLenBits
)

type prefix uint64

func (p prefix) len() int {
	return int(p & prefixLenMask)
}

func (p prefix) bits() uint64 {
	return uint64(p >> prefixLenBits)
}

func (p prefix) left() prefix {
	l := uint64(p) & prefixLenMask
	if l == maxPrefixLen {
		panic("BUG: max prefix len reached")
	}
	return prefix((uint64(p)&prefixBitMask)<<1 + l + 1)
}

func (p prefix) right() prefix {
	return p.left() + (1 << prefixLenBits)
}

func (p prefix) dir(bit bool) prefix {
	if bit {
		return p.right()
	}
	return p.left()
}

func (p prefix) String() string {
	if p.len() == 0 {
		return "<0>"
	}
	b := fmt.Sprintf("%064b", p.bits())
	return fmt.Sprintf("<%d:%s>", p.len(), b[64-p.len():])
}

func (p prefix) highBit() bool {
	if p == 0 {
		return false
	}
	return p.bits()>>(p.len()-1) != 0
}

// shift removes the highest bit from the prefix
// TBD: QQQQQ: test shift
func (p prefix) shift() prefix {
	switch l := uint64(p.len()); l {
	case 0:
		panic("BUG: can't shift zero prefix")
	case 1:
		return 0
	default:
		return prefix(((p.bits() & ((1 << (l - 1)) - 1)) << prefixLenBits) + l - 1)
	}
}

func load64(h []byte) uint64 {
	return binary.BigEndian.Uint64(h[:8])
}

// func hashPrefix(h []byte, nbits int) prefix {
// 	if nbits < 0 || nbits > maxPrefixLen {
// 		panic("BUG: bad prefix length")
// 	}
// 	if nbits == 0 {
// 		return 0
// 	}
// 	v := load64(h)
// 	return prefix((v>>(64-nbits-prefixLenBits))&prefixBitMask + uint64(nbits))
// }

func preFirst0(h []byte) prefix {
	l := min(maxPrefixLen, bits.LeadingZeros64(^load64(h)))
	return prefix(((1<<l)-1)<<prefixLenBits + l)
}

func preFirst1(h []byte) prefix {
	return prefix(min(maxPrefixLen, bits.LeadingZeros64(load64(h))))
}

func commonPrefix(a, b []byte) prefix {
	v1 := load64(a)
	v2 := load64(b)
	l := uint64(min(maxPrefixLen, bits.LeadingZeros64(v1^v2)))
	return prefix((v1>>(64-l))<<prefixLenBits + l)
}

type aggResult struct {
	tailRefs []uint64
	fp       fingerprint
	count    uint32
	itype    int
}

func (r *aggResult) update(node node) {
	r.fp.update(node.fp[:])
	r.count += node.c
	// fmt.Fprintf(os.Stderr, "QQQQQ: r.count <= %d r.fp <= %s\n", r.count, r.fp)
}

type fpTree struct {
	np       *nodePool
	root     nodeIndex
	maxDepth int
}

func newFPTree(np *nodePool, maxDepth int) *fpTree {
	return &fpTree{np: np, root: noIndex, maxDepth: maxDepth}
}

func (ft *fpTree) pushDown(fpA, fpB fingerprint, p prefix, curCount uint32) nodeIndex {
	// fmt.Fprintf(os.Stderr, "QQQQQ: pushDown: fpA %s fpB %s p %s\n", fpA.String(), fpB.String(), p)
	fpCombined := fpA
	fpCombined.update(fpB[:])
	if ft.maxDepth != 0 && p.len() == ft.maxDepth {
		// fmt.Fprintf(os.Stderr, "QQQQQ: pushDown: add at maxDepth\n")
		return ft.np.add(fpCombined, curCount+1, noIndex, noIndex)
	}
	if curCount != 1 {
		panic("BUG: pushDown of non-1-leaf below maxDepth")
	}
	dirA := fpA.bitFromLeft(p.len())
	dirB := fpB.bitFromLeft(p.len())
	// fmt.Fprintf(os.Stderr, "QQQQQ: pushDown: bitFromLeft %d: dirA %v dirB %v\n", p.len(), dirA, dirB)
	if dirA == dirB {
		childIdx := ft.pushDown(fpA, fpB, p.dir(dirA), 1)
		if dirA {
			// fmt.Fprintf(os.Stderr, "QQQQQ: pushDown: sameDir: left\n")
			return ft.np.add(fpCombined, 2, noIndex, childIdx)
		} else {
			// fmt.Fprintf(os.Stderr, "QQQQQ: pushDown: sameDir: right\n")
			return ft.np.add(fpCombined, 2, childIdx, noIndex)
		}
	}

	idxA := ft.np.add(fpA, 1, noIndex, noIndex)
	idxB := ft.np.add(fpB, curCount, noIndex, noIndex)
	if dirA {
		// fmt.Fprintf(os.Stderr, "QQQQQ: pushDown: add A-B\n")
		return ft.np.add(fpCombined, 2, idxB, idxA)
	} else {
		// fmt.Fprintf(os.Stderr, "QQQQQ: pushDown: add B-A\n")
		return ft.np.add(fpCombined, 2, idxA, idxB)
	}
}

func (ft *fpTree) addValue(fp fingerprint, p prefix, idx nodeIndex) nodeIndex {
	if idx == noIndex {
		// fmt.Fprintf(os.Stderr, "QQQQQ: addValue: addNew fp %s p %s idx %d\n", fp.String(), p.String(), idx)
		return ft.np.add(fp, 1, noIndex, noIndex)
	}
	node := ft.np.node(idx)
	// We've got a copy of the node, so we release it right away.
	// This way, it'll likely be reused for the new nodes created
	// as this hash is being added, as the node pool's freeList is
	// LIFO
	ft.np.release(idx)
	if node.c == 1 || (ft.maxDepth != 0 && p.len() == ft.maxDepth) {
		// fmt.Fprintf(os.Stderr, "QQQQQ: addValue: pushDown fp %s p %s idx %d\n", fp.String(), p.String(), idx)
		// we're at a leaf node, need to push down the old fingerprint, or,
		// if we've reached the max depth, just update the current node
		return ft.pushDown(fp, node.fp, p, node.c)
	}
	fpCombined := fp
	fpCombined.update(node.fp[:])
	if fp.bitFromLeft(p.len()) {
		// fmt.Fprintf(os.Stderr, "QQQQQ: addValue: replaceRight fp %s p %s idx %d\n", fp.String(), p.String(), idx)
		newRight := ft.addValue(fp, p.right(), node.right)
		return ft.np.add(fpCombined, node.c+1, node.left, newRight)
	} else {
		// fmt.Fprintf(os.Stderr, "QQQQQ: addValue: replaceLeft fp %s p %s idx %d\n", fp.String(), p.String(), idx)
		newLeft := ft.addValue(fp, p.left(), node.left)
		return ft.np.add(fpCombined, node.c+1, newLeft, node.right)
	}
}

func (ft *fpTree) addHash(h []byte) {
	var fp fingerprint
	fp.update(h)
	ft.root = ft.addValue(fp, 0, ft.root)
	// fmt.Fprintf(os.Stderr, "QQQQQ: addHash: new root %d\n", ft.root)
}

func (ft *fpTree) followPrefix(from nodeIndex, p prefix) (nodeIndex, bool) {
	// fmt.Fprintf(os.Stderr, "QQQQQ: followPrefix: from %d p %s highBit %v\n", from, p, p.highBit())
	switch {
	case p == 0:
		return from, true
	case from == noIndex:
		return noIndex, false
	case ft.np.node(from).leaf():
		return from, false
	case p.highBit():
		return ft.followPrefix(ft.np.node(from).right, p.shift())
	default:
		return ft.followPrefix(ft.np.node(from).left, p.shift())
	}
}

func (ft *fpTree) tailRefFromPrefix(p prefix) uint64 {
	if p.len() != ft.maxDepth {
		panic("BUG: tail from short prefix")
	}
	return p.bits()
}

func (ft *fpTree) tailRefFromFingerprint(fp fingerprint) uint64 {
	v := load64(fp[:])
	if ft.maxDepth >= 64 {
		return v
	}
	// fmt.Fprintf(os.Stderr, "QQQQQ: AAAAA: v %016x maxDepth %d shift %d\n", v, ft.maxDepth, (64 - ft.maxDepth))
	return v >> (64 - ft.maxDepth)
}

func (ft *fpTree) tailRefFromNodeAndPrefix(n node, p prefix) uint64 {
	if n.c == 1 {
		return ft.tailRefFromFingerprint(n.fp)
	} else {
		return ft.tailRefFromPrefix(p)
	}
}

func (ft *fpTree) aggregateLeft(idx nodeIndex, v uint64, p prefix, r *aggResult) {
	if idx == noIndex {
		// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateLeft %d %016x %s: noIndex\n", idx, v, p)
		return
	}
	node := ft.np.node(idx)
	switch {
	case p.len() == ft.maxDepth:
		if node.left != noIndex || node.right != noIndex {
			panic("BUG: node @ maxDepth has children")
		}
		tail := ft.tailRefFromPrefix(p)
		r.tailRefs = append(r.tailRefs, tail)
		// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateLeft %d %016x %s: hit maxDepth, add prefix to the tails: %016x\n", idx, v, p, tail)
	case node.leaf():
		// For leaf 1-nodes, we can use the fingerprint to get tailRef
		// by which the actual IDs will be selected
		if node.c != 1 {
			panic("BUG: leaf non-1 node below maxDepth")
		}
		tail := ft.tailRefFromFingerprint(node.fp)
		r.tailRefs = append(r.tailRefs, tail)
		// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateLeft %d %016x %s: hit 1-leaf, add prefix to the tails: %016x (fp %s)\n", idx, v, p, tail, node.fp)
	case v&bit63 == 0:
		// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateLeft %d %016x %s: incl right node %d + go left to node %d\n", idx, v, p, node.right, node.left)
		if node.right != noIndex {
			r.update(ft.np.node(node.right))
		}
		ft.aggregateLeft(node.left, v<<1, p.left(), r)
	default:
		// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateLeft %d %016x %s: go right node %d\n", idx, v, p, node.right)
		ft.aggregateLeft(node.right, v<<1, p.right(), r)
	}
}

func (ft *fpTree) aggregateRight(idx nodeIndex, v uint64, p prefix, r *aggResult) {
	if idx == noIndex {
		// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateRight %d %016x %s: noIndex\n", idx, v, p)
		return
	}
	node := ft.np.node(idx)
	switch {
	case p.len() == ft.maxDepth:
		if node.left != noIndex || node.right != noIndex {
			panic("BUG: node @ maxDepth has children")
		}
		tail := ft.tailRefFromPrefix(p)
		r.tailRefs = append(r.tailRefs, tail)
		// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateRight %d %016x %s: hit maxDepth, add prefix to the tails: %016x\n", idx, v, p, tail)
	case node.leaf():
		// For leaf 1-nodes, we can use the fingerprint to get tailRef
		// by which the actual IDs will be selected
		if node.c != 1 {
			panic("BUG: leaf non-1 node below maxDepth")
		}
		tail := ft.tailRefFromFingerprint(node.fp)
		r.tailRefs = append(r.tailRefs, tail)
		// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateRight %d %016x %s: hit 1-leaf, add prefix to the tails: %016x (fp %s)\n", idx, v, p, tail, node.fp)
	case v&bit63 == 0:
		// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateRight %d %016x %s: go left to node %d\n", idx, v, p, node.left)
		ft.aggregateRight(node.left, v<<1, p.left(), r)
	default:
		// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateRight %d %016x %s: incl left node %d + go right to node %d\n", idx, v, p, node.left, node.right)
		if node.left != noIndex {
			r.update(ft.np.node(node.left))
		}
		ft.aggregateRight(node.right, v<<1, p.right(), r)
	}
}

func (ft *fpTree) aggregateInterval(x, y []byte) aggResult {
	// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateInterval: %s %s\n", hex.EncodeToString(x), hex.EncodeToString(y))
	var r aggResult
	r.itype = bytes.Compare(x, y)
	switch {
	case r.itype == 0:
		// the whole set
		if ft.root != noIndex {
			r.update(ft.np.node(ft.root))
		}
	case r.itype < 0:
		// "proper" interval: [x; lca); (lca; y)
		p := commonPrefix(x, y)
		lca, found := ft.followPrefix(ft.root, p)
		// fmt.Fprintf(os.Stderr, "QQQQQ: commonPrefix %s lca %d found %v\n", p, lca, found)
		switch {
		case found:
			lcaNode := ft.np.node(lca)
			ft.aggregateLeft(lcaNode.left, load64(x)<<(p.len()+1), p.left(), &r)
			ft.aggregateRight(lcaNode.right, load64(y)<<(p.len()+1), p.right(), &r)
		case lca != noIndex:
			// fmt.Fprintf(os.Stderr, "QQQQQ: commonPrefix %s NOT found but have lca %d\n", p, lca)
			// Didn't reach LCA in the tree b/c ended up
			// at a leaf, just use the prefix to go
			// through the IDs
			lcaNode := ft.np.node(lca)
			r.tailRefs = append(r.tailRefs, ft.tailRefFromNodeAndPrefix(lcaNode, p))
		}
	default:
		// inverse interval: [min; y); [x; max]
		pf1 := preFirst1(y)
		idx1, found := ft.followPrefix(ft.root, pf1)
		switch {
		case found:
			ft.aggregateRight(idx1, load64(y)<<pf1.len(), pf1, &r)
		case idx1 != noIndex:
			pf1Node := ft.np.node(idx1)
			r.tailRefs = append(r.tailRefs, ft.tailRefFromNodeAndPrefix(pf1Node, pf1))
		}

		pf0 := preFirst0(x)
		idx2, found := ft.followPrefix(ft.root, pf0)
		switch {
		case found:
			ft.aggregateLeft(idx2, load64(x)<<pf0.len(), pf0, &r)
		case idx2 != noIndex:
			pf0Node := ft.np.node(idx2)
			r.tailRefs = append(r.tailRefs, ft.tailRefFromNodeAndPrefix(pf0Node, pf0))
		}
	}
	return r
}

func (ft *fpTree) dumpNode(w io.Writer, idx nodeIndex, indent, dir string) {
	if idx == noIndex {
		return
	}
	node := ft.np.node(idx)
	var countStr string
	leaf := node.leaf()
	if leaf {
		countStr = "LEAF"
	} else {
		countStr = strconv.Itoa(int(node.c))
	}
	fmt.Fprintf(w, "%s%sidx=%d %s %s\n", indent, dir, idx, node.fp, countStr)
	if !leaf {
		indent += "  "
		ft.dumpNode(w, node.left, indent, "l: ")
		ft.dumpNode(w, node.right, indent, "r: ")
	}
}

func (ft *fpTree) dump(w io.Writer) {
	if ft.root == noIndex {
		fmt.Fprintln(w, "empty tree")
	} else {
		ft.dumpNode(w, ft.root, "", "")
	}
}

type inMemFPTree struct {
	tree *fpTree
	ids  [][][]byte
}

type fpResult struct {
	fp    fingerprint
	count uint32
}

func newInMemFPTree(np *nodePool, maxDepth int) *inMemFPTree {
	if maxDepth == 0 {
		panic("BUG: can't use newInMemFPTree with zero maxDepth")
	}
	return &inMemFPTree{
		tree: newFPTree(np, maxDepth),
		ids:  make([][][]byte, 1<<maxDepth),
	}
}

func (mft *inMemFPTree) addHash(h []byte) {
	mft.tree.addHash(h)
	idx := load64(h) >> (64 - mft.tree.maxDepth)
	s := mft.ids[idx]
	n := slices.IndexFunc(s, func(cur []byte) bool {
		return bytes.Compare(cur, h) > 0
	})
	if n < 0 {
		mft.ids[idx] = append(s, h)
	} else {
		mft.ids[idx] = slices.Insert(s, n, h)
	}
}

func (mft *inMemFPTree) aggregateInterval(x, y []byte) fpResult {
	r := mft.tree.aggregateInterval(x, y)
	for _, t := range r.tailRefs {
		ids := mft.ids[t]
		for _, id := range ids {
			// FIXME: this can be optimized as the IDs are ordered
			if idWithinInterval(id, x, y, r.itype) {
				// fmt.Fprintf(os.Stderr, "QQQQQ: including tail: %s\n", hex.EncodeToString(id))
				r.fp.update(id)
				r.count++
			} else {
				// fmt.Fprintf(os.Stderr, "QQQQQ: NOT including tail: %s\n", hex.EncodeToString(id))
			}
		}
	}
	return fpResult{fp: r.fp, count: r.count}
}

func idWithinInterval(id, x, y []byte, itype int) bool {
	switch itype {
	case 0:
		return true
	case -1:
		return bytes.Compare(id, x) >= 0 && bytes.Compare(id, y) < 0
	default:
		return bytes.Compare(id, y) < 0 || bytes.Compare(id, x) >= 0
	}
}

// TBD: perhaps use json-based SELECTs
// TBD: extra cache for after-24bit entries
// TBD: benchmark 24-bit limit (not going beyond the cache)
// TBD: optimize, get rid of binary.BigEndian.*
