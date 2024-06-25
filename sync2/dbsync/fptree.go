package dbsync

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"math/bits"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/spacemeshos/go-spacemesh/sql"
)

type trace struct {
	traceEnabled bool
	traceStack   []string
}

func (t *trace) out(msg string) {
	fmt.Fprintf(os.Stderr, "TRACE: %s%s\n", strings.Repeat("  ", len(t.traceStack)), msg)
}

func (t *trace) enter(format string, args ...any) {
	if !t.traceEnabled {
		return
	}
	msg := fmt.Sprintf(format, args...)
	t.out("ENTER: " + msg)
	t.traceStack = append(t.traceStack, msg)
}

func (t *trace) leave(results ...any) {
	if !t.traceEnabled {
		return
	}
	if len(t.traceStack) == 0 {
		panic("BUG: trace stack underflow")
	}
	msg := t.traceStack[len(t.traceStack)-1]
	if len(results) != 0 {
		var r []string
		for _, res := range results {
			r = append(r, fmt.Sprint(res))
		}
		msg += " => " + strings.Join(r, ", ")
	}
	t.traceStack = t.traceStack[:len(t.traceStack)-1]
	t.out("LEAVE: " + msg)
}

func (t *trace) log(format string, args ...any) {
	if t.traceEnabled {
		msg := fmt.Sprintf(format, args...)
		t.out(msg)
	}
}

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

type nodeIndex uint32

const noIndex = ^nodeIndex(0)

type nodePool struct {
	rcPool[node, nodeIndex]
}

func (np *nodePool) add(fp fingerprint, c uint32, left, right nodeIndex) nodeIndex {
	// panic("TBD: this is invalid, adds unneeded refs")
	// if left != noIndex {
	// 	np.rcPool.ref(left)
	// }
	// if right != noIndex {
	// 	np.rcPool.ref(right)
	// }
	idx := np.rcPool.add(node{fp: fp, c: c, left: left, right: right})
	// fmt.Fprintf(os.Stderr, "QQQQQ: add: idx %d fp %s c %d left %d right %d\n", idx, fp, c, left, right)
	return idx
}

func (np *nodePool) ref(idx nodeIndex) { // TBD: QQQQ: rmme
	// fmt.Fprintf(os.Stderr, "QQQQQ: ref: idx %d\n", idx)
	np.rcPool.ref(idx)
}

func (np *nodePool) release(idx nodeIndex) bool { // TBD: QQQQ: rmme
	r := np.rcPool.release(idx)
	// fmt.Fprintf(os.Stderr, "QQQQQ: release: idx %d: %v\n", idx, r)
	return r
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

const (
	prefixLenBits = 6
	prefixLenMask = 1<<prefixLenBits - 1
	prefixBitMask = ^uint64(prefixLenMask)
	maxPrefixLen  = 64 - prefixLenBits
)

type prefix uint64

func mkprefix(bits uint64, l int) prefix {
	return prefix(bits<<prefixLenBits + uint64(l))
}

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

func (p prefix) lowBit() bool {
	return p&(1<<prefixLenBits) != 0
}

func (p prefix) minID(b KeyBytes) {
	if len(b) < 8 {
		panic("BUG: id slice too small")
	}
	v := p.bits() << (64 - p.len())
	binary.BigEndian.PutUint64(b, v)
	for n := 8; n < len(b); n++ {
		b[n] = 0
	}
}

func (p prefix) maxID(b KeyBytes) {
	if len(b) < 8 {
		panic("BUG: id slice too small")
	}
	s := uint64(64 - p.len())
	v := (p.bits() << s) | ((1 << s) - 1)
	binary.BigEndian.PutUint64(b, v)
	for n := 8; n < len(b); n++ {
		b[n] = 0xff
	}
}

// shift removes the highest bit from the prefix
func (p prefix) shift() prefix {
	switch l := p.len(); l {
	case 0:
		panic("BUG: can't shift zero prefix")
	case 1:
		return 0
	default:
		l--
		return mkprefix(p.bits()&((1<<l)-1), l)
	}
}

func load64(h KeyBytes) uint64 {
	return binary.BigEndian.Uint64(h[:8])
}

// func hashPrefix(h KeyBytes, nbits int) prefix {
// 	if nbits < 0 || nbits > maxPrefixLen {
// 		panic("BUG: bad prefix length")
// 	}
// 	if nbits == 0 {
// 		return 0
// 	}
// 	v := load64(h)
// 	return prefix((v>>(64-nbits-prefixLenBits))&prefixBitMask + uint64(nbits))
// }

func preFirst0(h KeyBytes) prefix {
	l := min(maxPrefixLen, bits.LeadingZeros64(^load64(h)))
	return mkprefix((1<<l)-1, l)
}

func preFirst1(h KeyBytes) prefix {
	return prefix(min(maxPrefixLen, bits.LeadingZeros64(load64(h))))
}

func commonPrefix(a, b KeyBytes) prefix {
	v1 := load64(a)
	v2 := load64(b)
	l := min(maxPrefixLen, bits.LeadingZeros64(v1^v2))
	return mkprefix(v1>>(64-l), l)
}

type fpResult struct {
	fp    fingerprint
	count uint32
	itype int
}

type tailRef struct {
	// node from which this tailRef has been derived
	idx nodeIndex
	// maxDepth bits of the key
	ref uint64
	// max count to get from this tail ref, -1 for unlimited
	limit int
}

type aggResult struct {
	tailRefs    []tailRef
	fp          fingerprint
	count       uint32
	itype       int
	limit       int
	lastVisited nodeIndex
	lastPrefix  prefix
}

func (r *aggResult) takeAtMost(count int) int {
	switch {
	case r.limit < 0:
		return -1
	case count <= r.limit:
		r.limit -= count
	default:
		count = r.limit
		r.limit = 0
	}
	return count
}

func (r *aggResult) update(node node) {
	r.fp.update(node.fp[:])
	r.count += node.c
	// // fmt.Fprintf(os.Stderr, "QQQQQ: r.count <= %d r.fp <= %s\n", r.count, r.fp)
}

type idStore interface {
	clone() idStore
	registerHash(h KeyBytes) error
	iterateIDs(tailRefs []tailRef, toCall func(tailRef, KeyBytes) bool) error
}

type fpTree struct {
	trace // rmme
	idStore
	np       *nodePool
	rootMtx  sync.Mutex
	root     nodeIndex
	maxDepth int
}

func newFPTree(np *nodePool, idStore idStore, maxDepth int) *fpTree {
	ft := &fpTree{np: np, idStore: idStore, root: noIndex, maxDepth: maxDepth}
	runtime.SetFinalizer(ft, (*fpTree).release)
	return ft
}

func (ft *fpTree) releaseNode(idx nodeIndex) {
	if idx == noIndex {
		return
	}
	node := ft.np.node(idx)
	if ft.np.release(idx) {
		// fmt.Fprintf(os.Stderr, "QQQQQ: releaseNode: freed %d, release l %d r %d\n", idx, node.left, node.right)
		ft.releaseNode(node.left)
		ft.releaseNode(node.right)
	} else {
		// fmt.Fprintf(os.Stderr, "QQQQQ: releaseNode: keep %d\n", idx)
	}
}

func (ft *fpTree) release() {
	ft.rootMtx.Lock()
	defer ft.rootMtx.Unlock()
	ft.releaseNode(ft.root)
	ft.root = noIndex
}

func (ft *fpTree) clone() *fpTree {
	ft.rootMtx.Lock()
	defer ft.rootMtx.Unlock()
	if ft.root != noIndex {
		ft.np.ref(ft.root)
	}
	return &fpTree{
		np:       ft.np,
		idStore:  ft.idStore.clone(),
		root:     ft.root,
		maxDepth: ft.maxDepth,
	}
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
			r := ft.np.add(fpCombined, 2, noIndex, childIdx)
			// fmt.Fprintf(os.Stderr, "QQQQQ: pushDown: sameDir: left => %d\n", r)
			return r
		} else {
			r := ft.np.add(fpCombined, 2, childIdx, noIndex)
			// fmt.Fprintf(os.Stderr, "QQQQQ: pushDown: sameDir: right => %d\n", r)
			return r
		}
	}

	idxA := ft.np.add(fpA, 1, noIndex, noIndex)
	idxB := ft.np.add(fpB, curCount, noIndex, noIndex)
	if dirA {
		r := ft.np.add(fpCombined, 2, idxB, idxA)
		// fmt.Fprintf(os.Stderr, "QQQQQ: pushDown: add A-B => %d\n", r)
		return r
	} else {
		r := ft.np.add(fpCombined, 2, idxA, idxB)
		// fmt.Fprintf(os.Stderr, "QQQQQ: pushDown: add B-A => %d\n", r)
		return r
	}
}

func (ft *fpTree) addValue(fp fingerprint, p prefix, idx nodeIndex) nodeIndex {
	if idx == noIndex {
		r := ft.np.add(fp, 1, noIndex, noIndex)
		// fmt.Fprintf(os.Stderr, "QQQQQ: addValue: addNew fp %s p %s => %d\n", fp.String(), p.String(), r)
		return r
	}
	node := ft.np.node(idx)
	// defer ft.releaseNode(idx)
	if node.c == 1 || (ft.maxDepth != 0 && p.len() == ft.maxDepth) {
		// we're at a leaf node, need to push down the old fingerprint, or,
		// if we've reached the max depth, just update the current node
		r := ft.pushDown(fp, node.fp, p, node.c)
		// fmt.Fprintf(os.Stderr, "QQQQQ: addValue: pushDown fp %s p %s oldIdx %d => %d\n", fp.String(), p.String(), idx, r)
		return r
	}
	fpCombined := fp
	fpCombined.update(node.fp[:])
	if fp.bitFromLeft(p.len()) {
		// fmt.Fprintf(os.Stderr, "QQQQQ: addValue: replaceRight fp %s p %s oldIdx %d\n", fp.String(), p.String(), idx)
		if node.left != noIndex {
			ft.np.ref(node.left)
			// fmt.Fprintf(os.Stderr, "QQQQQ: addValue: ref left %d -- refCount %d\n", node.left, ft.np.entry(node.left).refCount)
		}
		newRight := ft.addValue(fp, p.right(), node.right)
		r := ft.np.add(fpCombined, node.c+1, node.left, newRight)
		// fmt.Fprintf(os.Stderr, "QQQQQ: addValue: replaceRight fp %s p %s oldIdx %d => %d node.left %d newRight %d\n", fp.String(), p.String(), idx, r, node.left, newRight)
		return r
	} else {
		// fmt.Fprintf(os.Stderr, "QQQQQ: addValue: replaceLeft fp %s p %s oldIdx %d\n", fp.String(), p.String(), idx)
		if node.right != noIndex {
			// fmt.Fprintf(os.Stderr, "QQQQQ: addValue: ref right %d -- refCount %d\n", node.right, ft.np.entry(node.right).refCount)
			ft.np.ref(node.right)
		}
		newLeft := ft.addValue(fp, p.left(), node.left)
		r := ft.np.add(fpCombined, node.c+1, newLeft, node.right)
		// fmt.Fprintf(os.Stderr, "QQQQQ: addValue: replaceLeft fp %s p %s oldIdx %d => %d newLeft %d node.right %d\n", fp.String(), p.String(), idx, r, newLeft, node.right)
		return r
	}
}

func (ft *fpTree) addHash(h KeyBytes) error {
	// fmt.Fprintf(os.Stderr, "QQQQQ: addHash: %s\n", hex.EncodeToString(h))
	var fp fingerprint
	fp.update(h)
	ft.rootMtx.Lock()
	defer ft.rootMtx.Unlock()
	oldRoot := ft.root
	ft.root = ft.addValue(fp, 0, ft.root)
	ft.releaseNode(oldRoot)
	// fmt.Fprintf(os.Stderr, "QQQQQ: addHash: new root %d\n", ft.root)
	return ft.idStore.registerHash(h)
}

func (ft *fpTree) followPrefix(from nodeIndex, p, followed prefix) (idx nodeIndex, rp prefix, found bool) {
	ft.enter("followPrefix: from %d p %s highBit %v", from, p, p.highBit())
	defer func() { ft.leave(idx, rp, found) }()

	switch {
	case from == noIndex:
		return noIndex, followed, false
	case p == 0:
		return from, followed, true
	case ft.np.node(from).leaf():
		return from, followed, false
	case p.highBit():
		return ft.followPrefix(ft.np.node(from).right, p.shift(), followed.right())
	default:
		return ft.followPrefix(ft.np.node(from).left, p.shift(), followed.left())
	}
}

func (ft *fpTree) tailRefFromPrefix(idx nodeIndex, p prefix, limit int) tailRef {
	// TODO: QQQQ: FIXME: this may happen with reverse intervals,
	// but should we even be checking the prefixes in this case?
	if p.len() != ft.maxDepth {
		panic("BUG: tail from short prefix")
	}
	return tailRef{idx: idx, ref: p.bits(), limit: limit}
}

func (ft *fpTree) tailRefFromFingerprint(idx nodeIndex, fp fingerprint, limit int) tailRef {
	v := load64(fp[:])
	if ft.maxDepth >= 64 {
		return tailRef{idx: idx, ref: v, limit: limit}
	}
	// // fmt.Fprintf(os.Stderr, "QQQQQ: AAAAA: v %016x maxDepth %d shift %d\n", v, ft.maxDepth, (64 - ft.maxDepth))
	return tailRef{idx: idx, ref: v >> (64 - ft.maxDepth), limit: limit}
}

func (ft *fpTree) tailRefFromNodeAndPrefix(idx nodeIndex, n node, p prefix, limit int) tailRef {
	if n.c == 1 {
		return ft.tailRefFromFingerprint(idx, n.fp, limit)
	} else {
		return ft.tailRefFromPrefix(idx, p, limit)
	}
}

func (ft *fpTree) descendToLeftmostLeaf(idx nodeIndex, p prefix) (nodeIndex, prefix) {
	switch {
	case idx == noIndex:
		return noIndex, p
	case ft.np.node(idx).leaf():
		return idx, p
	default:
		return ft.descendToLeftmostLeaf(ft.np.node(idx).left, p.left())
	}
}

// func (ft *fpTree) descendToNextLeaf(idx nodeIndex, p, rem prefix) (nodeIndex, prefix) {
// 	switch {
// 	case idx == noIndex:
// 		panic("BUG: descendToNextLeaf: no node")
// 	case rem == 0:
// 		return noIndex, p
// 	case rem.highBit():
// 		// Descending to the right branch by following p:
// 		// the next leaf, if there's any, is further down the right branch.
// 		newIdx, newP := ft.descendToNextLeaf(ft.np.node(idx).right, p.right(), rem.shift())
// 		return newIdx, newP
// 	default:
// 		// Descending to the left branch by following p:
// 		// if the leaf is not found in the left branch, it's the leftmost leaf
// 		// on the right branch
// 		newIdx, newP := ft.descendToNextLeaf(ft.np.node(idx).left, p.left(), rem.shift())
// 		if newIdx != noIndex {
// 			return newIdx, newP
// 		}
// 		return ft.descendToLeftmostLeaf(ft.np.node(idx).right, p.right())
// 	}
// }

// func (ft *fpTree) nextLeaf(p prefix) (nodeIndex, prefix) {
// 	if ft.root == noIndex {
// 		return noIndex, 0
// 	}
// 	return ft.descendToNextLeaf(ft.root, 0, p)
// }

func (ft *fpTree) visitNode(idx nodeIndex, p prefix, r *aggResult) (node, bool) {
	if idx == noIndex {
		return node{}, false
	}
	ft.log("visitNode: idx %d p %s", idx, p)
	r.lastVisited = idx
	r.lastPrefix = p
	return ft.np.node(idx), true
}

func (ft *fpTree) aggregateUpToLimit(idx nodeIndex, p prefix, r *aggResult) {
	ft.enter("aggregateUpToLimit: idx %d p %s limit %d cur_fp %s cur_count %d", idx, p, r.limit,
		r.fp.String(), r.count)
	defer func() {
		ft.leave(r.fp, r.count)
	}()
	node, ok := ft.visitNode(idx, p, r)
	switch {
	case !ok || r.limit == 0:
		// for r.limit == 0, it's important that we still visit the node
		// so that we can get the item immediately following the included items
		ft.log("stop: ok %v r.limit %d", ok, r.limit)
	case r.limit < 0:
		// no limit
		ft.log("no limit")
		r.update(node)
	case node.c <= uint32(r.limit):
		// node is fully included
		ft.log("included fully")
		r.update(node)
		r.limit -= int(node.c)
	case node.leaf():
		tail := ft.tailRefFromPrefix(idx, p, r.takeAtMost(int(node.c)))
		r.tailRefs = append(r.tailRefs, tail)
		ft.log("add prefix to the tails: %016x => limit %d", tail.ref, r.limit)
	default:
		pLeft := p.left()
		left, haveLeft := ft.visitNode(node.left, pLeft, r)
		if haveLeft {
			if int(left.c) <= r.limit {
				// left node is fully included
				ft.log("include left in full")
				r.update(left)
				r.limit -= int(left.c)
			} else {
				// we must stop somewhere in the left subtree
				ft.log("descend to the left")
				ft.aggregateUpToLimit(node.left, pLeft, r)
				return
			}
		}
		ft.log("descend to the right")
		ft.aggregateUpToLimit(node.right, p.right(), r)
	}
}

func (ft *fpTree) aggregateLeft(idx nodeIndex, v uint64, p prefix, r *aggResult) {
	ft.enter("aggregateLeft: idx %d v %016x p %s limit %d", idx, v, p, r.limit)
	defer func() {
		ft.leave(r.fp, r.count, r.tailRefs)
	}()
	node, ok := ft.visitNode(idx, p, r)
	switch {
	case !ok || r.limit == 0:
		// for r.limit == 0, it's important that we still visit the node
		// so that we can get the item immediately following the included items
		ft.log("stop: ok %v r.limit %d", ok, r.limit)
	case p.len() == ft.maxDepth:
		if node.left != noIndex || node.right != noIndex {
			panic("BUG: node @ maxDepth has children")
		}
		tail := ft.tailRefFromPrefix(idx, p, r.takeAtMost(int(node.c)))
		r.tailRefs = append(r.tailRefs, tail)
		ft.log("add prefix to the tails: %016x => limit %d", tail.ref, r.limit)
	case node.leaf(): // TBD: combine with prev
		// For leaf 1-nodes, we can use the fingerprint to get tailRef
		// by which the actual IDs will be selected
		if node.c != 1 {
			panic("BUG: leaf non-1 node below maxDepth")
		}
		tail := ft.tailRefFromFingerprint(idx, node.fp, r.takeAtMost(1))
		r.tailRefs = append(r.tailRefs, tail)
		ft.log("add prefix to the tails (1-leaf): %016x (fp %s) => limit %d", tail.ref, node.fp, r.limit)
	case v&bit63 == 0:
		ft.log("incl right node %d + go left to node %d", node.right, node.left)
		if node.right != noIndex {
			ft.aggregateUpToLimit(node.right, p.right(), r)
		}
		ft.aggregateLeft(node.left, v<<1, p.left(), r)
	default:
		ft.log("go right to node %d", node.right)
		ft.aggregateLeft(node.right, v<<1, p.right(), r)
	}
}

func (ft *fpTree) aggregateRight(idx nodeIndex, v uint64, p prefix, r *aggResult) {
	ft.enter("aggregateRight: idx %d v %016x p %s limit %d", idx, v, p, r.limit)
	defer func() {
		ft.leave(r.fp, r.count, r.tailRefs)
	}()
	node, ok := ft.visitNode(idx, p, r)
	switch {
	case !ok || r.limit == 0:
		// for r.limit == 0, it's important that we still visit the node
		// so that we can get the item immediately following the included items
		ft.log("stop: ok %v r.limit %d", ok, r.limit)
	case p.len() == ft.maxDepth:
		if node.left != noIndex || node.right != noIndex {
			panic("BUG: node @ maxDepth has children")
		}
		tail := ft.tailRefFromPrefix(idx, p, r.takeAtMost(int(node.c)))
		r.tailRefs = append(r.tailRefs, tail)
		ft.log("add prefix to the tails: %016x => limit %d", tail.ref, r.limit)
	case node.leaf():
		// For leaf 1-nodes, we can use the fingerprint to get tailRef
		// by which the actual IDs will be selected
		if node.c != 1 {
			panic("BUG: leaf non-1 node below maxDepth")
		}
		tail := ft.tailRefFromFingerprint(idx, node.fp, r.takeAtMost(1))
		r.tailRefs = append(r.tailRefs, tail)
		ft.log("add prefix to the tails (1-leaf): %016x (fp %s) => limit %d", tail.ref, node.fp, r.limit)
	case v&bit63 == 0:
		ft.log("go left to node %d", node.left)
		ft.aggregateRight(node.left, v<<1, p.left(), r)
	default:
		ft.log("incl left node %d + go right to node %d", node.left, node.right)
		if node.left != noIndex {
			ft.aggregateUpToLimit(node.left, p.left(), r)
		}
		ft.aggregateRight(node.right, v<<1, p.right(), r)
	}
}

func (ft *fpTree) aggregateInterval(x, y KeyBytes, limit int) (r aggResult) {
	ft.rootMtx.Lock()
	defer ft.rootMtx.Unlock()
	ft.enter("aggregateInterval: x %s y %s limit %d", hex.EncodeToString(x), hex.EncodeToString(y), limit)
	defer func() {
		ft.leave(r)
	}()
	r = aggResult{limit: limit, lastVisited: noIndex}
	r.itype = bytes.Compare(x, y)
	switch {
	case r.itype == 0:
		// the whole set
		if ft.root != noIndex {
			ft.log("whole set")
			ft.aggregateUpToLimit(ft.root, 0, &r)
		} else {
			ft.log("empty set (no root)")
		}
	case r.itype < 0:
		// "proper" interval: [x; lca); (lca; y)
		p := commonPrefix(x, y)
		lcaIdx, followedPrefix, found := ft.followPrefix(ft.root, p, 0)
		var lcaNode node
		if lcaIdx != noIndex {
			lcaNode = ft.np.node(lcaIdx)
		}
		ft.log("commonPrefix %s lca %d found %v", p, lcaIdx, found)
		switch {
		case found && !lcaNode.leaf():
			if followedPrefix != p {
				panic("BUG: bad followedPrefix")
			}
			ft.visitNode(lcaIdx, followedPrefix, &r)
			ft.aggregateLeft(lcaNode.left, load64(x)<<(p.len()+1), p.left(), &r)
			ft.aggregateRight(lcaNode.right, load64(y)<<(p.len()+1), p.right(), &r)
		case lcaIdx != noIndex:
			ft.log("commonPrefix %s NOT found but have lca %d", p, lcaIdx)
			// Didn't reach LCA in the tree b/c ended up
			// at a leaf, just use the prefix to go
			// through the IDs
			if lcaNode.leaf() {
				ft.visitNode(lcaIdx, followedPrefix, &r)
				r.tailRefs = append(r.tailRefs,
					ft.tailRefFromNodeAndPrefix(
						lcaIdx, lcaNode, followedPrefix, r.takeAtMost(limit)))
			}
		}
	default:
		// inverse interval: [min; y); [x; max]
		pf0 := preFirst0(x)
		idx0, followedPrefix, found := ft.followPrefix(ft.root, pf0, 0)
		var pf0Node node
		if idx0 != noIndex {
			pf0Node = ft.np.node(idx0)
		}
		ft.log("pf0 %s idx0 %d found %v", pf0, idx0, found)
		switch {
		case found && !pf0Node.leaf():
			if followedPrefix != pf0 {
				panic("BUG: bad followedPrefix")
			}
			ft.aggregateLeft(idx0, load64(x)<<pf0.len(), pf0, &r)
		case idx0 != noIndex:
			if pf0Node.leaf() {
				ft.visitNode(idx0, followedPrefix, &r)
				rightLimit := r.takeAtMost(int(pf0Node.c))
				r.tailRefs = append(r.tailRefs, ft.tailRefFromNodeAndPrefix(idx0, pf0Node, followedPrefix, rightLimit))
			}
		}

		pf1 := preFirst1(y)
		idx1, followedPrefix, found := ft.followPrefix(ft.root, pf1, 0)
		var pf1Node node
		if idx1 != noIndex {
			pf1Node = ft.np.node(idx1)
		}
		ft.log("pf1 %s idx1 %d found %v", pf1, idx1, found)
		switch {
		case found && !pf1Node.leaf():
			if followedPrefix != pf1 {
				panic("BUG: bad followedPrefix")
			}
			ft.aggregateRight(idx1, load64(y)<<pf1.len(), pf1, &r)
		case idx1 != noIndex:
			if pf1Node.leaf() {
				ft.visitNode(idx1, followedPrefix, &r)
				leftLimit := r.takeAtMost(int(pf1Node.c))
				r.tailRefs = append(r.tailRefs, ft.tailRefFromNodeAndPrefix(idx1, pf1Node, followedPrefix, leftLimit))
			}
		}
	}
	return r
}

func (ft *fpTree) fingerprintInterval(x, y KeyBytes, limit int) (fpr fpResult, err error) {
	ft.enter("fingerprintInterval: x %s y %s limit %d", hex.EncodeToString(x), hex.EncodeToString(y), limit)
	defer func() {
		ft.leave(fpr, err)
	}()
	r := ft.aggregateInterval(x, y, limit)
	wasWithinRange := false
	ft.log("tailRefs: %#v count: %d", r.tailRefs, r.count)
	// Check for edge case: the fingerprinting has looped back to the same tail,
	// so we have the tail repeated twice and no other tails.
	// We can't hit any tails in between.
	noStop := false
	if len(r.tailRefs) == 2 && r.tailRefs[0].ref == r.tailRefs[1].ref {
		ft.log("edge case: tailRef loopback")
		r.tailRefs = r.tailRefs[:1]
		noStop = true
	}
	if err := ft.idStore.iterateIDs(r.tailRefs, func(tailRef tailRef, id KeyBytes) bool {
		if idWithinInterval(id, x, y, r.itype) {
			r.fp.update(id)
			r.count++
			ft.log("tailRef %v: id %s within range => fp %s count %d",
				tailRef,
				hex.EncodeToString(id),
				r.fp.String(), r.count)
			wasWithinRange = true
		} else {
			// if we were within the range but now we're out of it,
			// this means we're at or beyond y and can stop
			// return !wasWithinRange
			// QQQQQ: rmme
			if wasWithinRange {
				ft.log("tailRef %v: id %s outside range after id(s) within range => terminating",
					tailRef,
					hex.EncodeToString(id))
				// TBD: QQQQQ: terminate only for this tailRef
				return noStop
			} else {
				ft.log("tailRef %v: id %s outside range => continuing",
					tailRef,
					hex.EncodeToString(id))
				return true
			}
		}
		return true
	}); err != nil {
		return fpResult{}, err
	}
	return fpResult{fp: r.fp, count: r.count, itype: r.itype}, nil
}

func (ft *fpTree) dumpNode(w io.Writer, idx nodeIndex, indent, dir string) {
	if idx == noIndex {
		return
	}
	node := ft.np.node(idx)

	leaf := node.leaf()
	countStr := strconv.Itoa(int(node.c))
	if leaf {
		countStr = "LEAF-" + countStr
	}
	fmt.Fprintf(w, "%s%sidx=%d %s %s [%d]\n", indent, dir, idx, node.fp, countStr, ft.np.refCount(idx))
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

type memIDStore struct {
	mtx      sync.Mutex
	ids      map[uint64][]KeyBytes
	maxDepth int
}

var _ idStore = &memIDStore{}

func newMemIDStore(maxDepth int) *memIDStore {
	return &memIDStore{maxDepth: maxDepth}
}

func (m *memIDStore) clone() idStore {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	s := newMemIDStore(m.maxDepth)
	if m.ids != nil {
		s.ids = make(map[uint64][]KeyBytes, len(m.ids))
		for k, v := range m.ids {
			s.ids[k] = slices.Clone(v)
		}
	}
	return s
}

func (m *memIDStore) registerHash(h KeyBytes) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	if m.ids == nil {
		m.ids = make(map[uint64][]KeyBytes, 1<<m.maxDepth)
	}
	idx := load64(h) >> (64 - m.maxDepth)
	s := m.ids[idx]
	n := slices.IndexFunc(s, func(cur KeyBytes) bool {
		return bytes.Compare(cur, h) > 0
	})
	if n < 0 {
		m.ids[idx] = append(s, h)
	} else {
		m.ids[idx] = slices.Insert(s, n, h)
	}
	return nil
}

func (m *memIDStore) iterateIDs(tailRefs []tailRef, toCall func(tailRef, KeyBytes) bool) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	if m.ids == nil {
		return nil
	}
	// fmt.Fprintf(os.Stderr, "QQQQQ: memIDStore: iterateIDs: maxDepth %d tailRefs %v\n", m.maxDepth, tailRefs)
	// fmt.Fprintf(os.Stderr, "QQQQQ: memIDStore: iterateIDs: ids %#v\n", m.ids)
	for _, t := range tailRefs {
		count := t.limit
		if count == 0 {
			// fmt.Fprintf(os.Stderr, "QQQQQ: memIDStore: iterateIDs: t %v: count == 0\n", t)
			continue
		}
		for _, id := range m.ids[t.ref] {
			if count == 0 {
				// fmt.Fprintf(os.Stderr, "QQQQQ: memIDStore: iterateIDs: t %v id %s: count == 0\n", t, hex.EncodeToString(id))
				break
			}
			if count > 0 {
				// fmt.Fprintf(os.Stderr, "QQQQQ: memIDStore: iterateIDs: t %v id %s: dec count\n", t, hex.EncodeToString(id))
				count--
			}
			// fmt.Fprintf(os.Stderr, "QQQQQ: memIDStore: iterateIDs: t %v id %s: call\n", t, hex.EncodeToString(id))
			if !toCall(t, id) {
				// fmt.Fprintf(os.Stderr, "QQQQQ: memIDStore: iterateIDs: t %v id %s: stop\n", t, hex.EncodeToString(id))
				return nil
			}
		}
	}
	return nil
}

type sqlIDStore struct {
	db       sql.Database
	query    string
	keyLen   int
	maxDepth int
}

var _ idStore = &sqlIDStore{}

func newSQLIDStore(db sql.Database, query string, keyLen, maxDepth int) *sqlIDStore {
	return &sqlIDStore{db: db, query: query, keyLen: keyLen, maxDepth: maxDepth}
}

func (s *sqlIDStore) clone() idStore {
	return newSQLIDStore(s.db, s.query, s.keyLen, s.maxDepth)
}

func (s *sqlIDStore) registerHash(h KeyBytes) error {
	// should be registered by the handler code
	return nil
}

func (s *sqlIDStore) iterateIDs(tailRefs []tailRef, toCall func(tailRef, KeyBytes) bool) error {
	cont := true
	for _, t := range tailRefs {
		if t.limit == 0 {
			continue
		}
		p := mkprefix(t.ref, s.maxDepth)
		minID := make([]byte, s.keyLen)
		maxID := make([]byte, s.keyLen)
		p.minID(minID[:])
		p.maxID(maxID[:])
		// start := time.Now()
		query := s.query
		if t.limit > 0 {
			query += " LIMIT " + strconv.Itoa(t.limit)
		}
		if _, err := s.db.Exec(
			query,
			func(stmt *sql.Statement) {
				stmt.BindBytes(1, minID)
				stmt.BindBytes(2, maxID)
			},
			func(stmt *sql.Statement) bool {
				id := make(KeyBytes, s.keyLen)
				stmt.ColumnBytes(0, id)
				cont = toCall(t, id)
				return cont
			},
		); err != nil {
			return err
		}
		// fmt.Fprintf(os.Stderr, "QQQQQ: %v: sel atxs between %s and %s\n", time.Now().Sub(start), minID.String(), maxID.String())
		if !cont {
			break
		}
	}
	return nil
}

type dbBackedStore struct {
	*sqlIDStore
	*memIDStore
	maxDepth int
}

var _ idStore = &dbBackedStore{}

func newDBBackedStore(db sql.Database, query string, keyLen, maxDepth int) *dbBackedStore {
	return &dbBackedStore{
		sqlIDStore: newSQLIDStore(db, query, keyLen, maxDepth),
		memIDStore: newMemIDStore(maxDepth),
		maxDepth:   maxDepth,
	}
}

func (s *dbBackedStore) clone() idStore {
	return &dbBackedStore{
		sqlIDStore: s.sqlIDStore.clone().(*sqlIDStore),
		memIDStore: s.memIDStore.clone().(*memIDStore),
		maxDepth:   s.maxDepth,
	}
}

func (s *dbBackedStore) registerHash(h KeyBytes) error {
	return s.memIDStore.registerHash(h)
}

func (s *dbBackedStore) iterateIDs(tailRefs []tailRef, toCall func(tailRef, KeyBytes) bool) error {
	type memItem struct {
		tailRef tailRef
		id      KeyBytes
	}
	var memItems []memItem
	s.memIDStore.iterateIDs(tailRefs, func(tailRef tailRef, id KeyBytes) bool {
		memItems = append(memItems, memItem{tailRef: tailRef, id: id})
		return true
	})
	cont := true
	limits := make(map[uint64]int, len(tailRefs))
	for _, t := range tailRefs {
		if t.limit >= 0 {
			limits[t.ref] += t.limit
		}
	}
	if err := s.sqlIDStore.iterateIDs(tailRefs, func(tailRef tailRef, id KeyBytes) bool {
		ref := load64(id) >> (64 - s.maxDepth)
		limit, haveLimit := limits[ref]
		for len(memItems) > 0 && bytes.Compare(memItems[0].id, id) < 0 {
			if haveLimit && limit == 0 {
				return false
			}
			cont = toCall(memItems[0].tailRef, memItems[0].id)
			if !cont {
				return false
			}
			limits[ref] = limit - 1
			memItems = memItems[1:]
		}
		if haveLimit && limit == 0 {
			return false
		}
		cont = toCall(tailRef, id)
		limits[ref] = limit - 1
		return cont
	}); err != nil {
		return err
	}
	if cont {
		for _, mi := range memItems {
			ref := load64(mi.id) >> (64 - s.maxDepth)
			limit, haveLimit := limits[ref]
			if haveLimit && limit == 0 {
				break
			}
			if !toCall(mi.tailRef, mi.id) {
				break
			}
			limits[ref] = limit - 1
		}
	}
	return nil
}

func idWithinInterval(id, x, y KeyBytes, itype int) bool {
	switch itype {
	case 0:
		return true
	case -1:
		return bytes.Compare(id, x) >= 0 && bytes.Compare(id, y) < 0
	default:
		return bytes.Compare(id, y) < 0 || bytes.Compare(id, x) >= 0
	}
}

// TBD: optimize, get rid of binary.BigEndian.*
