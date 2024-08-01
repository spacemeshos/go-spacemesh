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

var errEasySplitFailed = errors.New("easy split failed")

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
	for n, arg := range args {
		if it, ok := arg.(hashsync.Iterator); ok {
			args[n] = formatIter(it)
		}
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
	for n, r := range results {
		if err, ok := r.(error); ok {
			results = []any{fmt.Sprintf("<error: %v>", err)}
			break
		}
		if it, ok := r.(hashsync.Iterator); ok {
			results[n] = formatIter(it)
		}
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
		for n, arg := range args {
			if it, ok := arg.(hashsync.Iterator); ok {
				args[n] = formatIter(it)
			}
		}
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

// TODO: use uint32 for prefix
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

func (p prefix) idAfter(b KeyBytes) {
	if len(b) < 8 {
		panic("BUG: id slice too small")
	}
	s := uint64(64 - p.len())
	v := (p.bits() + 1) << s
	if v == 0 {
		// wraparound
		for n := range b {
			b[n] = 0
		}
		return
	}
	binary.BigEndian.PutUint64(b, v)
	for n := 8; n < len(b); n++ {
		b[n] = 0 // QQQQQ: was 0xff
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
	fp         fingerprint
	count      uint32
	itype      int
	start, end hashsync.Iterator
}

type aggContext struct {
	x, y                    KeyBytes
	fp, fp0                 fingerprint
	count, count0           uint32
	itype                   int
	limit                   int
	total                   uint32
	start, end              hashsync.Iterator
	lastPrefix, lastPrefix0 *prefix
	easySplit               bool
}

// QQQQQ: TBD: rm
// // pruneX returns true if any ID derived from the specified prefix is strictly below x.
// // With inverse intervals, it should only be used when processing [x, max) part of the
// // interval.
// func (ac *aggContext) pruneX(p prefix) bool {
// 	// QQQQQ: TBD: <= must work, check !!!!!
// 	return (p.bits()+1)<<(64-p.len())-1 < load64(ac.x)
// }

// prefixAtOrAfterX verifies that the any key with the prefix p is at or after x.
// It can be used for the whole interval in case of a normal interval.
// With inverse intervals, it should only be used when processing the [x, max) part of the
// interval.
func (ac *aggContext) prefixAtOrAfterX(p prefix) bool {
	return p.bits()<<(64-p.len()) >= load64(ac.x)
}

// QQQQQ: TBD: rm
// // pruneY returns true if any ID derived from the specified prefix is at or after y.
// // With inverse intervals, it should only be used when processing the [0, y) part of the
// // interval.
// func (ac *aggContext) pruneY(p prefix) bool {
// 	return p.bits()<<(64-p.len()) >= load64(ac.y)
// }

// prefixBelowY verifies that the any key with the prefix p is below y.
// It can be used for the whole interval in case of a normal interval.
// With inverse intervals, it should only be used when processing the [0, y) part of the
// interval.
func (ac *aggContext) prefixBelowY(p prefix) bool {
	// QQQQQ: TBD: <= must work, check !!!!!
	return (p.bits()+1)<<(64-p.len())-1 < load64(ac.y)
}

func (ac *aggContext) fingreprintAtOrAfterX(fp fingerprint) bool {
	k := make(KeyBytes, len(ac.x))
	copy(k, fp[:])
	return bytes.Compare(k, ac.x) >= 0
}

func (ac *aggContext) fingreprintBelowY(fp fingerprint) bool {
	k := make(KeyBytes, len(ac.x))
	copy(k, fp[:])
	k[:fingerprintBytes].inc() // 1 after max key derived from the fingerprint
	return bytes.Compare(k, ac.y) <= 0
}

func (ac *aggContext) nodeAtOrAfterX(node node, p prefix) bool {
	if node.c == 1 {
		return ac.fingreprintAtOrAfterX(node.fp)
	}
	return ac.prefixAtOrAfterX(p)
}

func (ac *aggContext) nodeBelowY(node node, p prefix) bool {
	if node.c == 1 {
		return ac.fingreprintBelowY(node.fp)
	}
	return ac.prefixBelowY(p)
}

func (ac *aggContext) pruneY(node node, p prefix) bool {
	if p.bits()<<(64-p.len()) >= load64(ac.y) {
		// min ID derived from the prefix is at or after y => prune
		return true
	}
	if node.c != 1 {
		// node has count > 1, so we can't use its fingerpeint
		// to determine if it's below y
		return false
	}
	k := make(KeyBytes, len(ac.y))
	copy(k, node.fp[:])
	return bytes.Compare(k, ac.y) >= 0
}

func (ac *aggContext) maybeIncludeNode(node node, p prefix) bool {
	switch {
	case ac.limit < 0:
	case uint32(ac.limit) >= node.c:
		ac.limit -= int(node.c)
	case !ac.easySplit || !node.leaf():
		return false
	case ac.count == 0:
		// We're doing a split and this node is over the limit, but the first part
		// is still empty so we include this node in the first part and
		// then switch to the second part
		ac.limit = 0
	default:
		// We're doing a split and this node is over the limit, so store count and
		// fingerpint for the first part and include the current node in the
		// second part
		ac.limit = -1
		ac.fp0 = ac.fp
		ac.count0 = ac.count
		ac.lastPrefix0 = ac.lastPrefix
		copy(ac.fp[:], node.fp[:])
		ac.count = node.c
		ac.lastPrefix = &p
		return true
	}
	ac.fp.update(node.fp[:])
	ac.count += node.c
	ac.lastPrefix = &p
	if ac.easySplit && ac.limit == 0 {
		// We're doing a split and this node is exactly at the limit, or it was
		// above the limit but first part was still empty, so store count and
		// fingerprint for the first part which includes the current node and zero
		// out cound and figerprint for the second part
		ac.limit = -1
		ac.fp0 = ac.fp
		ac.count0 = ac.count
		ac.lastPrefix0 = ac.lastPrefix
		for n := range ac.fp {
			ac.fp[n] = 0
		}
		ac.count = 0
		ac.lastPrefix = nil
	}
	return true
}

type idStore interface {
	clone() idStore
	registerHash(h KeyBytes) error
	start() hashsync.Iterator
	iter(from KeyBytes) hashsync.Iterator
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
		keyLen:   ft.keyLen,
		maxDepth: ft.maxDepth,
	}
}

func (ft *fpTree) pushDown(fpA, fpB fingerprint, p prefix, curCount uint32) nodeIndex {
	// ft.log("QQQQQ: pushDown: fpA %s fpB %s p %s", fpA, fpB, p)
	fpCombined := fpA
	fpCombined.update(fpB[:])
	if ft.maxDepth != 0 && p.len() == ft.maxDepth {
		// ft.log("QQQQQ: pushDown: add at maxDepth")
		return ft.np.add(fpCombined, curCount+1, noIndex, noIndex)
	}
	if curCount != 1 {
		panic("BUG: pushDown of non-1-leaf below maxDepth")
	}
	dirA := fpA.bitFromLeft(p.len())
	dirB := fpB.bitFromLeft(p.len())
	// ft.log("QQQQQ: pushDown: bitFromLeft %d: dirA %v dirB %v", p.len(), dirA, dirB)
	if dirA == dirB {
		childIdx := ft.pushDown(fpA, fpB, p.dir(dirA), 1)
		if dirA {
			r := ft.np.add(fpCombined, 2, noIndex, childIdx)
			// ft.log("QQQQQ: pushDown: sameDir: left => %d", r)
			return r
		} else {
			r := ft.np.add(fpCombined, 2, childIdx, noIndex)
			// ft.log("QQQQQ: pushDown: sameDir: right => %d", r)
			return r
		}
	}

	idxA := ft.np.add(fpA, 1, noIndex, noIndex)
	idxB := ft.np.add(fpB, curCount, noIndex, noIndex)
	if dirA {
		r := ft.np.add(fpCombined, 2, idxB, idxA)
		// ft.log("QQQQQ: pushDown: add A-B => %d", r)
		return r
	} else {
		r := ft.np.add(fpCombined, 2, idxA, idxB)
		// ft.log("QQQQQ: pushDown: add B-A => %d", r)
		return r
	}
}

func (ft *fpTree) addValue(fp fingerprint, p prefix, idx nodeIndex) nodeIndex {
	if idx == noIndex {
		r := ft.np.add(fp, 1, noIndex, noIndex)
		// ft.log("QQQQQ: addValue: addNew fp %s p %s => %d", fp, p, r)
		return r
	}
	node := ft.np.node(idx)
	// defer ft.releaseNode(idx)
	if node.c == 1 || (ft.maxDepth != 0 && p.len() == ft.maxDepth) {
		// we're at a leaf node, need to push down the old fingerprint, or,
		// if we've reached the max depth, just update the current node
		r := ft.pushDown(fp, node.fp, p, node.c)
		// ft.log("QQQQQ: addValue: pushDown fp %s p %s oldIdx %d => %d", fp, p, idx, r)
		return r
	}
	fpCombined := fp
	fpCombined.update(node.fp[:])
	if fp.bitFromLeft(p.len()) {
		// ft.log("QQQQQ: addValue: replaceRight fp %s p %s oldIdx %d", fp, p, idx)
		if node.left != noIndex {
			ft.np.ref(node.left)
			// ft.log("QQQQQ: addValue: ref left %d -- refCount %d", node.left, ft.np.entry(node.left).refCount)
		}
		newRight := ft.addValue(fp, p.right(), node.right)
		r := ft.np.add(fpCombined, node.c+1, node.left, newRight)
		// ft.log("QQQQQ: addValue: replaceRight fp %s p %s oldIdx %d => %d node.left %d newRight %d", fp, p, idx, r, node.left, newRight)
		return r
	} else {
		// ft.log("QQQQQ: addValue: replaceLeft fp %s p %s oldIdx %d", fp, p, idx)
		if node.right != noIndex {
			// ft.log("QQQQQ: addValue: ref right %d -- refCount %d", node.right, ft.np.entry(node.right).refCount)
			ft.np.ref(node.right)
		}
		newLeft := ft.addValue(fp, p.left(), node.left)
		r := ft.np.add(fpCombined, node.c+1, newLeft, node.right)
		// ft.log("QQQQQ: addValue: replaceLeft fp %s p %s oldIdx %d => %d newLeft %d node.right %d", fp, p, idx, r, newLeft, node.right)
		return r
	}
}

func (ft *fpTree) addStoredHash(h KeyBytes) {
	var fp fingerprint
	fp.update(h)
	ft.rootMtx.Lock()
	defer ft.rootMtx.Unlock()
	ft.log("addStoredHash: h %s fp %s", h, fp)
	oldRoot := ft.root
	ft.root = ft.addValue(fp, 0, ft.root)
	ft.releaseNode(oldRoot)
}

func (ft *fpTree) addHash(h KeyBytes) error {
	ft.log("addHash: h %s", h)
	if err := ft.idStore.registerHash(h); err != nil {
		return err
	}
	ft.addStoredHash(h)
	return nil
}

func (ft *fpTree) followPrefix(from nodeIndex, p, followed prefix) (idx nodeIndex, rp prefix, found bool) {
	ft.enter("followPrefix: from %d p %s highBit %v", from, p, p.highBit())
	defer func() { ft.leave(idx, rp, found) }()

	for from != noIndex {
		switch {
		case p == 0:
			return from, followed, true
		case ft.np.node(from).leaf():
			return from, followed, false
		case p.highBit():
			from = ft.np.node(from).right
			p = p.shift()
			followed = followed.right()
		default:
			from = ft.np.node(from).left
			p = p.shift()
			followed = followed.left()
		}
	}

	return noIndex, followed, false
}

// aggregateEdge aggregates an edge of the interval, which can be bounded by x, y, both x
// and y or none of x and y, have a common prefix and optionally bounded by a limit of N of
// aggregated items.
// It returns a boolean indicating whether the limit or the right edge (y) was reached and
// an error, if any.
func (ft *fpTree) aggregateEdge(x, y KeyBytes, p prefix, ac *aggContext) (cont bool, err error) {
	ft.enter("aggregateEdge: x %s y %s p %s limit %d count %d", x, y, p, ac.limit, ac.count)
	defer func() {
		ft.leave(ac.limit, ac.count, cont, err)
	}()
	if ac.easySplit {
		// easySplit means we should not be querying the database,
		// so we'll have to retry using slower strategy
		return false, errEasySplitFailed
	}
	if ac.limit == 0 && ac.end != nil {
		ft.log("aggregateEdge: limit is 0 and end already set")
		return false, nil
	}
	var startFrom KeyBytes
	if x == nil {
		startFrom = make(KeyBytes, ft.keyLen)
		p.minID(startFrom)
	} else {
		startFrom = x
	}
	ft.log("aggregateEdge: startFrom %s", startFrom)
	it := ft.iter(startFrom)
	if ac.limit == 0 {
		ac.end = it.Clone()
		if x != nil {
			ft.log("aggregateEdge: limit 0: x is not nil, setting start to %s", ac.start)
			ac.start = ac.end
		}
		ft.log("aggregateEdge: limit is 0 at %s", ac.end)
		return false, nil
	}
	if x != nil {
		ac.start = it.Clone()
		ft.log("aggregateEdge: x is not nil, setting start to %s", ac.start)
	}

	for range ft.np.node(ft.root).c {
		id, err := it.Key()
		if err != nil {
			return false, err
		}
		ft.log("aggregateEdge: ID %s", id)
		if y != nil && id.Compare(y) >= 0 {
			ac.end = it
			ft.log("aggregateEdge: ID is over Y: %s", id)
			return false, nil
		}
		if !p.match(id.(KeyBytes)) {
			ft.log("aggregateEdge: ID doesn't match the prefix: %s", id)
			ac.lastPrefix = &p
			return true, nil
		}
		ac.fp.update(id.(KeyBytes))
		ac.count++
		if ac.limit > 0 {
			ac.limit--
		}
		if err := it.Next(); err != nil {
			ft.log("aggregateEdge: Next failed: %v", err)
			return false, err
		}
		if ac.limit == 0 {
			ac.end = it
			ft.log("aggregateEdge: limit exhausted")
			return false, nil
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
	ft.enter("aggregateUpToLimit: idx %d p %s limit %d cur_fp %s cur_count0 %d cur_count %d", idx, p, ac.limit,
		ac.fp, ac.count0, ac.count)
	defer func() {
		ft.leave(ac.fp, ac.count0, ac.count, err)
	}()
	node, ok := ft.node(idx)
	switch {
	case !ok:
		ft.log("stop: no node")
		return true, nil
	case ac.maybeIncludeNode(node, p):
		// node is fully included
		ft.log("included fully, lastPrefix = %s", ac.lastPrefix)
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
			if ac.maybeIncludeNode(left, pLeft) {
				// left node is fully included, after which
				// we need to stop somewhere in the right subtree
				ft.log("include left in full")
			} else {
				// we must stop somewhere in the left subtree,
				// and the right subtree is irrelevant unless
				// easySplit is being done and we must restart
				// after the limit is exhausted
				ft.log("descend to the left")
				if cont, err := ft.aggregateUpToLimit(node.left, pLeft, ac); !cont || err != nil {
					return cont, err
				}
				if !ac.easySplit {
					return
				}
			}
		}
		ft.log("descend to the right")
		return ft.aggregateUpToLimit(node.right, p.right(), ac)
	}
}

func (ft *fpTree) aggregateLeft(idx nodeIndex, v uint64, p prefix, ac *aggContext) (cont bool, err error) {
	ft.enter("aggregateLeft: idx %d v %016x p %s limit %d", idx, v, p, ac.limit)
	defer func() {
		ft.leave(ac.fp, ac.count0, ac.count, err)
	}()
	node, ok := ft.node(idx)
	switch {
	case !ok:
		// for ac.limit == 0, it's important that we still visit the node
		// so that we can get the item immediately following the included items
		ft.log("stop: no node")
		return true, nil
	// QQQQQ: TBD: rm
	// case ac.pruneX(p):
	// 	// TODO: rm, this never happens
	// 	ft.log("prune: prefix not at or after x")
	// 	return true, nil
	case ac.nodeAtOrAfterX(node, p) && ac.maybeIncludeNode(node, p):
		ft.log("including node in full: %s limit %d", p, ac.limit)
		return ac.limit != 0, nil
	case p.len() == ft.maxDepth || node.leaf():
		if node.left != noIndex || node.right != noIndex {
			panic("BUG: node @ maxDepth has children")
		}
		// if node.c == 1 {;
		// 	fmt.Fprintf(os.Stderr, "QQQQQ: aggregateLeft: edge with x %s p %s limit %d\n", ac.x, p, ac.limit)
		// }
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
		ft.leave(ac.fp, ac.count0, ac.count, err)
	}()
	node, ok := ft.node(idx)
	switch {
	case !ok:
		ft.log("stop: no node")
		return true, nil
	case ac.nodeBelowY(node, p) && ac.maybeIncludeNode(node, p):
		ft.log("including node in full: %s limit %d", p, ac.limit)
		return ac.limit != 0, nil
	case p.len() == ft.maxDepth || node.leaf():
		if node.left != noIndex || node.right != noIndex {
			panic("BUG: node @ maxDepth has children")
		}
		if ac.pruneY(node, p) {
			ft.log("node %d p %s pruned", idx, p)
			return false, nil
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

func (ft *fpTree) aggregateXX(ac *aggContext) (err error) {
	// [x; x) interval which denotes the whole set unless
	// the limit is specified, in which case we need to start aggregating
	// with x and wrap around if necessary
	ft.enter("aggregateXX: x %s limit %d", ac.x, ac.limit)
	defer func() {
		ft.leave(ac, err)
	}()
	if ft.root == noIndex {
		ft.log("empty set (no root)")
	} else if ac.maybeIncludeNode(ft.np.node(ft.root), 0) {
		ft.log("whole set")
	} else {
		// We need to aggregate up to ac.limit number of items starting
		// from x and wrapping around if necessary
		return ft.aggregateInverse(ac)
	}
	return nil
}

func (ft *fpTree) aggregateSimple(ac *aggContext) (err error) {
	// "proper" interval: [x; lca); (lca; y)
	ft.enter("aggregateSimple: x %s y %s limit %d", ac.x, ac.y, ac.limit)
	defer func() {
		ft.leave(ac, err)
	}()
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
		if _, err := ft.aggregateLeft(lca.left, load64(ac.x)<<(p.len()+1), p.left(), ac); err != nil {
			return err
		}
		if ac.limit != 0 {
			if _, err := ft.aggregateRight(lca.right, load64(ac.y)<<(p.len()+1), p.right(), ac); err != nil {
				return err
			}
		}
	case lcaIdx == noIndex || !lca.leaf():
		ft.log("commonPrefix %s NOT found b/c no items have it", p)
	case ac.nodeAtOrAfterX(lca, lcaPrefix) && ac.nodeBelowY(lca, lcaPrefix) &&
		ac.maybeIncludeNode(lca, lcaPrefix):
		ft.log("commonPrefix %s -- lca node %d included in full", p, lcaIdx)
	default:
		//ac.prefixAtOrAfterX(lcaPrefix) && ac.prefixBelowY(lcaPrefix):
		ft.log("commonPrefix %s -- lca %d", p, lcaIdx)
		_, err := ft.aggregateEdge(ac.x, ac.y, lcaPrefix, ac)
		return err
	}
	return nil
}

func (ft *fpTree) aggregateInverse(ac *aggContext) (err error) {
	// inverse interval: [min; y); [x; max]

	// First, we handle [x; max] part
	// For this, we process the subtree rooted in the LCA of 0x000000... (all 0s) and x
	ft.enter("aggregateInverse: x %s y %s limit %d", ac.x, ac.y, ac.limit)
	defer func() {
		ft.leave(ac, err)
	}()
	pf0 := preFirst0(ac.x)
	idx0, followedPrefix, found := ft.followPrefix(ft.root, pf0, 0)
	var pf0Node node
	if idx0 != noIndex {
		pf0Node = ft.np.node(idx0)
	}
	ft.log("pf0 %s idx0 %d found %v followedPrefix %s", pf0, idx0, found, followedPrefix)
	switch {
	case found && !pf0Node.leaf():
		if followedPrefix != pf0 {
			panic("BUG: bad followedPrefix")
		}
		cont, err := ft.aggregateLeft(idx0, load64(ac.x)<<pf0.len(), pf0, ac)
		if err != nil {
			return err
		}
		if !cont {
			return nil
		}
	case idx0 == noIndex || !pf0Node.leaf():
		// nothing to do
	case ac.nodeAtOrAfterX(pf0Node, followedPrefix) && ac.maybeIncludeNode(pf0Node, followedPrefix):
		// node is fully included
	default:
		_, err := ft.aggregateEdge(ac.x, nil, followedPrefix, ac)
		if err != nil {
			return err
		}
	}

	if ac.limit == 0 && !ac.easySplit {
		return nil
	}

	// Then we handle [min, y) part.
	// For this, we process the subtree rooted in the LCA of y and 0xffffff... (all 1s)
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
		if _, err := ft.aggregateRight(idx1, load64(ac.y)<<pf1.len(), pf1, ac); err != nil {
			return err
		}
	case idx1 == noIndex || !pf1Node.leaf():
		// nothing to do
	case ac.nodeBelowY(pf1Node, followedPrefix) && ac.maybeIncludeNode(pf1Node, followedPrefix):
		// node is fully included
	case ac.pruneY(pf1Node, followedPrefix):
		ft.log("node %d p %s pruned", idx1, followedPrefix)
		return nil
	default:
		_, err := ft.aggregateEdge(nil, ac.y, followedPrefix, ac)
		if err != nil {
			return err
		}
	}

	return nil
}

func (ft *fpTree) aggregateInterval(ac *aggContext) (err error) {
	// Lock rootMtx to make cloning fpTree thread-safe.
	// Note that fpTree methods other than clone are not thread-safe.
	ft.rootMtx.Lock()
	defer ft.rootMtx.Unlock()
	ft.enter("aggregateInterval: x %s y %s limit %d", ac.x, ac.y, ac.limit)
	defer func() {
		ft.leave(ac, err)
	}()
	ac.itype = bytes.Compare(ac.x, ac.y)
	if ft.root == noIndex {
		return nil
	}
	ac.total = ft.np.node(ft.root).c
	switch ac.itype {
	case 0:
		return ft.aggregateXX(ac)
	case -1:
		return ft.aggregateSimple(ac)
	default:
		return ft.aggregateInverse(ac)
	}
}

func (ft *fpTree) endIterFromPrefix(p prefix) hashsync.Iterator {
	k := make(KeyBytes, ft.keyLen)
	p.idAfter(k)
	ft.log("endIterFromPrefix: p: %s idAfter: %s", p, k)
	return ft.iter(k)
}

func (ft *fpTree) fingerprintInterval(x, y KeyBytes, limit int) (fpr fpResult, err error) {
	ft.enter("fingerprintInterval: x %s y %s limit %d", x, y, limit)
	defer func() {
		ft.leave(fpr.fp, fpr.count, fpr.itype, fpr.start, fpr.end, err)
	}()
	ac := aggContext{x: x, y: y, limit: limit}
	if err := ft.aggregateInterval(&ac); err != nil {
		return fpResult{}, err
	}
	fpr = fpResult{
		fp:    ac.fp,
		count: ac.count,
		itype: ac.itype,
	}

	if ac.total == 0 {
		return fpr, nil
	}

	if ac.start != nil {
		ft.log("fingerprintInterval: start %s", ac.start)
		fpr.start = ac.start
	} else {
		fpr.start = ft.iter(x)
		ft.log("fingerprintInterval: start from x: %s", fpr.start)
	}

	if ac.end != nil {
		ft.log("fingerprintInterval: end %s", ac.end)
		fpr.end = ac.end
	} else if (fpr.itype == 0 && limit < 0) || fpr.count == 0 {
		fpr.end = fpr.start
		ft.log("fingerprintInterval: end at start %s", fpr.end)
	} else if ac.lastPrefix != nil {
		fpr.end = ft.endIterFromPrefix(*ac.lastPrefix)
		ft.log("fingerprintInterval: end at lastPrefix %s -> %s", *ac.lastPrefix, fpr.end)
	} else {
		fpr.end = ft.iter(y)
		ft.log("fingerprintInterval: end at y: %s", fpr.end)
	}

	return fpr, nil
}

type splitResult struct {
	part0, part1 fpResult
	middle       KeyBytes
}

// easySplit splits an interval in two parts trying to do it in such way that the first
// part has close to limit items while not making any idStore queries so that the database
// is not accessed. If the split can't be done, which includes the situation where one of
// the sides has 0 items, easySplit returns errEasySplitFailed error
func (ft *fpTree) easySplit(x, y KeyBytes, limit int) (sr splitResult, err error) {
	ft.enter("easySplit: x %s y %s limit %d", x, y, limit)
	defer func() {
		ft.leave(sr.part0.fp, sr.part0.count, sr.part0.itype, sr.part0.start, sr.part0.end,
			sr.part1.fp, sr.part1.count, sr.part1.itype, sr.part1.start, sr.part1.end, err)
	}()
	if limit < 0 {
		panic("BUG: easySplit with limit < 0")
	}
	ac := aggContext{x: x, y: y, limit: limit, easySplit: true}
	if err := ft.aggregateInterval(&ac); err != nil {
		return splitResult{}, err
	}

	if ac.total == 0 {
		return splitResult{}, nil
	}

	if ac.count0 == 0 || ac.count == 0 {
		// need to get some items on both sides for the easy split to succeed
		ft.log("easySplit failed: one side missing: count0 %d count %d", ac.count0, ac.count)
		return splitResult{}, errEasySplitFailed
	}

	// It should not be possible to have ac.lastPrefix0 == nil or ac.lastPrefix == nil
	// if both ac.count0 and ac.count are non-zero, b/c of how
	// aggContext.maybeIncludeNode works
	if ac.lastPrefix0 == nil || ac.lastPrefix == nil {
		panic("BUG: easySplit lastPrefix or lastPrefix0 not set")
	}

	// ac.start / ac.end are only set in aggregateEdge which fails with
	// errEasySplitFailed if easySplit is enabled, so we can ignore them here
	middle := make(KeyBytes, ft.keyLen)
	ac.lastPrefix0.idAfter(middle)
	part0 := fpResult{
		fp:    ac.fp0,
		count: ac.count0,
		itype: ac.itype,
		start: ft.iter(x),
		end:   ft.endIterFromPrefix(*ac.lastPrefix0),
	}
	part1 := fpResult{
		fp:    ac.fp,
		count: ac.count,
		itype: ac.itype,
		start: part0.end.Clone(),
		end:   ft.endIterFromPrefix(*ac.lastPrefix),
	}
	return splitResult{
		part0:  part0,
		part1:  part1,
		middle: middle,
	}, nil
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

func (ft *fpTree) count() int {
	if ft.root == noIndex {
		return 0
	}
	return int(ft.np.node(ft.root).c)
}

type iterFormatter struct {
	it hashsync.Iterator
}

func (f iterFormatter) String() string {
	if k, err := f.it.Key(); err != nil {
		return fmt.Sprintf("<error: %v>", err)
	} else {
		return k.(fmt.Stringer).String()
	}
}

func formatIter(it hashsync.Iterator) fmt.Stringer {
	return iterFormatter{it: it}
}

// TBD: optimize, get rid of binary.BigEndian.*
// TBD: QQQQQ: detect unbalancedness when a ref gets too many items
// TBD: QQQQQ: ItemStore.Close(): close db conns, also free fpTree instead of using finalizer!
