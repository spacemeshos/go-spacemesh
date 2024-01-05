// TBD: add paper ref
package hashsync

import (
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
)

type Ordered interface {
	Compare(other Ordered) int
}

type LowerBound struct{}

var _ Ordered = LowerBound{}

func (vb LowerBound) Compare(x Ordered) int { return -1 }

type UpperBound struct{}

var _ Ordered = UpperBound{}

func (vb UpperBound) Compare(x Ordered) int { return 1 }

type FingerprintPredicate func(fp any) bool

func (fpred FingerprintPredicate) Match(y any) bool {
	return fpred != nil && fpred(y)
}

type MonoidTree interface {
	Copy() MonoidTree
	Fingerprint() any
	Add(v Ordered)
	Min() MonoidTreePointer
	Max() MonoidTreePointer
	RangeFingerprint(ptr MonoidTreePointer, start, end Ordered, stop FingerprintPredicate) (fp any, startNode, endNode MonoidTreePointer)
	Dump() string
}

func MonoidTreeFromSortedSlice[T Ordered](m Monoid, items []T) MonoidTree {
	s := make([]Ordered, len(items))
	for n, item := range items {
		s[n] = item
	}
	mt := NewMonoidTree(m).(*monoidTree)
	mt.root = mt.buildFromSortedSlice(nil, s)
	return mt
}

func MonoidTreeFromSlice[T Ordered](m Monoid, items []T) MonoidTree {
	sorted := make([]T, len(items))
	copy(sorted, items)
	slices.SortFunc(sorted, func(a, b T) int {
		return a.Compare(b)
	})
	return MonoidTreeFromSortedSlice(m, items)
}

type MonoidTreePointer interface {
	Equal(other MonoidTreePointer) bool
	Key() Ordered
	Prev()
	Next()
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

type monoidTreePointer struct {
	parentStack []*monoidTreeNode
	node        *monoidTreeNode
}

var _ MonoidTreePointer = &monoidTreePointer{}

func (p *monoidTreePointer) clone() *monoidTreePointer {
	// TODO: copy node stack
	r := &monoidTreePointer{
		parentStack: make([]*monoidTreeNode, len(p.parentStack), cap(p.parentStack)),
		node:        p.node,
	}
	copy(r.parentStack, p.parentStack)
	return r
}

func (p *monoidTreePointer) parent() {
	n := len(p.parentStack)
	if n == 0 {
		p.node = nil
	} else {
		n--
		p.node = p.parentStack[n]
		p.parentStack = p.parentStack[:n]
	}
}

func (p *monoidTreePointer) left() {
	if p.node != nil {
		p.parentStack = append(p.parentStack, p.node)
		p.node = p.node.left
	}
}

func (p *monoidTreePointer) right() {
	if p.node != nil {
		p.parentStack = append(p.parentStack, p.node)
		p.node = p.node.right
	}
}

func (p *monoidTreePointer) min() {
	for {
		switch {
		case p.node == nil || p.node.left == nil:
			return
		default:
			p.left()
		}
	}
}

func (p *monoidTreePointer) max() {
	for {
		switch {
		case p.node == nil || p.node.right == nil:
			return
		default:
			p.right()
		}
	}
}

func (p *monoidTreePointer) Equal(other MonoidTreePointer) bool {
	if other == nil {
		return p.node == nil
	}
	return p.node == other.(*monoidTreePointer).node
}

func (p *monoidTreePointer) Prev() {
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

func (p *monoidTreePointer) Next() {
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

func (p *monoidTreePointer) Key() Ordered {
	if p.node == nil {
		return nil
	}
	return p.node.key
}

type monoidTreeNode struct {
	left        *monoidTreeNode
	right       *monoidTreeNode
	key         Ordered
	max         Ordered
	fingerprint any
	flags       flags
}

func (mn *monoidTreeNode) red() bool {
	return mn != nil && (mn.flags&flagBlack) == 0
}

func (mn *monoidTreeNode) black() bool {
	return mn == nil || (mn.flags&flagBlack) != 0
}

func (mn *monoidTreeNode) child(dir dir) *monoidTreeNode {
	if mn == nil {
		return nil
	}
	if dir == left {
		return mn.left
	}
	return mn.right
}

func (mn *monoidTreeNode) Key() Ordered { return mn.key }

func (mn *monoidTreeNode) dump(w io.Writer, indent int) {
	indentStr := strings.Repeat("  ", indent)
	fmt.Fprintf(w, "%skey: %v\n", indentStr, mn.key)
	fmt.Fprintf(w, "%smax: %v\n", indentStr, mn.max)
	fmt.Fprintf(w, "%sfp: %v\n", indentStr, mn.fingerprint)
	color := "red"
	if mn.black() {
		color = "black"
	}
	fmt.Fprintf(w, "%scolor: %v\n", indentStr, color)
	if mn.left != nil {
		fmt.Fprintf(w, "%sleft:\n", indentStr)
		mn.left.dump(w, indent+1)
		if mn.left.key.Compare(mn.key) >= 0 {
			fmt.Fprintf(w, "%sERROR: left key >= parent key\n", indentStr)
		}
	}
	if mn.right != nil {
		fmt.Fprintf(w, "%sright:\n", indentStr)
		mn.right.dump(w, indent+1)
		if mn.right.key.Compare(mn.key) <= 0 {
			fmt.Fprintf(w, "%sERROR: right key <= parent key\n", indentStr)
		}
	}
}

func (mn *monoidTreeNode) dumpSubtree() string {
	var sb strings.Builder
	mn.dump(&sb, 0)
	return sb.String()
}

// cleanNodes removed flagCloned from all of the nodes in the subtree,
// so that it can be used in further cloned trees.
// A non-cloned node cannot have any cloned children, so the function
// stops the recursion at any non-cloned node.
func (mn *monoidTreeNode) cleanCloned() {
	if mn == nil || mn.flags&flagCloned == 0 {
		return
	}
	mn.flags &^= flagCloned
	mn.left.cleanCloned()
	mn.right.cleanCloned()
}

type monoidTree struct {
	m            Monoid
	root         *monoidTreeNode
	cachedMinPtr *monoidTreePointer
	cachedMaxPtr *monoidTreePointer
}

func NewMonoidTree(m Monoid) MonoidTree {
	return &monoidTree{m: m}
}

func (mt *monoidTree) Copy() MonoidTree {
	// Clean flagCloned from any nodes created specifically
	// for this subtree. This will mean they will have to be
	// re-cloned if they need to be changed again.
	mt.root.cleanCloned()
	// Don't reuse cachedMinPtr / cachedMaxPtr for the cloned
	// tree to be on the safe side
	return &monoidTree{
		m:    mt.m,
		root: mt.root,
	}
}

func (mt *monoidTree) rootPtr() *monoidTreePointer {
	return &monoidTreePointer{
		parentStack: make([]*monoidTreeNode, 0, initialParentStackSize),
		node:        mt.root,
	}
}

func (mt *monoidTree) ensureCloned(mn *monoidTreeNode) *monoidTreeNode {
	if mn.flags&flagCloned != 0 {
		return mn
	}
	cloned := *mn
	cloned.flags |= flagCloned
	return &cloned
}

func (mt *monoidTree) setChild(mn *monoidTreeNode, dir dir, child *monoidTreeNode) *monoidTreeNode {
	if mn == nil {
		panic("setChild for a nil node")
	}
	if mn.child(dir) == child {
		return mn
	}
	mn = mt.ensureCloned(mn)
	if dir == left {
		mn.left = child
	} else {
		mn.right = child
	}
	return mn
}

func (mt *monoidTree) flip(mn *monoidTreeNode) *monoidTreeNode {
	if mn.left == nil || mn.right == nil {
		panic("can't flip color with one or more nil children")
	}

	left := mt.ensureCloned(mn.left)
	right := mt.ensureCloned(mn.right)
	mn = mt.ensureCloned(mn)
	mn.left = left
	mn.right = right

	mn.flags ^= flagBlack
	left.flags ^= flagBlack
	right.flags ^= flagBlack
	return mn
}

func (mt *monoidTree) Min() MonoidTreePointer {
	if mt.root == nil {
		return nil
	}
	if mt.cachedMinPtr == nil {
		mt.cachedMinPtr = mt.rootPtr()
		mt.cachedMinPtr.min()
	}
	if mt.cachedMinPtr.node == nil {
		panic("BUG: no minNode in a non-empty tree")
	}
	return mt.cachedMinPtr.clone()
}

func (mt *monoidTree) Max() MonoidTreePointer {
	if mt.root == nil {
		return nil
	}
	if mt.cachedMaxPtr == nil {
		mt.cachedMaxPtr = mt.rootPtr()
		mt.cachedMaxPtr.max()
	}
	if mt.cachedMaxPtr.node == nil {
		panic("BUG: no maxNode in a non-empty tree")
	}
	return mt.cachedMaxPtr.clone()
}

func (mt *monoidTree) Fingerprint() any {
	if mt.root == nil {
		return mt.m.Identity()
	}
	return mt.root.fingerprint
}

func (mt *monoidTree) newNode(parent *monoidTreeNode, v Ordered) *monoidTreeNode {
	return &monoidTreeNode{
		key:         v,
		max:         v,
		fingerprint: mt.m.Fingerprint(v),
	}
}

func (mt *monoidTree) buildFromSortedSlice(parent *monoidTreeNode, s []Ordered) *monoidTreeNode {
	switch len(s) {
	case 0:
		return nil
	case 1:
		return mt.newNode(nil, s[0])
	}
	middle := len(s) / 2
	node := mt.newNode(parent, s[middle])
	node.left = mt.buildFromSortedSlice(node, s[:middle])
	node.right = mt.buildFromSortedSlice(node, s[middle+1:])
	if node.left != nil {
		node.fingerprint = mt.m.Op(node.left.fingerprint, node.fingerprint)
	}
	if node.right != nil {
		node.fingerprint = mt.m.Op(node.fingerprint, node.right.fingerprint)
		node.max = node.right.max
	}
	return node
}

func (mt *monoidTree) safeFingerprint(mn *monoidTreeNode) any {
	if mn == nil {
		return mt.m.Identity()
	}
	return mn.fingerprint
}

func (mt *monoidTree) updateFingerprintAndMax(mn *monoidTreeNode) {
	fp := mt.m.Op(mt.safeFingerprint(mn.left), mt.m.Fingerprint(mn.key))
	fp = mt.m.Op(fp, mt.safeFingerprint(mn.right))
	newMax := mn.key
	if mn.right != nil {
		newMax = mn.right.max
	}
	if mn.flags&flagCloned == 0 &&
		(!reflect.DeepEqual(mn.fingerprint, fp) || mn.max.Compare(newMax) != 0) {
		panic("BUG: updating fingerprint/max for a non-cloned node")
	}
	mn.fingerprint = fp
	mn.max = newMax
}

func (mt *monoidTree) rotate(mn *monoidTreeNode, d dir) *monoidTreeNode {
	// mn.verify()

	rd := d.flip()
	tmp := mn.child(rd)
	if tmp == nil {
		panic("BUG: nil parent after rotate")
	}
	// fmt.Fprintf(os.Stderr, "QQQQQ: rotate %s (child at %s is %s): subtree:\n%s\n",
	// 	d, rd, tmp.key, mn.dumpSubtree())
	mn = mt.setChild(mn, rd, tmp.child(d))
	tmp = mt.setChild(tmp, d, mn)

	// copy node color to the tmp
	tmp.flags = (tmp.flags &^ flagBlack) | (mn.flags & flagBlack)
	mn.flags &^= flagBlack // set to red

	// it's important to update mn first as it may be the new right child of
	// tmp, and we need to update tmp.max too
	mt.updateFingerprintAndMax(mn)
	mt.updateFingerprintAndMax(tmp)

	return tmp
}

func (mt *monoidTree) doubleRotate(mn *monoidTreeNode, d dir) *monoidTreeNode {
	rd := d.flip()
	mn = mt.setChild(mn, rd, mt.rotate(mn.child(rd), rd))
	return mt.rotate(mn, d)
}

func (mt *monoidTree) Add(v Ordered) {
	mt.root = mt.insert(mt.root, v, true)
	if mt.root.flags&flagBlack == 0 {
		mt.root = mt.ensureCloned(mt.root)
		mt.root.flags |= flagBlack
	}
}

func (mt *monoidTree) insert(mn *monoidTreeNode, v Ordered, rb bool) *monoidTreeNode {
	// simplified insert implementation idea from
	// https://zarif98sjs.github.io/blog/blog/redblacktree/
	if mn == nil {
		mn = mt.newNode(nil, v)
		// the new node is not really "cloned", but at this point it's
		// only present in this tree so we can safely modify it
		// without allocating new nodes
		mn.flags |= flagCloned
		// when the tree is being modified, cached min/max ptrs are no longer valid
		mt.cachedMinPtr = nil
		mt.cachedMaxPtr = nil
		return mn
	}
	c := v.Compare(mn.key)
	if c == 0 {
		return mn
	}
	d := left
	if c > 0 {
		d = right
	}
	oldChild := mn.child(d)
	newChild := mt.insert(oldChild, v, rb)
	mn = mt.setChild(mn, d, newChild)
	updateFP := true
	if rb {
		// non-red-black insert is used for testing
		mn, updateFP = mt.insertFixup(mn, d, oldChild != newChild)
	}
	if updateFP {
		mt.updateFingerprintAndMax(mn)
	}
	return mn
}

// insertFixup fixes a subtree after insert according to Red-Black tree rules.
// It returns the updated node and a boolean indicating whether the fingerprint/max
// update is needed. The latter is NOT the case
func (mt *monoidTree) insertFixup(mn *monoidTreeNode, d dir, updateFP bool) (*monoidTreeNode, bool) {
	child := mn.child(d)
	rd := d.flip()
	switch {
	case child.black():
		return mn, true
	case mn.child(rd).red():
		// both children of mn are red => any child has 2 reds in a row
		// (LL LR RR RL) => flip colors
		if child.child(d).red() || child.child(rd).red() {
			return mt.flip(mn), true
		}
		return mn, true
	case child.child(d).red():
		// another child of mn is black
		// any child has 2 reds in a row (LL RR) => rotate
		// rotate will update fingerprint of mn and the node
		// that replaces it
		return mt.rotate(mn, rd), updateFP
	case child.child(rd).red():
		// another child of mn is black
		// any child has 2 reds in a row (LR RL) => align first, then rotate
		// doubleRotate will update fingerprint of mn and the node
		// that replaces it
		return mt.doubleRotate(mn, rd), updateFP
	default:
		return mn, true
	}
}

func (mt *monoidTree) findGTENode(ptr *monoidTreePointer, x Ordered) bool {
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
			mt.findGTENode(ptr, x)
		case x.Compare(ptr.node.key) >= 0:
			// We're still below x (or at x, but allowEqual is
			// false), but given that we checked Max and saw that
			// this subtree has some keys that are greater than
			// or equal to x, we can find them on the right
			if ptr.node.right == nil {
				// mn.Max lied to us
				panic("BUG: MonoidTreeNode: x > mn.Max but no right branch")
			}
			// Avoid endless recursion in case of a bug
			if x.Compare(ptr.node.right.max) > 0 {
				panic("BUG: MonoidTreeNode: inconsistent Max on the right branch")
			}
			ptr.right()
		case ptr.node.left == nil || x.Compare(ptr.node.left.max) > 0:
			// The current node's key is greater than x and the
			// left branch is either empty or fully below x, so
			// the current node is what we were looking for
			return true
		default:
			// Some keys on the left branch are greater or equal
			// than x accordingto mn.Left.Max
			ptr.left()
		}
	}
}

func (mt *monoidTree) rangeFingerprint(preceding MonoidTreePointer, start, end Ordered, stop FingerprintPredicate) (fp any, startPtr, endPtr *monoidTreePointer) {
	if mt.root == nil {
		return mt.m.Identity(), nil, nil
	}
	var ptr *monoidTreePointer
	if preceding == nil {
		ptr = mt.rootPtr()
	} else {
		ptr = preceding.(*monoidTreePointer)
	}

	minPtr := mt.Min().(*monoidTreePointer)
	acc := mt.m.Identity()
	haveGTE := mt.findGTENode(ptr, start)
	startPtr = ptr.clone()
	switch {
	case start.Compare(end) >= 0:
		// rollover range, which includes the case start == end
		// this includes 2 subranges:
		// [start, max_element] and [min_element, end)
		var stopped bool
		if haveGTE {
			acc, stopped = mt.aggregateUntil(ptr, acc, start, UpperBound{}, stop)
		}

		if !stopped && end.Compare(minPtr.Key()) > 0 {
			ptr = minPtr.clone()
			acc, _ = mt.aggregateUntil(ptr, acc, LowerBound{}, end, stop)
		}
	case haveGTE:
		// normal range, that is, start < end
		acc, _ = mt.aggregateUntil(ptr, mt.m.Identity(), start, end, stop)
	}

	if startPtr.node == nil {
		startPtr = minPtr.clone()
	}
	if ptr.node == nil {
		ptr = minPtr.clone()
	}

	return acc, startPtr, ptr
}

func (mt *monoidTree) RangeFingerprint(ptr MonoidTreePointer, start, end Ordered, stop FingerprintPredicate) (fp any, startNode, endNode MonoidTreePointer) {
	fp, startPtr, endPtr := mt.rangeFingerprint(ptr, start, end, stop)
	switch {
	case startPtr == nil && endPtr == nil:
		// avoid wrapping nil in MonoidTreePointer interface
		return fp, nil, nil
	case startPtr == nil || endPtr == nil:
		panic("BUG: can't have nil node just on one end")
	default:
		return fp, startPtr, endPtr
	}
}

func (mt *monoidTree) aggregateUntil(ptr *monoidTreePointer, acc any, start, end Ordered, stop FingerprintPredicate) (fp any, stopped bool) {
	acc, stopped = mt.aggregateUp(ptr, acc, start, end, stop)
	if ptr.node == nil || end.Compare(ptr.node.key) <= 0 || stopped {
		return acc, stopped
	}

	// fmt.Fprintf(os.Stderr, "QQQQQ: from aggregateUp: acc %q; ptr.node %q\n", acc, ptr.node.key)
	f := mt.m.Op(acc, mt.m.Fingerprint(ptr.node.key))
	if stop.Match(f) {
		return acc, true
	}
	ptr.right()
	return mt.aggregateDown(ptr, f, end, stop)
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
func (mt *monoidTree) aggregateUp(ptr *monoidTreePointer, acc any, start, end Ordered, stop FingerprintPredicate) (fp any, stopped bool) {
	for {
		switch {
		case ptr.node == nil:
			// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateUp: null node\n")
			return acc, false
		case stop.Match(acc):
			// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateUp: stop: node %v acc %v\n", mn.key, acc)
			ptr.Prev()
			return acc, true
		case end.Compare(ptr.node.max) <= 0:
			// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateUp: LCA: node %v acc %v\n", mn.key, acc)
			// This node is a the LCA, the starting point for AggregateDown
			return acc, false
		case start.Compare(ptr.node.key) <= 0:
			// This node is within the target range
			// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateUp: in-range node %v acc %v\n", mn.key, acc)
			f := mt.m.Op(acc, mt.m.Fingerprint(ptr.node.key))
			if stop.Match(f) {
				// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateUp: stop at the own node %v acc %v\n", mn.key, acc)
				return acc, true
			}
			f1 := mt.m.Op(f, mt.safeFingerprint(ptr.node.right))
			if stop.Match(f1) {
				// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateUp: right subtree matches node %v acc %v f1 %v\n", mn.key, acc, f1)
				// The target node is somewhere in the right subtree
				if ptr.node.right == nil {
					panic("BUG: nil right child with non-identity fingerprint")
				}
				ptr.right()
				acc := mt.boundedAggregate(ptr, f, stop)
				if ptr.node == nil {
					panic("BUG: aggregateUp: bad subtree fingerprint on the right branch")
				}
				// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateUp: right subtree: node %v acc %v\n", node.key, acc)
				return acc, true
			} else {
				// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateUp: no right subtree match: node %v acc %v f1 %v\n", mn.key, acc, f1)
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
func (mt *monoidTree) aggregateDown(ptr *monoidTreePointer, acc any, end Ordered, stop FingerprintPredicate) (fp any, stopped bool) {
	for {
		switch {
		case ptr.node == nil:
			// fmt.Fprintf(os.Stderr, "QQQQQ: mn == nil\n")
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
			f := mt.m.Op(acc, mt.safeFingerprint(ptr.node.left))
			if stop.Match(f) {
				// fmt.Fprintf(os.Stderr, "QQQQQ: left subtree covers it\n")
				// The target node is somewhere in the left subtree
				if ptr.node.left == nil {
					panic("BUG: aggregateDown: nil left child with non-identity fingerprint")
				}
				ptr.left()
				return mt.boundedAggregate(ptr, acc, stop), true
			}
			f1 := mt.m.Op(f, mt.m.Fingerprint(ptr.node.key))
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
			f := mt.m.Op(acc, mt.safeFingerprint(ptr.node.left))
			if stop.Match(f) {
				// The target node is somewhere in the left subtree
				if ptr.node.left == nil {
					panic("BUG: aggregateDown: nil left child with non-identity fingerprint")
				}
				// XXXXX fixme
				ptr.left()
				return mt.boundedAggregate(ptr, acc, stop), true
			}
			return f, false
		default:
			// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateDown: going further down\n")
			// We're too far to the right, outside the range
			ptr.left()
		}
	}
}

func (mt *monoidTree) boundedAggregate(ptr *monoidTreePointer, acc any, stop FingerprintPredicate) any {
	for {
		// fmt.Fprintf(os.Stderr, "QQQQQ: boundedAggregate: node %v, acc %v\n", mn.key, acc)
		if ptr.node == nil {
			return acc
		}

		// If we don't need to stop, or if the stop point is somewhere after
		// this subtree, we can just use the pre-calculated subtree fingerprint
		if f := mt.m.Op(acc, ptr.node.fingerprint); !stop.Match(f) {
			return f
		}

		// This function is not supposed to be called with acc already matching
		// the stop condition
		if stop.Match(acc) {
			panic("BUG: boundedAggregate: initial fingerprint is matched before the first node")
		}

		if ptr.node.left != nil {
			// See if we can skip recursion on the left branch
			f := mt.m.Op(acc, ptr.node.left.fingerprint)
			if !stop.Match(f) {
				// fmt.Fprintf(os.Stderr, "QQQQQ: boundedAggregate: mn Left non-nil and no-stop %v, f %v, left fingerprint %v\n", mn.key, f, mn.Left.Fingerprint)
				acc = f
			} else {
				// The target node must be contained in the left subtree
				ptr.left()
				// fmt.Fprintf(os.Stderr, "QQQQQ: boundedAggregate: mn Left non-nil and stop %v, new node %v, acc %v\n", mn.key, node.key, acc)
				continue
			}
		}

		f := mt.m.Op(acc, mt.m.Fingerprint(ptr.node.key))
		if stop.Match(f) {
			return acc
		}
		acc = f

		if ptr.node.right != nil {
			f1 := mt.m.Op(f, ptr.node.right.fingerprint)
			if !stop.Match(f1) {
				// The right branch is still below the target fingerprint
				// fmt.Fprintf(os.Stderr, "QQQQQ: boundedAggregate: mn Right non-nil and no-stop %v, acc %v\n", mn.key, acc)
				acc = f1
			} else {
				// The target node must be contained in the right subtree
				acc = f
				ptr.right()
				// fmt.Fprintf(os.Stderr, "QQQQQ: boundedAggregate: mn Right non-nil and stop %v, new node %v, acc %v\n", mn.key, node.key, acc)
				continue
			}
		}
		// fmt.Fprintf(os.Stderr, "QQQQQ: boundedAggregate: %v -- return acc %v\n", mn.key, acc)
		return acc
	}
}

func (mt *monoidTree) Dump() string {
	if mt.root == nil {
		return "<empty>"
	}
	var sb strings.Builder
	mt.root.dump(&sb, 0)
	return sb.String()
}

// TBD: !!! values and Lookup (via findGTENode) !!!
// TODO: rename MonoidTreeNode to just Node, MonoidTree to SyncTree
// TODO: use sync.Pool for node alloc
//       see also:
//         https://www.akshaydeo.com/blog/2017/12/23/How-did-I-improve-latency-by-700-percent-using-syncPool/
//       so may need refcounting
