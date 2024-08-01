// TBD: add paper ref
package hashsync

import (
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"sync"
)

type Ordered interface {
	Compare(other any) int
}

type LowerBound struct{}

var _ Ordered = LowerBound{}

func (vb LowerBound) Compare(x any) int { return -1 }

type UpperBound struct{}

var _ Ordered = UpperBound{}

func (vb UpperBound) Compare(x any) int { return 1 }

type FingerprintPredicate func(fp any) bool

func (fpred FingerprintPredicate) Match(y any) bool {
	return fpred != nil && fpred(y)
}

type SyncTree interface {
	// Make a copy of the tree. The copy shares the structure with this tree but all
	// its nodes are copy-on-write, so any changes in the copied tree do not affect
	// this one and are safe to perform in another goroutine. The copy operation is
	// O(n) where n is the number of nodes added to this tree since its creation via
	// either NewSyncTree function or this Copy method, or the last call of this Copy
	// method for this tree, whichever occurs last. The call to Copy is thread-safe.
	Copy() SyncTree
	Fingerprint() any
	Add(k Ordered)
	Set(k Ordered, v any)
	Lookup(k Ordered) (any, bool)
	Min() SyncTreePointer
	RangeFingerprint(ptr SyncTreePointer, start, end Ordered, stop FingerprintPredicate) (fp any, startNode, endNode SyncTreePointer)
	Dump() string
}

func SyncTreeFromSortedSlice[T Ordered](m Monoid, items []T) SyncTree {
	s := make([]Ordered, len(items))
	for n, item := range items {
		s[n] = item
	}
	st := NewSyncTree(m).(*syncTree)
	st.root = st.buildFromSortedSlice(s)
	return st
}

func SyncTreeFromSlice[T Ordered](m Monoid, items []T) SyncTree {
	sorted := make([]T, len(items))
	copy(sorted, items)
	slices.SortFunc(sorted, func(a, b T) int {
		return a.Compare(b)
	})
	return SyncTreeFromSortedSlice(m, items)
}

type SyncTreePointer interface {
	Equal(other SyncTreePointer) bool
	Key() Ordered
	Value() any
	Prev()
	Next()
	Clone() SyncTreePointer
}

type flags uint8

const (
	// flagBlack indicates a black node. If it is not set, the
	// node is red, which is the default for newly created nodes
	flagBlack flags = 1
	// flagCloned indicates a node that is only present in this
	// tree and not in any of its copies, thus permitting
	// modification of this node without cloning it. When the tree
	// is copied, flagCloned is cleared on all of its nodes.
	flagCloned flags = 2
)

type dir uint8

const (
	left  dir = 0
	right dir = 1
)

func (d dir) flip() dir { return d ^ 1 }

func (d dir) String() string {
	switch d {
	case left:
		return "left"
	case right:
		return "right"
	default:
		return fmt.Sprintf("<bad: %d>", d)
	}
}

const initialParentStackSize = 32

type syncTreePointer struct {
	parentStack []*syncTreeNode
	node        *syncTreeNode
}

var _ SyncTreePointer = &syncTreePointer{}

func (p *syncTreePointer) clone() *syncTreePointer {
	// TODO: copy node stack
	r := &syncTreePointer{
		parentStack: make([]*syncTreeNode, len(p.parentStack), cap(p.parentStack)),
		node:        p.node,
	}
	copy(r.parentStack, p.parentStack)
	return r
}

func (p *syncTreePointer) parent() {
	n := len(p.parentStack)
	if n == 0 {
		p.node = nil
	} else {
		n--
		p.node = p.parentStack[n]
		p.parentStack = p.parentStack[:n]
	}
}

func (p *syncTreePointer) left() {
	if p.node != nil {
		p.parentStack = append(p.parentStack, p.node)
		p.node = p.node.left
	}
}

func (p *syncTreePointer) right() {
	if p.node != nil {
		p.parentStack = append(p.parentStack, p.node)
		p.node = p.node.right
	}
}

func (p *syncTreePointer) min() {
	for {
		switch {
		case p.node == nil || p.node.left == nil:
			return
		default:
			p.left()
		}
	}
}

func (p *syncTreePointer) max() {
	for {
		switch {
		case p.node == nil || p.node.right == nil:
			return
		default:
			p.right()
		}
	}
}

func (p *syncTreePointer) Equal(other SyncTreePointer) bool {
	if other == nil {
		return p.node == nil
	}
	return p.node == other.(*syncTreePointer).node
}

func (p *syncTreePointer) Prev() {
	switch {
	case p.node == nil:
	case p.node.left != nil:
		p.left()
		p.max()
	default:
		oldNode := p.node
		for {
			p.parent()
			if p.node == nil || oldNode != p.node.left {
				return
			}
			oldNode = p.node
		}
	}
}

func (p *syncTreePointer) Next() {
	switch {
	case p.node == nil:
	case p.node.right != nil:
		p.right()
		p.min()
	default:
		oldNode := p.node
		for {
			p.parent()
			if p.node == nil || oldNode != p.node.right {
				return
			}
			oldNode = p.node
		}
	}
}

func (p *syncTreePointer) Clone() SyncTreePointer {
	return &syncTreePointer{
		parentStack: slices.Clone(p.parentStack),
		node:        p.node,
	}
}

func (p *syncTreePointer) Key() Ordered {
	if p.node == nil {
		return nil
	}
	return p.node.key
}

func (p *syncTreePointer) Value() any {
	if p.node == nil {
		return nil
	}
	return p.node.value
}

type syncTreeNode struct {
	left        *syncTreeNode
	right       *syncTreeNode
	key         Ordered
	value       any
	max         Ordered
	fingerprint any
	flags       flags
}

func (sn *syncTreeNode) red() bool {
	return sn != nil && (sn.flags&flagBlack) == 0
}

func (sn *syncTreeNode) black() bool {
	return sn == nil || (sn.flags&flagBlack) != 0
}

func (sn *syncTreeNode) child(dir dir) *syncTreeNode {
	if sn == nil {
		return nil
	}
	if dir == left {
		return sn.left
	}
	return sn.right
}

func (sn *syncTreeNode) Key() Ordered { return sn.key }

func (sn *syncTreeNode) dump(w io.Writer, indent int) {
	indentStr := strings.Repeat("  ", indent)
	fmt.Fprintf(w, "%skey: %v\n", indentStr, sn.key)
	fmt.Fprintf(w, "%smax: %v\n", indentStr, sn.max)
	fmt.Fprintf(w, "%sfp: %v\n", indentStr, sn.fingerprint)
	color := "red"
	if sn.black() {
		color = "black"
	}
	fmt.Fprintf(w, "%scolor: %v\n", indentStr, color)
	if sn.left != nil {
		fmt.Fprintf(w, "%sleft:\n", indentStr)
		sn.left.dump(w, indent+1)
		if sn.left.key.Compare(sn.key) >= 0 {
			fmt.Fprintf(w, "%sERROR: left key >= parent key\n", indentStr)
		}
	}
	if sn.right != nil {
		fmt.Fprintf(w, "%sright:\n", indentStr)
		sn.right.dump(w, indent+1)
		if sn.right.key.Compare(sn.key) <= 0 {
			fmt.Fprintf(w, "%sERROR: right key <= parent key\n", indentStr)
		}
	}
}

func (sn *syncTreeNode) dumpSubtree() string {
	var sb strings.Builder
	sn.dump(&sb, 0)
	return sb.String()
}

// cleanNodes removed flagCloned from all of the nodes in the subtree,
// so that it can be used in further cloned trees.
// A non-cloned node cannot have any cloned children, so the function
// stops the recursion at any non-cloned node.
func (sn *syncTreeNode) cleanCloned() {
	if sn == nil || sn.flags&flagCloned == 0 {
		return
	}
	sn.flags &^= flagCloned
	sn.left.cleanCloned()
	sn.right.cleanCloned()
}

type syncTree struct {
	rootMtx      sync.Mutex
	m            Monoid
	root         *syncTreeNode
	cachedMinPtr *syncTreePointer
	cachedMaxPtr *syncTreePointer
}

func NewSyncTree(m Monoid) SyncTree {
	return &syncTree{m: m}
}

func (st *syncTree) Copy() SyncTree {
	st.rootMtx.Lock()
	defer st.rootMtx.Unlock()
	// Clean flagCloned from any nodes created specifically for
	// this tree. This will mean they will have to be re-cloned if
	// they need to be changed again.
	st.root.cleanCloned()
	// Don't reuse cachedMinPtr / cachedMaxPtr for the cloned
	// tree to be on the safe side
	return &syncTree{
		m:    st.m,
		root: st.root,
	}
}

func (st *syncTree) rootPtr() *syncTreePointer {
	return &syncTreePointer{
		parentStack: make([]*syncTreeNode, 0, initialParentStackSize),
		node:        st.root,
	}
}

func (st *syncTree) ensureCloned(sn *syncTreeNode) *syncTreeNode {
	if sn.flags&flagCloned != 0 {
		return sn
	}
	cloned := *sn
	cloned.flags |= flagCloned
	return &cloned
}

func (st *syncTree) setChild(sn *syncTreeNode, dir dir, child *syncTreeNode) *syncTreeNode {
	if sn == nil {
		panic("setChild for a nil node")
	}
	if sn.child(dir) == child {
		return sn
	}
	sn = st.ensureCloned(sn)
	if dir == left {
		sn.left = child
	} else {
		sn.right = child
	}
	return sn
}

func (st *syncTree) flip(sn *syncTreeNode) *syncTreeNode {
	if sn.left == nil || sn.right == nil {
		panic("can't flip color with one or more nil children")
	}

	left := st.ensureCloned(sn.left)
	right := st.ensureCloned(sn.right)
	sn = st.ensureCloned(sn)
	sn.left = left
	sn.right = right

	sn.flags ^= flagBlack
	left.flags ^= flagBlack
	right.flags ^= flagBlack
	return sn
}

func (st *syncTree) Min() SyncTreePointer {
	if st.root == nil {
		return nil
	}
	if st.cachedMinPtr == nil {
		st.cachedMinPtr = st.rootPtr()
		st.cachedMinPtr.min()
	}
	if st.cachedMinPtr.node == nil {
		panic("BUG: no minNode in a non-empty tree")
	}
	return st.cachedMinPtr.clone()
}

func (st *syncTree) Fingerprint() any {
	if st.root == nil {
		return st.m.Identity()
	}
	return st.root.fingerprint
}

func (st *syncTree) newNode(k Ordered, v any) *syncTreeNode {
	return &syncTreeNode{
		key:         k,
		value:       v,
		max:         k,
		fingerprint: st.m.Fingerprint(k),
	}
}

func (st *syncTree) buildFromSortedSlice(s []Ordered) *syncTreeNode {
	switch len(s) {
	case 0:
		return nil
	case 1:
		return st.newNode(s[0], nil)
	}
	middle := len(s) / 2
	node := st.newNode(s[middle], nil)
	node.left = st.buildFromSortedSlice(s[:middle])
	node.right = st.buildFromSortedSlice(s[middle+1:])
	if node.left != nil {
		node.fingerprint = st.m.Op(node.left.fingerprint, node.fingerprint)
	}
	if node.right != nil {
		node.fingerprint = st.m.Op(node.fingerprint, node.right.fingerprint)
		node.max = node.right.max
	}
	return node
}

func (st *syncTree) safeFingerprint(sn *syncTreeNode) any {
	if sn == nil {
		return st.m.Identity()
	}
	return sn.fingerprint
}

func (st *syncTree) updateFingerprintAndMax(sn *syncTreeNode) {
	fp := st.m.Op(st.safeFingerprint(sn.left), st.m.Fingerprint(sn.key))
	fp = st.m.Op(fp, st.safeFingerprint(sn.right))
	newMax := sn.key
	if sn.right != nil {
		newMax = sn.right.max
	}
	if sn.flags&flagCloned == 0 &&
		(!reflect.DeepEqual(sn.fingerprint, fp) || sn.max.Compare(newMax) != 0) {
		panic("BUG: updating fingerprint/max for a non-cloned node")
	}
	sn.fingerprint = fp
	sn.max = newMax
}

func (st *syncTree) rotate(sn *syncTreeNode, d dir) *syncTreeNode {
	// sn.verify()

	rd := d.flip()
	tmp := sn.child(rd)
	if tmp == nil {
		panic("BUG: nil parent after rotate")
	}
	// fmt.Fprintf(os.Stderr, "QQQQQ: rotate %s (child at %s is %s): subtree:\n%s\n",
	// 	d, rd, tmp.key, sn.dumpSubtree())
	sn = st.setChild(sn, rd, tmp.child(d))
	tmp = st.setChild(tmp, d, sn)

	// copy node color to the tmp
	tmp.flags = (tmp.flags &^ flagBlack) | (sn.flags & flagBlack)
	sn.flags &^= flagBlack // set to red

	// it's important to update sn first as it may be the new right child of
	// tmp, and we need to update tmp.max too
	st.updateFingerprintAndMax(sn)
	st.updateFingerprintAndMax(tmp)

	return tmp
}

func (st *syncTree) doubleRotate(sn *syncTreeNode, d dir) *syncTreeNode {
	rd := d.flip()
	sn = st.setChild(sn, rd, st.rotate(sn.child(rd), rd))
	return st.rotate(sn, d)
}

func (st *syncTree) Add(k Ordered) {
	st.add(k, nil, false)
}

func (st *syncTree) Set(k Ordered, v any) {
	st.add(k, v, true)
}

func (st *syncTree) add(k Ordered, v any, set bool) {
	st.rootMtx.Lock()
	defer st.rootMtx.Unlock()
	st.root = st.insert(st.root, k, v, true, set)
	if st.root.flags&flagBlack == 0 {
		st.root = st.ensureCloned(st.root)
		st.root.flags |= flagBlack
	}
}

func (st *syncTree) insert(sn *syncTreeNode, k Ordered, v any, rb, set bool) *syncTreeNode {
	// simplified insert implementation idea from
	// https://zarif98sjs.github.io/blog/blog/redblacktree/
	if sn == nil {
		sn = st.newNode(k, v)
		// the new node is not really "cloned", but at this point it's
		// only present in this tree so we can safely modify it
		// without allocating new nodes
		sn.flags |= flagCloned
		// when the tree is being modified, cached min/max ptrs are no longer valid
		st.cachedMinPtr = nil
		st.cachedMaxPtr = nil
		return sn
	}
	c := k.Compare(sn.key)
	if c == 0 {
		if v != sn.value {
			sn = st.ensureCloned(sn)
			sn.value = v
		}
		return sn
	}
	d := left
	if c > 0 {
		d = right
	}
	oldChild := sn.child(d)
	newChild := st.insert(oldChild, k, v, rb, set)
	sn = st.setChild(sn, d, newChild)
	updateFP := true
	if rb {
		// non-red-black insert is used for testing
		sn, updateFP = st.insertFixup(sn, d, oldChild != newChild)
	}
	if updateFP {
		st.updateFingerprintAndMax(sn)
	}
	return sn
}

// insertFixup fixes a subtree after insert according to Red-Black tree rules.
// It returns the updated node and a boolean indicating whether the fingerprint/max
// update is needed. The latter is NOT the case
func (st *syncTree) insertFixup(sn *syncTreeNode, d dir, updateFP bool) (*syncTreeNode, bool) {
	child := sn.child(d)
	rd := d.flip()
	switch {
	case child.black():
		return sn, true
	case sn.child(rd).red():
		// both children of sn are red => any child has 2 reds in a row
		// (LL LR RR RL) => flip colors
		if child.child(d).red() || child.child(rd).red() {
			return st.flip(sn), true
		}
		return sn, true
	case child.child(d).red():
		// another child of sn is black
		// any child has 2 reds in a row (LL RR) => rotate
		// rotate will update fingerprint of sn and the node
		// that replaces it
		return st.rotate(sn, rd), updateFP
	case child.child(rd).red():
		// another child of sn is black
		// any child has 2 reds in a row (LR RL) => align first, then rotate
		// doubleRotate will update fingerprint of sn and the node
		// that replaces it
		return st.doubleRotate(sn, rd), updateFP
	default:
		return sn, true
	}
}

func (st *syncTree) Lookup(k Ordered) (any, bool) {
	// TODO: lookups shouldn't cause any allocation!
	ptr := st.rootPtr()
	if !st.findGTENode(ptr, k) || ptr.node == nil || ptr.Key().Compare(k) != 0 {
		return nil, false
	}
	return ptr.Value(), true
}

func (st *syncTree) findGTENode(ptr *syncTreePointer, x Ordered) bool {
	for {
		switch {
		case ptr.node == nil:
			return false
		case x.Compare(ptr.node.key) == 0:
			// Exact match
			return true
		case x.Compare(ptr.node.max) > 0:
			// All of this subtree is below v, maybe we can have
			// some luck with the parent node
			ptr.parent()
			st.findGTENode(ptr, x)
		case x.Compare(ptr.node.key) >= 0:
			// We're still below x (or at x, but allowEqual is
			// false), but given that we checked Max and saw that
			// this subtree has some keys that are greater than
			// or equal to x, we can find them on the right
			if ptr.node.right == nil {
				// sn.Max lied to us
				// TODO: QQQQQ: this bug is being hit
				panic("BUG: SyncTreeNode: x > sn.Max but no right branch")
			}
			// Avoid endless recursion in case of a bug
			if x.Compare(ptr.node.right.max) > 0 {
				// TODO: QQQQQ: this bug is being hit
				panic("BUG: SyncTreeNode: inconsistent Max on the right branch")
			}
			ptr.right()
		case ptr.node.left == nil || x.Compare(ptr.node.left.max) > 0:
			// The current node's key is greater than x and the
			// left branch is either empty or fully below x, so
			// the current node is what we were looking for
			return true
		default:
			// Some keys on the left branch are greater or equal
			// than x accordingto sn.Left.Max
			ptr.left()
		}
	}
}

func (st *syncTree) rangeFingerprint(preceding SyncTreePointer, start, end Ordered, stop FingerprintPredicate) (fp any, startPtr, endPtr *syncTreePointer) {
	if st.root == nil {
		return st.m.Identity(), nil, nil
	}
	var ptr *syncTreePointer
	if preceding == nil {
		ptr = st.rootPtr()
	} else {
		ptr = preceding.(*syncTreePointer)
	}

	minPtr := st.Min().(*syncTreePointer)
	acc := st.m.Identity()
	haveGTE := st.findGTENode(ptr, start)
	startPtr = ptr.clone()
	switch {
	case start.Compare(end) >= 0:
		// rollover range, which includes the case start == end
		// this includes 2 subranges:
		// [start, max_element] and [min_element, end)
		var stopped bool
		if haveGTE {
			acc, stopped = st.aggregateUntil(ptr, acc, start, UpperBound{}, stop)
		}

		if !stopped && end.Compare(minPtr.Key()) > 0 {
			ptr = minPtr.clone()
			acc, _ = st.aggregateUntil(ptr, acc, LowerBound{}, end, stop)
		}
	case haveGTE:
		// normal range, that is, start < end
		acc, _ = st.aggregateUntil(ptr, st.m.Identity(), start, end, stop)
	}

	if startPtr.node == nil {
		startPtr = minPtr.clone()
	}
	if ptr.node == nil {
		ptr = minPtr.clone()
	}

	return acc, startPtr, ptr
}

func (st *syncTree) RangeFingerprint(ptr SyncTreePointer, start, end Ordered, stop FingerprintPredicate) (fp any, startNode, endNode SyncTreePointer) {
	fp, startPtr, endPtr := st.rangeFingerprint(ptr, start, end, stop)
	switch {
	case startPtr == nil && endPtr == nil:
		// avoid wrapping nil in SyncTreePointer interface
		return fp, nil, nil
	case startPtr == nil || endPtr == nil:
		panic("BUG: can't have nil node just on one end")
	default:
		return fp, startPtr, endPtr
	}
}

func (st *syncTree) aggregateUntil(ptr *syncTreePointer, acc any, start, end Ordered, stop FingerprintPredicate) (fp any, stopped bool) {
	acc, stopped = st.aggregateUp(ptr, acc, start, end, stop)
	if ptr.node == nil || end.Compare(ptr.node.key) <= 0 || stopped {
		return acc, stopped
	}

	// fmt.Fprintf(os.Stderr, "QQQQQ: from aggregateUp: acc %q; ptr.node %q\n", acc, ptr.node.key)
	f := st.m.Op(acc, st.m.Fingerprint(ptr.node.key))
	if stop.Match(f) {
		return acc, true
	}
	ptr.right()
	return st.aggregateDown(ptr, f, end, stop)
}

// aggregateUp ascends from the left (lower) end of the range towards the LCA
// (lowest common ancestor) of nodes within the range [start,end).  Instead of
// descending from the root node, the LCA is determined by the way of checking
// whether the stored max subtree key is below or at the end or not, saving
// some extra tree traversal when processing the ascending ranges.
// On the way up, if the current node is within the range, we include the right
// subtree in the aggregation using its saved fingerprint, as it is guaranteed
// to lie with the range. When we happen to go up from the right branch, we can
// only reach a predecessor node that lies below the start, and in this case we
// don't include the right subtree in the aggregation to avoid aggregating the
// same subset of nodes twice.
// If stop function is passed, we find the node on which it returns true
// for the fingerprint accumulated between start and that node, if the target
// node is somewhere to the left from the LCA.
func (st *syncTree) aggregateUp(ptr *syncTreePointer, acc any, start, end Ordered, stop FingerprintPredicate) (fp any, stopped bool) {
	for {
		switch {
		case ptr.node == nil:
			// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateUp: null node\n")
			return acc, false
		case stop.Match(acc):
			// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateUp: stop: node %v acc %v\n", sn.key, acc)
			ptr.Prev()
			return acc, true
		case end.Compare(ptr.node.max) <= 0:
			// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateUp: LCA: node %v acc %v\n", sn.key, acc)
			// This node is a the LCA, the starting point for AggregateDown
			return acc, false
		case start.Compare(ptr.node.key) <= 0:
			// This node is within the target range
			// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateUp: in-range node %v acc %v\n", sn.key, acc)
			f := st.m.Op(acc, st.m.Fingerprint(ptr.node.key))
			if stop.Match(f) {
				// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateUp: stop at the own node %v acc %v\n", sn.key, acc)
				return acc, true
			}
			f1 := st.m.Op(f, st.safeFingerprint(ptr.node.right))
			if stop.Match(f1) {
				// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateUp: right subtree matches node %v acc %v f1 %v\n", sn.key, acc, f1)
				// The target node is somewhere in the right subtree
				if ptr.node.right == nil {
					panic("BUG: nil right child with non-identity fingerprint")
				}
				ptr.right()
				acc := st.boundedAggregate(ptr, f, stop)
				if ptr.node == nil {
					panic("BUG: aggregateUp: bad subtree fingerprint on the right branch")
				}
				// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateUp: right subtree: node %v acc %v\n", node.key, acc)
				return acc, true
			} else {
				// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateUp: no right subtree match: node %v acc %v f1 %v\n", sn.key, acc, f1)
				acc = f1
			}
		}
		ptr.parent()
	}
}

// aggregateDown descends from the LCA (lowest common ancestor) of nodes within
// the range ending at the 'end'. On the way down, the unvisited left subtrees
// are guaranteed to lie within the range, so they're included into the
// aggregation using their saved fingerprint.
// If stop function is passed, we find the node on which it returns true
// for the fingerprint accumulated between start and that node
func (st *syncTree) aggregateDown(ptr *syncTreePointer, acc any, end Ordered, stop FingerprintPredicate) (fp any, stopped bool) {
	for {
		switch {
		case ptr.node == nil:
			// fmt.Fprintf(os.Stderr, "QQQQQ: sn == nil\n")
			return acc, false
		case stop.Match(acc):
			// fmt.Fprintf(os.Stderr, "QQQQQ: stop on node\n")
			ptr.Prev()
			return acc, true
		case end.Compare(ptr.node.key) > 0:
			// fmt.Fprintf(os.Stderr, "QQQQQ: within the range\n")
			// We're within the range but there also may be nodes
			// within the range to the right. The left branch is
			// fully within the range
			f := st.m.Op(acc, st.safeFingerprint(ptr.node.left))
			if stop.Match(f) {
				// fmt.Fprintf(os.Stderr, "QQQQQ: left subtree covers it\n")
				// The target node is somewhere in the left subtree
				if ptr.node.left == nil {
					panic("BUG: aggregateDown: nil left child with non-identity fingerprint")
				}
				ptr.left()
				return st.boundedAggregate(ptr, acc, stop), true
			}
			f1 := st.m.Op(f, st.m.Fingerprint(ptr.node.key))
			if stop.Match(f1) {
				// fmt.Fprintf(os.Stderr, "QQQQQ: stop at the node, prev %#v\n", node.prev())
				return f, true
			} else {
				acc = f1
			}
			// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateDown on the right\n")
			ptr.right()
		case ptr.node.left == nil || end.Compare(ptr.node.left.max) > 0:
			// fmt.Fprintf(os.Stderr, "QQQQQ: node covers the range\n")
			// Found the rightmost bounding node
			f := st.m.Op(acc, st.safeFingerprint(ptr.node.left))
			if stop.Match(f) {
				// The target node is somewhere in the left subtree
				if ptr.node.left == nil {
					panic("BUG: aggregateDown: nil left child with non-identity fingerprint")
				}
				// XXXXX fixme
				ptr.left()
				return st.boundedAggregate(ptr, acc, stop), true
			}
			return f, false
		default:
			// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateDown: going further down\n")
			// We're too far to the right, outside the range
			ptr.left()
		}
	}
}

func (st *syncTree) boundedAggregate(ptr *syncTreePointer, acc any, stop FingerprintPredicate) any {
	for {
		// fmt.Fprintf(os.Stderr, "QQQQQ: boundedAggregate: node %v, acc %v\n", sn.key, acc)
		if ptr.node == nil {
			return acc
		}

		// If we don't need to stop, or if the stop point is somewhere after
		// this subtree, we can just use the pre-calculated subtree fingerprint
		if f := st.m.Op(acc, ptr.node.fingerprint); !stop.Match(f) {
			return f
		}

		// This function is not supposed to be called with acc already matching
		// the stop condition
		if stop.Match(acc) {
			panic("BUG: boundedAggregate: initial fingerprint is matched before the first node")
		}

		if ptr.node.left != nil {
			// See if we can skip recursion on the left branch
			f := st.m.Op(acc, ptr.node.left.fingerprint)
			if !stop.Match(f) {
				// fmt.Fprintf(os.Stderr, "QQQQQ: boundedAggregate: sn Left non-nil and no-stop %v, f %v, left fingerprint %v\n", sn.key, f, sn.Left.Fingerprint)
				acc = f
			} else {
				// The target node must be contained in the left subtree
				ptr.left()
				// fmt.Fprintf(os.Stderr, "QQQQQ: boundedAggregate: sn Left non-nil and stop %v, new node %v, acc %v\n", sn.key, node.key, acc)
				continue
			}
		}

		f := st.m.Op(acc, st.m.Fingerprint(ptr.node.key))
		if stop.Match(f) {
			return acc
		}
		acc = f

		if ptr.node.right != nil {
			f1 := st.m.Op(f, ptr.node.right.fingerprint)
			if !stop.Match(f1) {
				// The right branch is still below the target fingerprint
				// fmt.Fprintf(os.Stderr, "QQQQQ: boundedAggregate: sn Right non-nil and no-stop %v, acc %v\n", sn.key, acc)
				acc = f1
			} else {
				// The target node must be contained in the right subtree
				acc = f
				ptr.right()
				// fmt.Fprintf(os.Stderr, "QQQQQ: boundedAggregate: sn Right non-nil and stop %v, new node %v, acc %v\n", sn.key, node.key, acc)
				continue
			}
		}
		// fmt.Fprintf(os.Stderr, "QQQQQ: boundedAggregate: %v -- return acc %v\n", sn.key, acc)
		return acc
	}
}

func (st *syncTree) Dump() string {
	if st.root == nil {
		return "<empty>"
	}
	var sb strings.Builder
	st.root.dump(&sb, 0)
	return sb.String()
}

// TODO: use sync.Pool for node alloc
//       see also:
//         https://www.akshaydeo.com/blog/2017/12/23/How-did-I-improve-latency-by-700-percent-using-syncPool/
//       so may need refcounting
