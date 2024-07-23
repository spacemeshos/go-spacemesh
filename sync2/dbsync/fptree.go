package dbsync

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/spacemeshos/go-spacemesh/sync2/hashsync"
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

// QQQQQ: rm ?
// func (p prefix) maxID(b KeyBytes) {
// 	if len(b) < 8 {
// 		panic("BUG: id slice too small")
// 	}
// 	s := uint64(64 - p.len())
// 	v := (p.bits() << s) | ((1 << s) - 1)
// 	binary.BigEndian.PutUint64(b, v)
// 	for n := 8; n < len(b); n++ {
// 		b[n] = 0xff
// 	}
// }

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

func (p prefix) match(b KeyBytes) bool {
	return load64(b)>>(64-p.len()) == p.bits()
}

func load64(h KeyBytes) uint64 {
	return binary.BigEndian.Uint64(h[:8])
}

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

type aggContext struct {
	x, y  KeyBytes
	fp    fingerprint
	count uint32
	itype int
	limit int
	total uint32
}

func (ac *aggContext) prefixWithinRange(p prefix) bool {
	if ac.itype == 0 {
		return true
	}
	x := load64(ac.x)
	y := load64(ac.y)
	v := p.bits() << (64 - p.len())
	maxV := v + (1 << (64 - p.len())) - 1
	if ac.itype < 0 {
		// normal interval
		// fmt.Fprintf(os.Stderr, "QQQQQ: (0) itype %d x %016x y %016x v %016x maxV %016x result %v\n", ac.itype, x, y, v, maxV, v >= x && maxV < y)
		return v >= x && maxV < y
	}
	// inverted interval
	// fmt.Fprintf(os.Stderr, "QQQQQ: (1) itype %d x %016x y %016x v %016x maxV %016x result %v\n", ac.itype, x, y, v, maxV, v >= x || v < y)
	return v >= x || maxV < y
}

func (ac *aggContext) maybeIncludeNode(node node) bool {
	switch {
	case ac.limit < 0:
	case uint32(ac.limit) < node.c:
		return false
	default:
		ac.limit -= int(node.c)
	}
	ac.fp.update(node.fp[:])
	ac.count += node.c
	return true
}

type iterator interface {
	hashsync.Iterator
	clone() iterator
}

type idStore interface {
	clone() idStore
	registerHash(h KeyBytes) error
	iter(from KeyBytes) (iterator, error)
}

type fpTree struct {
	trace // rmme
	idStore
	np       *nodePool
	rootMtx  sync.Mutex
	root     nodeIndex
	keyLen   int
	maxDepth int
}

func newFPTree(np *nodePool, idStore idStore, keyLen, maxDepth int) *fpTree {
	ft := &fpTree{
		np:       np,
		idStore:  idStore,
		root:     noIndex,
		keyLen:   keyLen,
		maxDepth: maxDepth,
	}
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

	// QQQQQ: refactor into a loop
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

// aggregateEdge aggregates an edge of the interval, which can be bounded by x, y, both x
// and y or none of x and y, have a common prefix and optionally bounded by a limit of N of
// aggregated items.
// It returns a boolean indicating whether the limit or the right edge (y) was reached and
// an error, if any.
func (ft *fpTree) aggregateEdge(x, y KeyBytes, p prefix, ac *aggContext) (cont bool, err error) {
	ft.log("aggregateEdge: x %s y %s p %s limit %d count %d", x.String(), y.String(), p, ac.limit, ac.count)
	defer func() {
		ft.log("aggregateEdge ==> limit %d count %d\n", ac.limit, ac.count)
	}()
	if ac.limit == 0 {
		ft.log("aggregateEdge: limit is 0")
		return false, nil
	}
	var startFrom KeyBytes
	if x == nil {
		startFrom = make(KeyBytes, ft.keyLen)
		p.minID(startFrom)
	} else {
		startFrom = x
	}
	ft.log("aggregateEdge: startFrom %s", startFrom.String())
	it, err := ft.iter(startFrom)
	if err != nil {
		if errors.Is(err, errEmptySet) {
			ft.log("aggregateEdge: empty set")
			return false, nil
		}
		ft.log("aggregateEdge: error: %v", err)
		return false, err
	}

	for range ft.np.node(ft.root).c {
		id := it.Key().(KeyBytes)
		ft.log("aggregateEdge: ID %s", id.String())
		if y != nil && id.Compare(y) >= 0 {
			ft.log("aggregateEdge: ID is over Y: %s", id.String())
			return false, nil
		}
		if !p.match(id) {
			ft.log("aggregateEdge: ID doesn't match the prefix: %s", id.String())
			// Got to the end of the tailRef without exhausting the limit
			return true, nil
		}
		ac.fp.update(id)
		ac.count++
		if ac.limit > 0 {
			ac.limit--
			if ac.limit == 0 {
				ft.log("aggregateEdge: limit exhausted")
				return false, nil
			}
		}
		if err := it.Next(); err != nil {
			ft.log("aggregateEdge: next error: %v", err)
			return false, err
		}
	}

	return true, nil
}

func (ft *fpTree) node(idx nodeIndex) (node, bool) {
	if idx == noIndex {
		return node{}, false
	}
	return ft.np.node(idx), true
}

func (ft *fpTree) aggregateUpToLimit(idx nodeIndex, p prefix, ac *aggContext) (cont bool, err error) {
	ft.enter("aggregateUpToLimit: idx %d p %s limit %d cur_fp %s cur_count %d", idx, p, ac.limit,
		ac.fp.String(), ac.count)
	defer func() {
		ft.leave(ac.fp, ac.count)
	}()
	for {
		node, ok := ft.node(idx)
		switch {
		case !ok:
			ft.log("stop: no node")
			return true, nil
		case ac.limit == 0:
			// for ac.limit == 0, it's important that we still visit the node
			// so that we can get the item immediately following the included items
			ft.log("stop: limit exhausted")
			return false, nil
		case ac.maybeIncludeNode(node):
			// node is fully included
			ft.log("included fully")
			return true, nil
		case node.leaf():
			// reached the limit on this node, do not need to continue after
			// done with it
			cont, err := ft.aggregateEdge(nil, nil, p, ac)
			if err != nil {
				return false, err
			}
			if cont {
				panic("BUG: expected limit not reached")
			}
			return false, nil
		default:
			pLeft := p.left()
			left, haveLeft := ft.node(node.left)
			if haveLeft {
				if ac.maybeIncludeNode(left) {
					// left node is fully included, after which
					// we need to stop somewhere in the right subtree
					ft.log("include left in full")
				} else {
					// we must stop somewhere in the left subtree,
					// and the right subtree is irrelevant
					ft.log("descend to the left")
					idx = node.left
					p = pLeft
					continue
				}
			}
			ft.log("descend to the right")
			idx = node.right
			p = p.right()
		}
	}
}

func (ft *fpTree) aggregateLeft(idx nodeIndex, v uint64, p prefix, ac *aggContext) (cont bool, err error) {
	ft.enter("aggregateLeft: idx %d v %016x p %s limit %d", idx, v, p, ac.limit)
	defer func() {
		ft.leave(ac.fp, ac.count)
	}()
	node, ok := ft.node(idx)
	switch {
	case !ok:
		// for ac.limit == 0, it's important that we still visit the node
		// so that we can get the item immediately following the included items
		ft.log("stop: no node")
		return true, nil
	case ac.limit == 0:
		ft.log("stop: limit exhausted")
		return false, nil
	case ac.prefixWithinRange(p) && ac.maybeIncludeNode(node):
		ft.log("including node in full: %s limit %d", p, ac.limit)
		return ac.limit != 0, nil
	case p.len() == ft.maxDepth || node.leaf():
		if node.left != noIndex || node.right != noIndex {
			panic("BUG: node @ maxDepth has children")
		}
		return ft.aggregateEdge(ac.x, nil, p, ac)
	case v&bit63 == 0:
		ft.log("incl right node %d + go left to node %d", node.right, node.left)
		cont, err := ft.aggregateLeft(node.left, v<<1, p.left(), ac)
		if !cont || err != nil {
			return false, err
		}
		if node.right != noIndex {
			return ft.aggregateUpToLimit(node.right, p.right(), ac)
		}
		return true, nil
	default:
		ft.log("go right to node %d", node.right)
		return ft.aggregateLeft(node.right, v<<1, p.right(), ac)
	}
}

func (ft *fpTree) aggregateRight(idx nodeIndex, v uint64, p prefix, ac *aggContext) (cont bool, err error) {
	ft.enter("aggregateRight: idx %d v %016x p %s limit %d", idx, v, p, ac.limit)
	defer func() {
		ft.leave(ac.fp, ac.count)
	}()
	node, ok := ft.node(idx)
	switch {
	case !ok:
		// for ac.limit == 0, it's important that we still visit the node
		// so that we can get the item immediately following the included items
		ft.log("stop: no node")
		return true, nil
	case ac.limit == 0:
		ft.log("stop: limit exhausted")
		return false, nil
	case ac.prefixWithinRange(p) && ac.maybeIncludeNode(node):
		ft.log("including node in full: %s limit %d", p, ac.limit)
		return ac.limit != 0, nil
	case p.len() == ft.maxDepth || node.leaf():
		if node.left != noIndex || node.right != noIndex {
			panic("BUG: node @ maxDepth has children")
		}
		return ft.aggregateEdge(nil, ac.y, p, ac)
	case v&bit63 == 0:
		ft.log("go left to node %d", node.left)
		return ft.aggregateRight(node.left, v<<1, p.left(), ac)
	default:
		ft.log("incl left node %d + go right to node %d", node.left, node.right)
		if node.left != noIndex {
			cont, err := ft.aggregateUpToLimit(node.left, p.left(), ac)
			if !cont || err != nil {
				return false, err
			}
		}
		return ft.aggregateRight(node.right, v<<1, p.right(), ac)
	}
}

func (ft *fpTree) aggregateInterval(ac *aggContext) error {
	ft.rootMtx.Lock()
	defer ft.rootMtx.Unlock()
	ft.enter("aggregateInterval: x %s y %s limit %d", ac.x.String(), ac.y.String(), ac.limit)
	defer func() {
		ft.leave(ac)
	}()
	if ft.root == noIndex {
		return nil
	}
	ac.total = ft.np.node(ft.root).c
	ac.itype = bytes.Compare(ac.x, ac.y)
	switch {
	case ac.itype == 0:
		// the whole set
		if ft.root != noIndex {
			ft.log("whole set")
			_, err := ft.aggregateUpToLimit(ft.root, 0, ac)
			return err
		} else {
			ft.log("empty set (no root)")
		}

	case ac.itype < 0:
		// "proper" interval: [x; lca); (lca; y)
		p := commonPrefix(ac.x, ac.y)
		lcaIdx, lcaPrefix, fullPrefixFound := ft.followPrefix(ft.root, p, 0)
		var lca node
		if lcaIdx != noIndex {
			// QQQQQ: TBD: perhaps just return if lcaIdx == noIndex
			lca = ft.np.node(lcaIdx)
		}
		ft.log("commonPrefix %s lca %d found %v", p, lcaIdx, fullPrefixFound)
		switch {
		case fullPrefixFound && !lca.leaf():
			if lcaPrefix != p {
				panic("BUG: bad followedPrefix")
			}
			ft.aggregateLeft(lca.left, load64(ac.x)<<(p.len()+1), p.left(), ac)
			ft.aggregateRight(lca.right, load64(ac.y)<<(p.len()+1), p.right(), ac)
		case lcaIdx == noIndex || !lca.leaf():
			ft.log("commonPrefix %s NOT found b/c no items have it", p)
		default:
			ft.log("commonPrefix %s -- lca %d", p, lcaIdx)
			_, err := ft.aggregateEdge(ac.x, ac.y, lcaPrefix, ac)
			return err
		}

	default:
		// inverse interval: [min; y); [x; max]
		// first, we handle [x; max] part
		pf0 := preFirst0(ac.x)
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
			ft.aggregateLeft(idx0, load64(ac.x)<<pf0.len(), pf0, ac)
		case idx0 == noIndex || !pf0Node.leaf():
			// nothing to do
		case ac.prefixWithinRange(followedPrefix) && ac.maybeIncludeNode(pf0Node):
			// node is fully included
		default:
			_, err := ft.aggregateEdge(ac.x, nil, followedPrefix, ac)
			if err != nil {
				return err
			}
		}

		if ac.limit == 0 {
			return nil
		}

		// then we handle [min, y) part
		pf1 := preFirst1(ac.y)
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
			ft.aggregateRight(idx1, load64(ac.y)<<pf1.len(), pf1, ac)
		case idx1 == noIndex || !pf1Node.leaf():
			// nothing to do
		case ac.prefixWithinRange(followedPrefix) && ac.maybeIncludeNode(pf1Node):
			// node is fully included
		default:
			_, err := ft.aggregateEdge(nil, ac.y, followedPrefix, ac)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (ft *fpTree) fingerprintInterval(x, y KeyBytes, limit int) (fpr fpResult, err error) {
	ft.enter("fingerprintInterval: x %s y %s limit %d", x.String(), y.String(), limit)
	defer func() {
		ft.leave(fpr, err)
	}()
	ac := aggContext{x: x, y: y, limit: limit}
	if err := ft.aggregateInterval(&ac); err != nil {
		return fpResult{}, err
	}
	return fpResult{fp: ac.fp, count: ac.count, itype: ac.itype}, nil
}

func (ft *fpTree) dumpNode(w io.Writer, idx nodeIndex, indent, dir string) {
	if idx == noIndex {
		return
	}
	node := ft.np.node(idx)

	leaf := node.leaf()
	countStr := strconv.Itoa(int(node.c))
	if leaf {
		countStr = "LEAF:" + countStr
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

// TBD: optimize, get rid of binary.BigEndian.*
// TBD: QQQQQ: detect unbalancedness when a ref gets too many items
// TBD: QQQQQ: ItemStore.Close(): close db conns, also free fpTree instead of using finalizer!
