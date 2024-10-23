package fptree

import (
	"errors"
	"fmt"
	"io"
	"runtime"
	"strconv"
	"strings"

	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
	"github.com/spacemeshos/go-spacemesh/sync2/sqlstore"
)

var errEasySplitFailed = errors.New("easy split failed")

const (
	FingerprintSize = rangesync.FingerprintSize
	// sizeHintCoef is used to calculate the number of pool entries to preallocate for
	// an FPTree based on the expected number of items which this tree may contain.
	sizeHintCoef = 2.1
)

// FPResult represents the result of a range fingerprint query against FPTree, as returned
// by FingerprintInterval.
type FPResult struct {
	// Range fingerprint
	FP rangesync.Fingerprint
	// Number of items in the range
	Count uint32
	// Interval type: -1 for normal, 0 for the whole set, 1 for wrapped around ("inverse")
	IType int
	// Items in the range
	Items rangesync.SeqResult
	// The item following the range
	Next rangesync.KeyBytes
}

// SplitResult represents the result of a split operation.
type SplitResult struct {
	// The two parts of the inteval
	Part0, Part1 FPResult
	// Moddle point value
	Middle rangesync.KeyBytes
}

// aggContext is the context used for aggregation operations.
type aggContext struct {
	// nodePool used by the tree
	np *nodePool
	// Bounds of the interval being aggregated
	x, y rangesync.KeyBytes
	// The current fingerprint of the items aggregated so far, since the beginning or
	// after the split ("easy split")
	fp rangesync.Fingerprint
	// The fingerprint of the items aggregated in the first part of the split
	fp0 rangesync.Fingerprint
	// Number of items aggregated so far, since the beginning or after the split
	// ("easy split")
	count uint32
	// Number of items aggregated in the first part of the split
	count0 uint32
	// Interval type: -1 for normal, 0 for the whole set, 1 for wrapped around ("inverse")
	itype int
	// Maximum remaining number of items to aggregate.
	limit int
	// The number of items aggregated so far.
	total uint32
	// The resulting item sequence.
	items rangesync.SeqResult
	// The item immediately following the aggregated items.
	next rangesync.KeyBytes
	// The prefix corresponding to the last aggregated node.
	lastPrefix *prefix
	// The prefix corresponding to the last aggregated node in the first part of the split.
	lastPrefix0 *prefix
	// Whether the aggregation is being done for an "easy split" (split operation
	// without querying the underlying IDStore).
	easySplit bool
}

// prefixAtOrAfterX verifies that the any key with the prefix p is at or after x.
// It can be used for the whole interval in case of a normal interval.
// With inverse intervals, it should only be used when processing the [x, max) part of the
// interval.
func (ac *aggContext) prefixAtOrAfterX(p prefix) bool {
	b := make(rangesync.KeyBytes, len(ac.x))
	p.minID(b)
	return b.Compare(ac.x) >= 0
}

// prefixBelowY verifies that the any key with the prefix p is below y.
// It can be used for the whole interval in case of a normal interval.
// With inverse intervals, it should only be used when processing the [0, y) part of the
// interval.
func (ac *aggContext) prefixBelowY(p prefix) bool {
	b := make(rangesync.KeyBytes, len(ac.y))
	// If p.idAfter(b) is true, this means there's wraparound and
	// b is zero whereas all the possible keys beginning with prefix p
	// are non-zero. In this case, there can be no key y such that
	// all the keys beginning with prefix p are below y.
	return !p.idAfter(b) && b.Compare(ac.y) <= 0
}

// fingerprintAtOrAfterX verifies that the specified fingerprint, which should be derived
// from a single key, is at or after x bound of the interval.
func (ac *aggContext) fingreprintAtOrAfterX(fp rangesync.Fingerprint) bool {
	k := make(rangesync.KeyBytes, len(ac.x))
	copy(k, fp[:])
	return k.Compare(ac.x) >= 0
}

// fingerprintBelowY verifies that the specified fingerprint, which should be derived from a
// single key, is below y bound of the interval.
func (ac *aggContext) fingreprintBelowY(fp rangesync.Fingerprint) bool {
	k := make(rangesync.KeyBytes, len(ac.x))
	copy(k, fp[:])
	k[:FingerprintSize].Inc() // 1 after max key derived from the fingerprint
	return k.Compare(ac.y) <= 0
}

// nodeAtOrAfterX verifies that the node with the given index is at or after x bound of the
// interval.
func (ac *aggContext) nodeAtOrAfterX(idx nodeIndex, p prefix) bool {
	count, fp, _ := ac.np.info(idx)
	if count == 1 {
		v := ac.np.value(idx)
		if v != nil {
			return v.Compare(ac.x) >= 0
		}
		return ac.fingreprintAtOrAfterX(fp)
	}
	return ac.prefixAtOrAfterX(p)
}

// nodeBelowY verifies that the node with the given index is below y bound of the interval.
func (ac *aggContext) nodeBelowY(idx nodeIndex, p prefix) bool {
	count, fp, _ := ac.np.info(idx)
	if count == 1 {
		v := ac.np.value(idx)
		if v != nil {
			return v.Compare(ac.y) < 0
		}
		return ac.fingreprintBelowY(fp)
	}
	return ac.prefixBelowY(p)
}

// pruneX returns true if the specified node can be pruned during left-aggregation because
// all of its keys are below the x bound of the interval.
func (ac *aggContext) pruneX(idx nodeIndex, p prefix) bool {
	b := make(rangesync.KeyBytes, len(ac.x))
	if !p.idAfter(b) && b.Compare(ac.x) <= 0 {
		// idAfter derived from the prefix is at or below y => prune
		return true
	}
	count, fp, _ := ac.np.info(idx)
	if count > 1 {
		// node has count > 1, so we can't use its fingerprint or value to
		// determine if it's at or after X
		return false
	}
	k := ac.np.value(idx)
	if k != nil {
		return k.Compare(ac.x) < 0
	}

	k = make(rangesync.KeyBytes, len(ac.x))
	copy(k, fp[:])
	k[:FingerprintSize].Inc() // 1 after max key derived from the fingerprint
	return k.Compare(ac.x) <= 0
}

// pruneY returns true if the specified node can be pruned during right-aggregation
// because all of its keys are at or after the y bound of the interval.
func (ac *aggContext) pruneY(idx nodeIndex, p prefix) bool {
	b := make(rangesync.KeyBytes, len(ac.y))
	p.minID(b)
	if b.Compare(ac.y) >= 0 {
		// min ID derived from the prefix is at or after y => prune
		return true
	}

	count, fp, _ := ac.np.info(idx)
	if count > 1 {
		// node has count > 1, so we can't use its fingerprint or value to
		// determine if it's below y
		return false
	}
	k := ac.np.value(idx)
	if k == nil {
		k = make(rangesync.KeyBytes, len(ac.y))
		copy(k, fp[:])
	}
	return k.Compare(ac.y) >= 0
}

// switchToSecondPart switches aggregation to the second part of the "easy split".
func (ac *aggContext) switchToSecondPart() {
	ac.limit = -1
	ac.fp0 = ac.fp
	ac.count0 = ac.count
	ac.lastPrefix0 = ac.lastPrefix
	clear(ac.fp[:])
	ac.count = 0
	ac.lastPrefix = nil
}

// maybeIncludeNode returns tries to include the full contents of the specified node in
// the aggregation and returns if it succeeded, based on the remaining limit and the numer
// of items in the node.
// It also handles "easy split" happening at the node.
func (ac *aggContext) maybeIncludeNode(idx nodeIndex, p prefix) bool {
	count, fp, leaf := ac.np.info(idx)
	switch {
	case ac.limit < 0:
	case uint32(ac.limit) >= count:
		ac.limit -= int(count)
	case !ac.easySplit || !leaf:
		return false
	case ac.count == 0:
		// We're doing a split and this node is over the limit, but the first part
		// is still empty so we include this node in the first part and
		// then switch to the second part
		ac.limit = 0
	default:
		// We're doing a split and this node is over the limit, so store count and
		// fingerprint for the first part and include the current node in the
		// second part
		ac.limit = -1
		ac.fp0 = ac.fp
		ac.count0 = ac.count
		ac.lastPrefix0 = ac.lastPrefix
		copy(ac.fp[:], fp[:])
		ac.count = count
		ac.lastPrefix = &p
		return true
	}
	ac.fp.Update(fp[:])
	ac.count += count
	ac.lastPrefix = &p
	if ac.easySplit && ac.limit == 0 {
		// We're doing a split and this node is exactly at the limit, or it was
		// above the limit but first part was still empty, so store count and
		// fingerprint for the first part which includes the current node and zero
		// out cound and figerprint for the second part
		ac.switchToSecondPart()
	}
	return true
}

// FPTree is a binary tree data structure designed to perform range fingerprint queries
// efficiently.
// FPTree can work on its own, with fingerprint query complexity being O(log n).
// It can also be backed by an IDStore with a depth limit the binary tree, in which
// case the query efficiency degrades with the number of items growing.
// O(log n) query efficiency can be retained in this case for queries which
// have the number of non-zero bits, starting from the high bit, below maxDepth.
// FPTree does not do any special balancing and relies on the IDs added on it being
// uniformly distributed, which is the case for the IDs based on cryptographic hashes.
type FPTree struct {
	trace
	idStore  sqlstore.IDStore
	np       *nodePool
	root     nodeIndex
	keyLen   int
	maxDepth int
}

var _ sqlstore.IDStore = &FPTree{}

// NewFPTreeWithValues creates an FPTree which also stores the items themselves and does
// not make use of a backing IDStore.
// sizeHint specifies the approximage expected number of items.
// keyLen specifies the number of bytes in keys used.
func NewFPTreeWithValues(sizeHint, keyLen int) *FPTree {
	return NewFPTree(sizeHint, nil, keyLen, 0)
}

// NewFPTree creates an FPTree of limited depth backed by an IDStore.
// sizeHint specifies the approximage expected number of items.
// keyLen specifies the number of bytes in keys used.
func NewFPTree(sizeHint int, idStore sqlstore.IDStore, keyLen, maxDepth int) *FPTree {
	var np nodePool
	if sizeHint > 0 {
		size := int(float64(sizeHint) * sizeHintCoef)
		if maxDepth > 0 {
			size = min(size, 1<<(maxDepth+1))
		}
		np.init(size)
	}
	if idStore == nil && maxDepth != 0 {
		panic("BUG: newFPTree: no idStore, but maxDepth specified")
	}
	ft := &FPTree{
		np:       &np,
		idStore:  idStore,
		root:     noIndex,
		keyLen:   keyLen,
		maxDepth: maxDepth,
	}
	runtime.SetFinalizer(ft, (*FPTree).Release)
	return ft
}

// traverse traverses the subtree rooted in idx in order and calls the given function for
// each item.
func (ft *FPTree) traverse(idx nodeIndex, yield func(rangesync.KeyBytes) bool) (res bool) {
	ft.enter("traverse: idx %d", idx)
	defer func() {
		ft.leave(res)
	}()
	if idx == noIndex {
		ft.log("no index")
		return true
	}
	l := ft.np.left(idx)
	r := ft.np.right(idx)
	if l == noIndex && r == noIndex {
		v := ft.np.value(idx)
		if v != nil {
			ft.log("yield value %s", v.ShortString())
		}
		if v != nil && !yield(v) {
			return false
		}
		return true
	}
	return ft.traverse(l, yield) && ft.traverse(r, yield)
}

// travereFrom traverses the subtree rooted in idx in order and calls the given function for
// each item starting from the given key.
func (ft *FPTree) traverseFrom(
	idx nodeIndex,
	p prefix,
	from rangesync.KeyBytes,
	yield func(rangesync.KeyBytes) bool,
) (res bool) {
	ft.enter("traverseFrom: idx %d p %s from %s", idx, p, from)
	defer func() {
		ft.leave(res)
	}()
	if idx == noIndex {
		return true
	}
	if p == emptyPrefix || ft.np.leaf(idx) {
		v := ft.np.value(idx)
		if v != nil && v.Compare(from) >= 0 {
			ft.log("yield value %s", v.ShortString())
			if !yield(v) {
				return false
			}
		}
		return true
	}
	if !p.highBit() {
		return ft.traverseFrom(ft.np.left(idx), p.shift(), from, yield) &&
			ft.traverse(ft.np.right(idx), yield)
	} else {
		return ft.traverseFrom(ft.np.right(idx), p.shift(), from, yield)
	}
}

// All returns all the items currently in the tree (including those in the IDStore).
// Implements sqlstore.All.
func (ft *FPTree) All() rangesync.SeqResult {
	ft.np.lockRead()
	defer ft.np.unlockRead()
	switch {
	case ft.root == noIndex:
		return rangesync.EmptySeqResult()
	case ft.storeValues():
		return rangesync.SeqResult{
			Seq: func(yield func(rangesync.KeyBytes) bool) {
				for {
					if !ft.traverse(ft.root, yield) {
						break
					}
				}
			},
			Error: rangesync.NoSeqError,
		}
	}
	return ft.idStore.All()
}

// From returns all the items in the tree that are greater than or equal to the given key.
// Implements sqlstore.IDStore.
func (ft *FPTree) From(from rangesync.KeyBytes, sizeHint int) rangesync.SeqResult {
	ft.np.lockRead()
	defer ft.np.unlockRead()
	switch {
	case ft.root == noIndex:
		return rangesync.EmptySeqResult()
	case ft.storeValues():
		return rangesync.SeqResult{
			Seq: func(yield func(rangesync.KeyBytes) bool) {
				p := prefixFromKeyBytes(from)
				if !ft.traverseFrom(ft.root, p, from, yield) {
					return
				}
				for {
					if !ft.traverse(ft.root, yield) {
						break
					}
				}
			},
			Error: rangesync.NoSeqError,
		}
	}
	return ft.idStore.From(from, sizeHint)
}

// Release releases resources used by the tree.
// Implements sqlstore.IDStore.
func (ft *FPTree) Release() {
	ft.np.lockWrite()
	defer ft.np.unlockWrite()
	ft.np.release(ft.root)
	ft.root = noIndex
	if ft.idStore != nil {
		ft.idStore.Release()
	}
}

// Clear removes all items from the tree.
// It should only be used with trees that were created using NewFPtreeWithValues.
func (ft *FPTree) Clear() {
	if !ft.storeValues() {
		// if we have an idStore, it can't be cleared and thus the tree can't be
		// cleared either
		panic("BUG: can only clear fpTree with values")
	}
	ft.Release()
}

// Clone makes a copy of the tree.
// The copy operation is thread-safe and has complexity of O(1).
func (ft *FPTree) Clone() sqlstore.IDStore {
	ft.np.lockWrite()
	defer ft.np.unlockWrite()
	if ft.root != noIndex {
		ft.np.ref(ft.root)
	}
	var idStore sqlstore.IDStore
	if !ft.storeValues() {
		idStore = ft.idStore.Clone()
	}
	return &FPTree{
		np:       ft.np,
		idStore:  idStore,
		root:     ft.root,
		keyLen:   ft.keyLen,
		maxDepth: ft.maxDepth,
	}
}

// pushLeafDown pushes a leaf node down the tree when the node's path matches that of the
// new to be added, splitting it if necessary.
func (ft *FPTree) pushLeafDown(
	idx nodeIndex,
	replace bool,
	singleFP, prevFP rangesync.Fingerprint,
	depth int,
	curCount uint32,
	value, prevValue rangesync.KeyBytes,
) (newIdx nodeIndex) {
	if idx == noIndex {
		panic("BUG: pushLeafDown on a nonexistent node")
	}
	// Once we stumble upon a node with refCount > 1, we no longer can replace nodes
	// as they're also referenced by another tree.
	if replace && ft.np.refCount(idx) > 1 {
		ft.np.releaseOne(idx)
		replace = false
	}
	replace = replace && ft.np.refCount(idx) == 1
	replaceIdx := noIndex
	if replace {
		replaceIdx = idx
	}
	fpCombined := rangesync.CombineFingerprints(singleFP, prevFP)
	if ft.maxDepth != 0 && depth == ft.maxDepth {
		newIdx = ft.np.add(fpCombined, curCount+1, noIndex, noIndex, nil, replaceIdx)
		return newIdx
	}
	if curCount != 1 {
		panic("BUG: pushDown of non-1-leaf below maxDepth")
	}
	dirA := singleFP.BitFromLeft(depth)
	dirB := prevFP.BitFromLeft(depth)
	if dirA == dirB {
		// TODO: in the proper radix tree, these 1-child nodes should never be
		// created, accumulating the prefix instead
		childIdx := ft.pushLeafDown(idx, replace, singleFP, prevFP, depth+1, 1, value, prevValue)
		if dirA {
			newIdx = ft.np.add(fpCombined, 2, noIndex, childIdx, nil, noIndex)
		} else {
			newIdx = ft.np.add(fpCombined, 2, childIdx, noIndex, nil, noIndex)
		}
	} else {
		idxA := ft.np.add(singleFP, 1, noIndex, noIndex, value, noIndex)
		idxB := ft.np.add(prevFP, curCount, noIndex, noIndex, prevValue, replaceIdx)
		if dirA {
			newIdx = ft.np.add(fpCombined, 2, idxB, idxA, nil, noIndex)
		} else {
			newIdx = ft.np.add(fpCombined, 2, idxA, idxB, nil, noIndex)
		}
	}
	return newIdx
}

// addValue adds a value to the subtree rooted in idx.
func (ft *FPTree) addValue(
	idx nodeIndex,
	replace bool,
	fp rangesync.Fingerprint,
	depth int,
	value rangesync.KeyBytes,
) (newIdx nodeIndex) {
	if idx == noIndex {
		newIdx = ft.np.add(fp, 1, noIndex, noIndex, value, noIndex)
		return newIdx
	}
	// Once we stumble upon a node with refCount > 1, we no longer can replace nodes
	// as they're also referenced by another tree.
	if replace && ft.np.refCount(idx) > 1 {
		ft.np.releaseOne(idx)
		replace = false
	}
	count, nodeFP, leaf := ft.np.info(idx)
	left := ft.np.left(idx)
	right := ft.np.right(idx)
	nodeValue := ft.np.value(idx)
	if leaf {
		if count != 1 && (ft.maxDepth == 0 || depth != ft.maxDepth) {
			panic("BUG: unexpected leaf node")
		}
		// we're at a leaf node, need to push down the old fingerprint, or,
		// if we've reached the max depth, just update the current node
		return ft.pushLeafDown(idx, replace, fp, nodeFP, depth, count, value, nodeValue)
	}
	replaceIdx := noIndex
	if replace {
		replaceIdx = idx
	}
	fpCombined := rangesync.CombineFingerprints(fp, nodeFP)
	if fp.BitFromLeft(depth) {
		newRight := ft.addValue(right, replace, fp, depth+1, value)
		newIdx := ft.np.add(fpCombined, count+1, left, newRight, nil, replaceIdx)
		if !replace && left != noIndex {
			// the original node is not being replaced, so the reused left
			// node has acquired another reference
			ft.np.ref(left)
		}
		return newIdx
	} else {
		newLeft := ft.addValue(left, replace, fp, depth+1, value)
		newIdx := ft.np.add(fpCombined, count+1, newLeft, right, nil, replaceIdx)
		if !replace && right != noIndex {
			// the original node is not being replaced, so the reused right
			// node has acquired another reference
			ft.np.ref(right)
		}
		return newIdx
	}
}

// AddStoredKey adds a key to the tree, assuming that either the tree doesn't have an
// IDStore ar the IDStore already contains the key.
func (ft *FPTree) AddStoredKey(k rangesync.KeyBytes) {
	var fp rangesync.Fingerprint
	fp.Update(k)
	ft.log("addStoredHash: h %s fp %s", k, fp)
	var v rangesync.KeyBytes
	if ft.storeValues() {
		v = k
	}
	ft.np.lockWrite()
	defer ft.np.unlockWrite()
	ft.root = ft.addValue(ft.root, true, fp, 0, v)
}

// RegisterKey registers a key in the tree.
// If the tree has an IDStore, the key is also registered with the IDStore.
func (ft *FPTree) RegisterKey(k rangesync.KeyBytes) error {
	ft.log("addHash: k %s", k)
	if !ft.storeValues() {
		if err := ft.idStore.RegisterKey(k); err != nil {
			return err
		}
	}
	ft.AddStoredKey(k)
	return nil
}

// storeValues returns true if the tree stores the values (has no IDStore).
func (ft *FPTree) storeValues() bool {
	return ft.idStore == nil
}

// CheckKey returns true if the tree contains or may contain the given key.
// If this function returns false, the tree definitely doesn't contain the key.
// If this function returns true and the tree stores the values, the key is definitely
// contained in the tree.
// If this function returns true and the tree doesn't store the values, the key may be
// contained in the tree.
func (ft *FPTree) CheckKey(k rangesync.KeyBytes) bool {
	// We're unlikely to be able to find a node with the full prefix, but if we can
	// find a leaf node with matching partial prefix, that's good enough except
	// that we also need to check the node's fingerprint.
	idx, _, _ := ft.followPrefix(ft.root, prefixFromKeyBytes(k), emptyPrefix)
	if idx == noIndex {
		return false
	}
	count, fp, _ := ft.np.info(idx)
	if count != 1 {
		return true
	}
	var kFP rangesync.Fingerprint
	kFP.Update(k)
	return fp == kFP
}

// followPrefix follows the bit prefix p from the node idx.
func (ft *FPTree) followPrefix(from nodeIndex, p, followed prefix) (idx nodeIndex, rp prefix, found bool) {
	ft.enter("followPrefix: from %d p %s highBit %v", from, p, p.highBit())
	defer func() { ft.leave(idx, rp, found) }()

	for from != noIndex {
		switch {
		case p.len() == 0:
			return from, followed, true
		case ft.np.leaf(from):
			return from, followed, false
		case p.highBit():
			from = ft.np.right(from)
			p = p.shift()
			followed = followed.right()
		default:
			from = ft.np.left(from)
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
func (ft *FPTree) aggregateEdge(
	x, y rangesync.KeyBytes,
	idx nodeIndex,
	p prefix,
	ac *aggContext,
) (cont bool, err error) {
	ft.enter("aggregateEdge: x %s y %s p %s limit %d count %d", x, y, p, ac.limit, ac.count)
	defer func() {
		ft.leave(ac.limit, ac.count, cont, err)
	}()
	if ft.storeValues() {
		panic("BUG: aggregateEdge should not be used for tree with values")
	}
	if ac.easySplit {
		// easySplit means we should not be querying the database,
		// so we'll have to retry using slower strategy
		return false, errEasySplitFailed
	}
	if ac.limit == 0 && ac.next != nil {
		ft.log("aggregateEdge: limit is 0 and end already set")
		return false, nil
	}
	var startFrom rangesync.KeyBytes
	if x == nil {
		startFrom = make(rangesync.KeyBytes, ft.keyLen)
		p.minID(startFrom)
	} else {
		startFrom = x
	}
	ft.log("aggregateEdge: startFrom %s", startFrom)
	sizeHint := int(ft.np.count(idx))
	switch {
	case ac.limit == 0:
		sizeHint = 1
	case ac.limit > 0:
		sizeHint = min(ac.limit, sizeHint)
	}
	sr := ft.From(startFrom, sizeHint)
	if ac.limit == 0 {
		next, err := sr.First()
		if err != nil {
			return false, err
		}
		ac.next = next.Clone()
		if x != nil {
			ft.log("aggregateEdge: limit 0: x is not nil, setting start to %s", ac.next.String())
			ac.items = sr
		}
		ft.log("aggregateEdge: limit is 0 at %s", ac.next.String())
		return false, nil
	}
	if x != nil {
		ac.items = sr
		ft.log("aggregateEdge: x is not nil, setting start to %v", sr)
	}

	n := ft.np.count(ft.root)
	for id := range sr.Seq {
		if ac.limit == 0 && !ac.easySplit {
			ac.next = id.Clone()
			ft.log("aggregateEdge: limit exhausted")
			return false, nil
		}
		if n == 0 {
			break
		}
		ft.log("aggregateEdge: ID %s", id)
		if y != nil && id.Compare(y) >= 0 {
			ac.next = id.Clone()
			ft.log("aggregateEdge: ID is over Y: %s", id)
			return false, nil
		}
		if !p.match(id) {
			ft.log("aggregateEdge: ID doesn't match the prefix: %s", id)
			ac.lastPrefix = &p
			return true, nil
		}
		if ac.limit == 0 {
			ft.log("aggregateEdge: switching to second part of easySplit")
			ac.switchToSecondPart()
		}
		ac.fp.Update(id)
		ac.count++
		if ac.limit > 0 {
			ac.limit--
		}
		n--
	}
	if err := sr.Error(); err != nil {
		return false, err
	}

	return true, nil
}

// aggregateUpToLimit aggregates the subtree rooted in idx up to the limit of N of nodes.
func (ft *FPTree) aggregateUpToLimit(idx nodeIndex, p prefix, ac *aggContext) (cont bool, err error) {
	ft.enter("aggregateUpToLimit: idx %d p %s limit %d cur_fp %s cur_count0 %d cur_count %d", idx, p, ac.limit,
		ac.fp, ac.count0, ac.count)
	defer func() {
		ft.leave(ac.fp, ac.count0, ac.count, err)
	}()
	switch {
	case idx == noIndex:
		ft.log("stop: no node")
		return true, nil
	case ac.limit == 0:
		return false, nil
	case ac.maybeIncludeNode(idx, p):
		// node is fully included
		ft.log("included fully, lastPrefix = %s", ac.lastPrefix)
		return true, nil
	case ft.np.leaf(idx):
		// reached the limit on this node, do not need to continue after
		// done with it
		cont, err := ft.aggregateEdge(nil, nil, idx, p, ac)
		if err != nil {
			return false, err
		}
		if cont {
			panic("BUG: expected limit not reached")
		}
		return false, nil
	default:
		pLeft := p.left()
		left := ft.np.left(idx)
		if left != noIndex {
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
				if cont, err := ft.aggregateUpToLimit(left, pLeft, ac); !cont || err != nil {
					return cont, err
				}
				if !ac.easySplit {
					return false, nil
				}
			}
		}
		ft.log("descend to the right")
		return ft.aggregateUpToLimit(ft.np.right(idx), p.right(), ac)
	}
}

// aggregateLeft aggregates the subtree that covers the left subtree of the LCA in case of
// normal intervals, and the subtree that covers [x, MAX] part for the inverse (wrapped
// around) intervals.
func (ft *FPTree) aggregateLeft(
	idx nodeIndex,
	k rangesync.KeyBytes,
	p prefix,
	ac *aggContext,
) (cont bool, err error) {
	ft.enter("aggregateLeft: idx %d k %s p %s limit %d", idx, k.ShortString(), p, ac.limit)
	defer func() {
		ft.leave(ac.fp, ac.count0, ac.count, err)
	}()
	switch {
	case idx == noIndex:
		// for ac.limit == 0, it's important that we still visit the node
		// so that we can get the item immediately following the included items
		ft.log("stop: no node")
		return true, nil
	case ac.limit == 0:
		return false, nil
	case ac.nodeAtOrAfterX(idx, p) && ac.maybeIncludeNode(idx, p):
		ft.log("including node in full: %s limit %d", p, ac.limit)
		return true, nil
	case (ft.maxDepth != 0 && p.len() == ft.maxDepth) || ft.np.leaf(idx):
		if ac.pruneX(idx, p) {
			ft.log("node %d p %s pruned", idx, p)
			// we've not reached X yet so we should not stop, thus true
			return true, nil
		}
		return ft.aggregateEdge(ac.x, nil, idx, p, ac)
	case !k.BitFromLeft(p.len()):
		left := ft.np.left(idx)
		right := ft.np.right(idx)
		ft.log("incl right node %d + go left to node %d", right, left)
		cont, err := ft.aggregateLeft(left, k, p.left(), ac)
		if !cont || err != nil {
			return false, err
		}
		if right != noIndex {
			return ft.aggregateUpToLimit(right, p.right(), ac)
		}
		return true, nil
	default:
		right := ft.np.right(idx)
		ft.log("go right to node %d", right)
		return ft.aggregateLeft(right, k, p.right(), ac)
	}
}

// aggregateRight aggregates the subtree that covers the right subtree of the LCA in case
// of normal intervals, and the subtree that covers [0, y) part for the inverse (wrapped
// around) intervals.
func (ft *FPTree) aggregateRight(
	idx nodeIndex,
	k rangesync.KeyBytes,
	p prefix,
	ac *aggContext,
) (cont bool, err error) {
	ft.enter("aggregateRight: idx %d k %s p %s limit %d", idx, k.ShortString(), p, ac.limit)
	defer func() {
		ft.leave(ac.fp, ac.count0, ac.count, err)
	}()
	switch {
	case idx == noIndex:
		ft.log("stop: no node")
		return true, nil
	case ac.limit == 0:
		return false, nil
	case ac.nodeBelowY(idx, p) && ac.maybeIncludeNode(idx, p):
		ft.log("including node in full: %s limit %d", p, ac.limit)
		return ac.limit != 0, nil
	case (ft.maxDepth != 0 && p.len() == ft.maxDepth) || ft.np.leaf(idx):
		if ac.pruneY(idx, p) {
			ft.log("node %d p %s pruned", idx, p)
			return false, nil
		}
		return ft.aggregateEdge(nil, ac.y, idx, p, ac)
	case !k.BitFromLeft(p.len()):
		left := ft.np.left(idx)
		ft.log("go left to node %d", left)
		return ft.aggregateRight(left, k, p.left(), ac)
	default:
		left := ft.np.left(idx)
		right := ft.np.right(idx)
		ft.log("incl left node %d + go right to node %d", left, right)
		if left != noIndex {
			cont, err := ft.aggregateUpToLimit(left, p.left(), ac)
			if !cont || err != nil {
				return false, err
			}
		}
		return ft.aggregateRight(ft.np.right(idx), k, p.right(), ac)
	}
}

// aggregateXX aggregtes intervals of form [x, x) which denotes the whole set.
func (ft *FPTree) aggregateXX(ac *aggContext) (err error) {
	// [x, x) interval which denotes the whole set unless
	// the limit is specified, in which case we need to start aggregating
	// with x and wrap around if necessary
	ft.enter("aggregateXX: x %s limit %d", ac.x, ac.limit)
	defer func() {
		ft.leave(ac, err)
	}()
	if ft.root == noIndex {
		ft.log("empty set (no root)")
	} else if ac.maybeIncludeNode(ft.root, emptyPrefix) {
		ft.log("whole set")
	} else {
		// We need to aggregate up to ac.limit number of items starting
		// from x and wrapping around if necessary
		return ft.aggregateInverse(ac)
	}
	return nil
}

// aggregateSimple aggregates simple (normal) intervals of form [x, y) where x < y.
func (ft *FPTree) aggregateSimple(ac *aggContext) (err error) {
	// "proper" interval: [x, lca); (lca, y)
	ft.enter("aggregateSimple: x %s y %s limit %d", ac.x, ac.y, ac.limit)
	defer func() {
		ft.leave(ac, err)
	}()
	p := commonPrefix(ac.x, ac.y)
	lcaIdx, lcaPrefix, fullPrefixFound := ft.followPrefix(ft.root, p, emptyPrefix)
	ft.log("commonPrefix %s lcaPrefix %s lca %d found %v", p, lcaPrefix, lcaIdx, fullPrefixFound)
	switch {
	case fullPrefixFound && !ft.np.leaf(lcaIdx):
		if lcaPrefix != p {
			panic("BUG: bad followedPrefix")
		}
		if _, err := ft.aggregateLeft(ft.np.left(lcaIdx), ac.x, p.left(), ac); err != nil {
			return err
		}
		if ac.limit != 0 {
			if _, err := ft.aggregateRight(ft.np.right(lcaIdx), ac.y, p.right(), ac); err != nil {
				return err
			}
		}
	case lcaIdx == noIndex || !ft.np.leaf(lcaIdx):
		ft.log("commonPrefix %s NOT found b/c no items have it", p)
	case ac.nodeAtOrAfterX(lcaIdx, lcaPrefix) && ac.nodeBelowY(lcaIdx, lcaPrefix) &&
		ac.maybeIncludeNode(lcaIdx, lcaPrefix):
		ft.log("commonPrefix %s -- lca node %d included in full", p, lcaIdx)
	case ft.np.leaf(lcaIdx) && ft.np.value(lcaIdx) != nil:
		// leaf 1-node with value that could not be included should be skipped
		return nil
	default:
		ft.log("commonPrefix %s -- lca %d", p, lcaIdx)
		_, err := ft.aggregateEdge(ac.x, ac.y, lcaIdx, lcaPrefix, ac)
		return err
	}
	return nil
}

// aggregateInverse aggregates inverse intervals of form [x, y) where x > y.
func (ft *FPTree) aggregateInverse(ac *aggContext) (err error) {
	// inverse interval: [min, y); [x, max]

	// First, we handle [x, max] part
	// For this, we process the subtree rooted in the LCA of 0x000000... (all 0s) and x
	ft.enter("aggregateInverse: x %s y %s limit %d", ac.x, ac.y, ac.limit)
	defer func() {
		ft.leave(ac, err)
	}()
	pf0 := preFirst0(ac.x)
	idx0, followedPrefix, found := ft.followPrefix(ft.root, pf0, emptyPrefix)
	ft.log("pf0 %s idx0 %d found %v followedPrefix %s", pf0, idx0, found, followedPrefix)
	switch {
	case found && !ft.np.leaf(idx0):
		if followedPrefix != pf0 {
			panic("BUG: bad followedPrefix")
		}
		cont, err := ft.aggregateLeft(idx0, ac.x, pf0, ac)
		if err != nil {
			return err
		}
		if !cont {
			return nil
		}
	case idx0 == noIndex || !ft.np.leaf(idx0):
		// nothing to do
	case ac.nodeAtOrAfterX(idx0, followedPrefix) && ac.maybeIncludeNode(idx0, followedPrefix):
		// node is fully included
	case ac.pruneX(idx0, followedPrefix):
		// the node is below X
		ft.log("node %d p %s pruned", idx0, followedPrefix)
	default:
		_, err := ft.aggregateEdge(ac.x, nil, idx0, followedPrefix, ac)
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
	idx1, followedPrefix, found := ft.followPrefix(ft.root, pf1, emptyPrefix)
	ft.log("pf1 %s idx1 %d found %v", pf1, idx1, found)
	switch {
	case found && !ft.np.leaf(idx1):
		if followedPrefix != pf1 {
			panic("BUG: bad followedPrefix")
		}
		if _, err := ft.aggregateRight(idx1, ac.y, pf1, ac); err != nil {
			return err
		}
	case idx1 == noIndex || !ft.np.leaf(idx1):
		// nothing to do
	case ac.nodeBelowY(idx1, followedPrefix) && ac.maybeIncludeNode(idx1, followedPrefix):
		// node is fully included
	case ac.pruneY(idx1, followedPrefix):
		// the node is at or after Y
		ft.log("node %d p %s pruned", idx1, followedPrefix)
		return nil
	default:
		_, err := ft.aggregateEdge(nil, ac.y, idx1, followedPrefix, ac)
		if err != nil {
			return err
		}
	}

	return nil
}

// aggregateInterval aggregates an interval, updating the aggContext accordingly.
func (ft *FPTree) aggregateInterval(ac *aggContext) (err error) {
	ft.enter("aggregateInterval: x %s y %s limit %d", ac.x, ac.y, ac.limit)
	defer func() {
		ft.leave(ac, err)
	}()
	ac.itype = ac.x.Compare(ac.y)
	if ft.root == noIndex {
		return nil
	}
	ac.total = ft.np.count(ft.root)
	switch ac.itype {
	case 0:
		return ft.aggregateXX(ac)
	case -1:
		return ft.aggregateSimple(ac)
	default:
		return ft.aggregateInverse(ac)
	}
}

// startFromPrefix returns a SeqResult which begins with the first item that has the
// specified prefix.
func (ft *FPTree) startFromPrefix(ac *aggContext, p prefix) rangesync.SeqResult {
	k := make(rangesync.KeyBytes, ft.keyLen)
	p.idAfter(k)
	ft.log("startFromPrefix: p: %s idAfter: %s", p, k)
	return ft.From(k, 1)
}

// nextFromPrefix return the first item that has the prefix p.
func (ft *FPTree) nextFromPrefix(ac *aggContext, p prefix) (rangesync.KeyBytes, error) {
	id, err := ft.startFromPrefix(ac, p).First()
	if err != nil {
		return nil, err
	}
	if id == nil {
		return nil, nil
	}
	return id.Clone(), nil
}

// FingerprintInteval performs a range fingerprint query with specified bounds and limit.
func (ft *FPTree) FingerprintInterval(x, y rangesync.KeyBytes, limit int) (fpr FPResult, err error) {
	ft.np.lockRead()
	defer ft.np.unlockRead()
	return ft.fingerprintInterval(x, y, limit)
}

func (ft *FPTree) fingerprintInterval(x, y rangesync.KeyBytes, limit int) (fpr FPResult, err error) {
	ft.enter("fingerprintInterval: x %s y %s limit %d", x, y, limit)
	defer func() {
		ft.leave(fpr.FP, fpr.Count, fpr.IType, fpr.Items, fpr.Next, err)
	}()
	ac := aggContext{np: ft.np, x: x, y: y, limit: limit}
	if err := ft.aggregateInterval(&ac); err != nil {
		return FPResult{}, err
	}
	fpr = FPResult{
		FP:    ac.fp,
		Count: ac.count,
		IType: ac.itype,
		Items: rangesync.EmptySeqResult(),
	}

	if ac.total == 0 {
		return fpr, nil
	}

	if ac.items.Seq != nil {
		ft.log("fingerprintInterval: items %v", ac.items)
		fpr.Items = ac.items
	} else {
		fpr.Items = ft.From(x, 1)
		ft.log("fingerprintInterval: start from x: %v", fpr.Items)
	}

	if ac.next != nil {
		ft.log("fingerprintInterval: next %s", ac.next)
		fpr.Next = ac.next
	} else if (fpr.IType == 0 && limit < 0) || fpr.Count == 0 {
		next, err := fpr.Items.First()
		if err != nil {
			return FPResult{}, err
		}
		if next != nil {
			fpr.Next = next.Clone()
		}
		ft.log("fingerprintInterval: next at start %s", fpr.Next)
	} else if ac.lastPrefix != nil {
		fpr.Next, err = ft.nextFromPrefix(&ac, *ac.lastPrefix)
		ft.log("fingerprintInterval: next at lastPrefix %s -> %s", *ac.lastPrefix, fpr.Next)
	} else {
		next, err := ft.From(y, 1).First()
		if err != nil {
			return FPResult{}, err
		}
		fpr.Next = next.Clone()
		ft.log("fingerprintInterval: next at y: %s", fpr.Next)
	}

	return fpr, nil
}

// easySplit splits an interval in two parts trying to do it in such way that the first
// part has close to limit items while not making any idStore queries so that the database
// is not accessed. If the split can't be done, which includes the situation where one of
// the sides has 0 items, easySplit returns errEasySplitFailed error.
// easySplit never fails for a tree with values.
func (ft *FPTree) easySplit(x, y rangesync.KeyBytes, limit int) (sr SplitResult, err error) {
	ft.enter("easySplit: x %s y %s limit %d", x, y, limit)
	defer func() {
		ft.leave(sr.Part0.FP, sr.Part0.Count, sr.Part0.IType, sr.Part0.Items, sr.Part0.Next,
			sr.Part1.FP, sr.Part1.Count, sr.Part1.IType, sr.Part1.Items, sr.Part1.Next, err)
	}()
	if limit < 0 {
		panic("BUG: easySplit with limit < 0")
	}
	ac := aggContext{np: ft.np, x: x, y: y, limit: limit, easySplit: true}
	if err := ft.aggregateInterval(&ac); err != nil {
		return SplitResult{}, err
	}

	if ac.total == 0 {
		return SplitResult{}, nil
	}

	if ac.count0 == 0 || ac.count == 0 {
		// need to get some items on both sides for the easy split to succeed
		ft.log("easySplit failed: one side missing: count0 %d count %d", ac.count0, ac.count)
		return SplitResult{}, errEasySplitFailed
	}

	// It should not be possible to have ac.lastPrefix0 == nil or ac.lastPrefix == nil
	// if both ac.count0 and ac.count are non-zero, b/c of how
	// aggContext.maybeIncludeNode works
	if ac.lastPrefix0 == nil || ac.lastPrefix == nil {
		panic("BUG: easySplit lastPrefix or lastPrefix0 not set")
	}

	// ac.start / ac.end are only set in aggregateEdge which fails with
	// errEasySplitFailed if easySplit is enabled, so we can ignore them here
	middle := make(rangesync.KeyBytes, ft.keyLen)
	ac.lastPrefix0.idAfter(middle)
	ft.log("easySplit: lastPrefix0 %s middle %s", ac.lastPrefix0, middle)
	items := ft.From(x, 1)
	part0 := FPResult{
		FP:    ac.fp0,
		Count: ac.count0,
		IType: ac.itype,
		Items: items,
		// Next is only used during splitting itself, and thus not included
	}
	items = ft.startFromPrefix(&ac, *ac.lastPrefix0)
	part1 := FPResult{
		FP:    ac.fp,
		Count: ac.count,
		IType: ac.itype,
		Items: items,
		// Next is only used during splitting itself, and thus not included
	}
	return SplitResult{
		Part0:  part0,
		Part1:  part1,
		Middle: middle,
	}, nil
}

// Split splits an interval in two parts.
func (ft *FPTree) Split(x, y rangesync.KeyBytes, limit int) (sr SplitResult, err error) {
	ft.np.lockRead()
	defer ft.np.unlockRead()
	sr, err = ft.easySplit(x, y, limit)
	if err == nil {
		return sr, nil
	}
	if err != errEasySplitFailed {
		return SplitResult{}, err
	}

	fpr0, err := ft.fingerprintInterval(x, y, limit)
	if err != nil {
		return SplitResult{}, err
	}

	if fpr0.Count == 0 {
		return SplitResult{}, errors.New("can't split empty range")
	}

	fpr1, err := ft.fingerprintInterval(fpr0.Next, y, -1)
	if err != nil {
		return SplitResult{}, err
	}

	if fpr1.Count == 0 {
		return SplitResult{}, errors.New("split produced empty 2nd range")
	}

	return SplitResult{
		Part0:  fpr0,
		Part1:  fpr1,
		Middle: fpr0.Next,
	}, nil
}

// dumpNode prints the node structure to the writer.
func (ft *FPTree) dumpNode(w io.Writer, idx nodeIndex, indent, dir string) {
	if idx == noIndex {
		return
	}

	count, fp, leaf := ft.np.info(idx)
	countStr := strconv.Itoa(int(count))
	if leaf {
		countStr = "LEAF:" + countStr
	}
	var valStr string
	if v := ft.np.value(idx); v != nil {
		valStr = fmt.Sprintf(" <val:%s>", v.ShortString())
	}
	fmt.Fprintf(w, "%s%sidx=%d %s %s [%d]%s\n", indent, dir, idx, fp, countStr, ft.np.refCount(idx), valStr)
	if !leaf {
		indent += "  "
		ft.dumpNode(w, ft.np.left(idx), indent, "l: ")
		ft.dumpNode(w, ft.np.right(idx), indent, "r: ")
	}
}

// Dump prints the tree structure to the writer.
func (ft *FPTree) Dump(w io.Writer) {
	ft.np.lockRead()
	defer ft.np.unlockRead()
	if ft.root == noIndex {
		fmt.Fprintln(w, "empty tree")
	} else {
		ft.dumpNode(w, ft.root, "", "")
	}
}

// DumpToString returns the tree structure as a string.
func (ft *FPTree) DumpToString() string {
	var sb strings.Builder
	ft.Dump(&sb)
	return sb.String()
}

// Count returns the number of items in the tree.
func (ft *FPTree) Count() int {
	ft.np.lockRead()
	defer ft.np.unlockRead()
	if ft.root == noIndex {
		return 0
	}
	return int(ft.np.count(ft.root))
}

// EnableTrace enables or disables tracing for the tree.
func (ft *FPTree) EnableTrace(enable bool) {
	ft.traceEnabled = enable
}
