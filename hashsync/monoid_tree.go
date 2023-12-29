// TBD: add paper ref
package hashsync

import (
	"fmt"
	"io"
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
	Fingerprint() any
	Add(v Ordered)
	Min() MonoidTreeNode
	Max() MonoidTreeNode
	RangeFingerprint(node MonoidTreeNode, start, end Ordered, stop FingerprintPredicate) (fp any, startNode, endNode MonoidTreeNode)
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

type MonoidTreeNode interface {
	Key() Ordered
	Prev() MonoidTreeNode
	Next() MonoidTreeNode
}

type color uint8

const (
	red   color = 0
	black color = 1
)

func (c color) flip() color { return c ^ 1 }

func (c color) String() string {
	switch c {
	case red:
		return "red"
	case black:
		return "black"
	default:
		return fmt.Sprintf("<bad: %d>", c)
	}
}

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

type monoidTreeNode struct {
	parent      *monoidTreeNode
	left        *monoidTreeNode
	right       *monoidTreeNode
	key         Ordered
	max         Ordered
	fingerprint any
	color       color
}

func (mn *monoidTreeNode) red() bool {
	return mn != nil && mn.color == red
}

func (mn *monoidTreeNode) black() bool {
	return mn == nil || mn.color == black
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

func (mn *monoidTreeNode) setChild(dir dir, child *monoidTreeNode) {
	if mn == nil {
		panic("setChild for a nil node")
	}
	if dir == left {
		mn.left = child
	} else {
		mn.right = child
	}
	if child != nil {
		child.parent = mn
	}
}

func (mn *monoidTreeNode) flip() {
	if mn.left == nil || mn.right == nil {
		panic("can't flip color with one or more nil children")
	}
	mn.color = mn.color.flip()
	mn.left.color = mn.left.color.flip()
	mn.right.color = mn.right.color.flip()
}

func (mn *monoidTreeNode) Key() Ordered { return mn.key }

func (mn *monoidTreeNode) minNode() *monoidTreeNode {
	if mn.left == nil {
		return mn
	}
	return mn.left.minNode()
}

func (mn *monoidTreeNode) maxNode() *monoidTreeNode {
	if mn.right == nil {
		return mn
	}
	return mn.right.maxNode()
}

func (mn *monoidTreeNode) prev() *monoidTreeNode {
	switch {
	case mn == nil:
		return nil
	case mn.left != nil:
		return mn.left.maxNode()
	default:
		p := mn.parent
		for p != nil && mn == p.left {
			mn = p
			p = p.parent
		}
		return p
	}
}

func (mn *monoidTreeNode) next() *monoidTreeNode {
	switch {
	case mn == nil:
		return nil
	case mn.right != nil:
		return mn.right.minNode()
	default:
		p := mn.parent
		for p != nil && mn == p.right {
			mn = p
			p = p.parent
		}
		return p
	}
}

func (mn *monoidTreeNode) rmmeStr() string {
	if mn == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%s", mn.key)
}

func (mn *monoidTreeNode) Prev() MonoidTreeNode {
	if prev := mn.prev(); prev != nil {
		return prev
	}
	return nil
}

func (mn *monoidTreeNode) Next() MonoidTreeNode {
	if next := mn.next(); next != nil {
		return next
	}
	return nil
}

func (mn *monoidTreeNode) dump(w io.Writer, indent int) {
	indentStr := strings.Repeat("  ", indent)
	fmt.Fprintf(w, "%skey: %v\n", indentStr, mn.key)
	fmt.Fprintf(w, "%smax: %v\n", indentStr, mn.max)
	fmt.Fprintf(w, "%sfp: %v\n", indentStr, mn.fingerprint)
	if mn.left != nil {
		fmt.Fprintf(w, "%sleft:\n", indentStr)
		mn.left.dump(w, indent+1)
		if mn.left.parent != mn {
			fmt.Fprintf(w, "%sERROR: bad parent on the left\n", indentStr)
		}
		if mn.left.key.Compare(mn.key) >= 0 {
			fmt.Fprintf(w, "%sERROR: left key >= parent key\n", indentStr)
		}
	}
	if mn.right != nil {
		fmt.Fprintf(w, "%sright:\n", indentStr)
		mn.right.dump(w, indent+1)
		if mn.right.parent != mn {
			fmt.Fprintf(w, "%sERROR: bad parent on the right\n", indentStr)
		}
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

type monoidTree struct {
	m             Monoid
	root          *monoidTreeNode
	cachedMinNode *monoidTreeNode
	cachedMaxNode *monoidTreeNode
}

func NewMonoidTree(m Monoid) MonoidTree {
	return &monoidTree{m: m}
}

func (mt *monoidTree) Min() MonoidTreeNode {
	if mt.root == nil {
		return nil
	}
	if mt.cachedMinNode == nil {
		mt.cachedMinNode = mt.root.minNode()
	}
	if mt.cachedMinNode == nil {
		panic("BUG: no minNode in a non-empty tree")
	}
	return mt.cachedMinNode
}

func (mt *monoidTree) Max() MonoidTreeNode {
	if mt.root == nil {
		return nil
	}
	if mt.cachedMaxNode == nil {
		mt.cachedMaxNode = mt.root.maxNode()
	}
	if mt.cachedMaxNode == nil {
		panic("BUG: no maxNode in a non-empty tree")
	}
	return mt.cachedMaxNode
}

func (mt *monoidTree) Fingerprint() any {
	if mt.root == nil {
		return mt.m.Identity()
	}
	return mt.root.fingerprint
}

func (mt *monoidTree) newNode(parent *monoidTreeNode, v Ordered) *monoidTreeNode {
	return &monoidTreeNode{
		parent:      parent,
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
		node.left.parent = node
		node.fingerprint = mt.m.Op(node.left.fingerprint, node.fingerprint)
	}
	if node.right != nil {
		node.right.parent = node
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
	mn.fingerprint = mt.m.Op(fp, mt.safeFingerprint(mn.right))
	if mn.right != nil {
		mn.max = mn.right.max
	} else {
		mn.max = mn.key
	}
}

func (mt *monoidTree) rotate(mn *monoidTreeNode, d dir) *monoidTreeNode {
	// mn.verify()

	rd := d.flip()
	tmp := mn.child(rd)
	// fmt.Fprintf(os.Stderr, "QQQQQ: rotate %s (child at %s is %s): subtree:\n%s\n",
	// 	d, rd, tmp.key, mn.dumpSubtree())
	mn.setChild(rd, tmp.child(d))
	tmp.parent = mn.parent
	tmp.setChild(d, mn)

	tmp.color = mn.color
	mn.color = red

	// it's important to update mn first as it may be the new right child of
	// tmp, and we need to update tmp.max too
	mt.updateFingerprintAndMax(mn)
	mt.updateFingerprintAndMax(tmp)

	return tmp
}

func (mt *monoidTree) doubleRotate(mn *monoidTreeNode, d dir) *monoidTreeNode {
	rd := d.flip()
	mn.setChild(rd, mt.rotate(mn.child(rd), rd))
	return mt.rotate(mn, d)
}

func (mt *monoidTree) Add(v Ordered) {
	mt.root = mt.insert(mt.root, v, true)
	mt.root.color = black
}

func (mt *monoidTree) insert(mn *monoidTreeNode, v Ordered, rb bool) *monoidTreeNode {
	// simplified insert implementation idea from
	// https://zarif98sjs.github.io/blog/blog/redblacktree/
	if mn == nil {
		mn = mt.newNode(nil, v)
		if mt.cachedMinNode != nil && v.Compare(mt.cachedMinNode.key) < 0 {
			mt.cachedMinNode = mn
		}
		if mt.cachedMaxNode != nil && v.Compare(mt.cachedMaxNode.key) > 0 {
			mt.cachedMaxNode = mn
		}
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
	mn.setChild(d, newChild)
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

func (mt *monoidTree) insertFixup(mn *monoidTreeNode, d dir, updateFP bool) (*monoidTreeNode, bool) {
	child := mn.child(d)
	rd := d.flip()
	switch {
	case child.black():
		return mn, true
	case mn.child(rd).red():
		updateFP = true
		// both children of mn are red => any child has 2 reds in a row
		// (LL LR RR RL) => flip colors
		if child.child(d).red() || child.child(rd).red() {
			mn.flip()
		}
	case child.child(d).red():
		// another child of mn is black
		// any child has 2 reds in a row (LL RR) => rotate
		// rotate will update fingerprint of mn and the node
		// that replaces it
		mn = mt.rotate(mn, rd)
	case child.child(rd).red():
		// another child of mn is black
		// any child has 2 reds in a row (LR RL) => align first, then rotate
		// doubleRotate will update fingerprint of mn and the node
		// that replaces it
		mn = mt.doubleRotate(mn, rd)
	default:
		updateFP = true
	}
	return mn, updateFP
}

func (mt *monoidTree) findGTENode(mn *monoidTreeNode, x Ordered) *monoidTreeNode {
	switch {
	case mn == nil:
		return nil
	case x.Compare(mn.key) == 0:
		// Exact match
		return mn
	case x.Compare(mn.max) > 0:
		// All of this subtree is below v, maybe we can have
		// some luck with the parent node
		return mt.findGTENode(mn.parent, x)
	case x.Compare(mn.key) >= 0:
		// We're still below x (or at x, but allowEqual is
		// false), but given that we checked Max and saw that
		// this subtree has some keys that are greater than
		// or equal to x, we can find them on the right
		if mn.right == nil {
			// mn.Max lied to us
			panic("BUG: MonoidTreeNode: x > mn.Max but no right branch")
		}
		// Avoid endless recursion in case of a bug
		if x.Compare(mn.right.max) > 0 {
			panic("BUG: MonoidTreeNode: inconsistent Max on the right branch")
		}
		return mt.findGTENode(mn.right, x)
	case mn.left == nil || x.Compare(mn.left.max) > 0:
		// The current node's key is greater than x and the
		// left branch is either empty or fully below x, so
		// the current node is what we were looking for
		return mn
	default:
		// Some keys on the left branch are greater or equal
		// than x accordingto mn.Left.Max
		r := mt.findGTENode(mn.left, x)
		if r == nil {
			panic("BUG: MonoidTreeNode: inconsistent Max on the left branch")
		}
		return r
	}
}

func (mt *monoidTree) invRangeFingerprint(mn *monoidTreeNode, x, y Ordered, stop FingerprintPredicate) (any, *monoidTreeNode) {
	// QQQQQ: rename: rollover range
	next := mn
	minNode := mn.minNode()

	var acc any
	var stopped bool
	rightStartNode := mt.findGTENode(mn, x)
	if rightStartNode != nil {
		acc, next, stopped = mt.aggregateUntil(rightStartNode, acc, x, UpperBound{}, stop)
		if stopped {
			return acc, next
		}
	} else {
		acc = mt.m.Identity()
	}

	if y.Compare(minNode.key) > 0 {
		acc, next, _ = mt.aggregateUntil(minNode, acc, LowerBound{}, y, stop)
	}

	return acc, next
}

func (mt *monoidTree) rangeFingerprint(node MonoidTreeNode, start, end Ordered, stop FingerprintPredicate) (fp any, startNode, endNode *monoidTreeNode) {
	if mt.root == nil {
		return mt.m.Identity(), nil, nil
	}
	if node == nil {
		node = mt.root
	}

	mn := node.(*monoidTreeNode)
	minNode := mt.root.minNode()
	acc := mt.m.Identity()
	startNode = mt.findGTENode(mn, start)
	switch {
	case start.Compare(end) >= 0:
		// rollover range, which includes the case start == end
		// this includes 2 subranges:
		// [start, max_element] and [min_element, end)
		var stopped bool
		if node != nil {
			acc, endNode, stopped = mt.aggregateUntil(startNode, acc, start, UpperBound{}, stop)
		}

		if !stopped && end.Compare(minNode.key) > 0 {
			acc, endNode, _ = mt.aggregateUntil(minNode, acc, LowerBound{}, end, stop)
		}
	case node != nil:
		// normal range, that is, start < end
		acc, endNode, _ = mt.aggregateUntil(startNode, mt.m.Identity(), start, end, stop)
	}

	if startNode == nil {
		startNode = minNode
	}
	if endNode == nil {
		endNode = minNode
	}

	return acc, startNode, endNode
}

func (mt *monoidTree) RangeFingerprint(node MonoidTreeNode, start, end Ordered, stop FingerprintPredicate) (fp any, startNode, endNode MonoidTreeNode) {
	fp, stn, endn := mt.rangeFingerprint(node, start, end, stop)
	switch {
	case stn == nil && endn == nil:
		// avoid wrapping nil in MonoidTreeNode interface
		return fp, nil, nil
	case stn == nil || endn == nil:
		panic("BUG: can't have nil node just on one end")
	default:
		return fp, stn, endn
	}
}

func (mt *monoidTree) aggregateUntil(mn *monoidTreeNode, acc any, start, end Ordered, stop FingerprintPredicate) (fp any, node *monoidTreeNode, stopped bool) {
	acc, node, stopped = mt.aggregateUp(mn, acc, start, end, stop)
	if node == nil || end.Compare(node.key) <= 0 || stopped {
		return acc, node, stopped
	}

	f := mt.m.Op(acc, mt.m.Fingerprint(node.key))
	if stop.Match(f) {
		return acc, node, true
	}
	return mt.aggregateDown(node.right, f, end, stop)
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
func (mt *monoidTree) aggregateUp(mn *monoidTreeNode, acc any, start, end Ordered, stop FingerprintPredicate) (fp any, node *monoidTreeNode, stopped bool) {
	switch {
	case mn == nil:
		// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateUp: null node\n")
		return acc, nil, false
	case stop.Match(acc):
		// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateUp: stop: node %v acc %v\n", mn.key, acc)
		return acc, mn.prev(), true
	case end.Compare(mn.max) <= 0:
		// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateUp: LCA: node %v acc %v\n", mn.key, acc)
		// This node is a the LCA, the starting point for AggregateDown
		return acc, mn, false
	case start.Compare(mn.key) <= 0:
		// This node is within the target range
		// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateUp: in-range node %v acc %v\n", mn.key, acc)
		f := mt.m.Op(acc, mt.m.Fingerprint(mn.key))
		if stop.Match(f) {
			// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateUp: stop at the own node %v acc %v\n", mn.key, acc)
			return acc, mn, true
		}
		f1 := mt.m.Op(f, mt.safeFingerprint(mn.right))
		if stop.Match(f1) {
			// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateUp: right subtree matches node %v acc %v f1 %v\n", mn.key, acc, f1)
			// The target node is somewhere in the right subtree
			if mn.right == nil {
				panic("BUG: nil right child with non-identity fingerprint")
			}
			acc, node := mt.boundedAggregate(mn.right, f, stop)
			if node == nil {
				panic("BUG: aggregateUp: bad subtree fingerprint on the right branch")
			}
			// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateUp: right subtree: node %v acc %v\n", node.key, acc)
			return acc, node, true
		} else {
			// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateUp: no right subtree match: node %v acc %v f1 %v\n", mn.key, acc, f1)
			acc = f1
		}
	}
	if mn.parent == nil {
		// No need for AggregateDown as we've covered the entire
		// [start, end) range
		return acc, nil, false
	}
	return mt.aggregateUp(mn.parent, acc, start, end, stop)
}

// aggregateDown descends from the LCA (lowest common ancestor) of nodes within
// the range ending at the 'end'. On the way down, the unvisited left subtrees
// are guaranteed to lie within the range, so they're included into the
// aggregation using their saved fingerprint.
// If stop function is passed, we find the node on which it returns true
// for the fingerprint accumulated between start and that node
func (mt *monoidTree) aggregateDown(mn *monoidTreeNode, acc any, end Ordered, stop FingerprintPredicate) (fp any, node *monoidTreeNode, stopped bool) {
	switch {
	case mn == nil:
		// fmt.Fprintf(os.Stderr, "QQQQQ: mn == nil\n")
		return acc, nil, false
	case stop.Match(acc):
		// fmt.Fprintf(os.Stderr, "QQQQQ: stop on node\n")
		return acc, mn.prev(), true
	case end.Compare(mn.key) > 0:
		// fmt.Fprintf(os.Stderr, "QQQQQ: within the range\n")
		// We're within the range but there also may be nodes
		// within the range to the right. The left branch is
		// fully within the range
		f := mt.m.Op(acc, mt.safeFingerprint(mn.left))
		if stop.Match(f) {
			// fmt.Fprintf(os.Stderr, "QQQQQ: left subtree covers it\n")
			// The target node is somewhere in the left subtree
			if mn.left == nil {
				panic("BUG: aggregateDown: nil left child with non-identity fingerprint")
			}
			acc, node := mt.boundedAggregate(mn.left, acc, stop)
			// fmt.Fprintf(os.Stderr, "QQQQQ: boundedAggregate: returned acc %v node %#v\n", acc, node)
			if node == nil {
				panic("BUG: aggregateDown: bad subtree fingerprint on the left branch")
			}
			return acc, node, true
		}
		f1 := mt.m.Op(f, mt.m.Fingerprint(mn.key))
		if stop.Match(f1) {
			// fmt.Fprintf(os.Stderr, "QQQQQ: stop at the node, prev %#v\n", node.prev())
			return f, mn, true
		} else {
			acc = f1
		}
		// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateDown on the right\n")
		return mt.aggregateDown(mn.right, acc, end, stop)
	case mn.left == nil || end.Compare(mn.left.max) > 0:
		// fmt.Fprintf(os.Stderr, "QQQQQ: node covers the range\n")
		// Found the rightmost bounding node
		f := mt.m.Op(acc, mt.safeFingerprint(mn.left))
		if stop.Match(f) {
			// The target node is somewhere in the left subtree
			if mn.left == nil {
				panic("BUG: aggregateDown: nil left child with non-identity fingerprint")
			}
			acc, node := mt.boundedAggregate(mn.left, acc, stop)
			// fmt.Fprintf(os.Stderr, "QQQQQ: boundedAggregate(2): returned acc %v node %#v\n", acc, node)
			if node == nil {
				panic("BUG: aggregateDown: bad subtree fingerprint on the left branch")
			}
			return acc, node, true
		}
		return f, mn, false
	default:
		// fmt.Fprintf(os.Stderr, "QQQQQ: aggregateDown: going further down\n")
		// We're too far to the right, outside the range
		return mt.aggregateDown(mn.left, acc, end, stop)
	}
}

func (mt *monoidTree) boundedAggregate(mn *monoidTreeNode, acc any, stop FingerprintPredicate) (any, *monoidTreeNode) {
	// fmt.Fprintf(os.Stderr, "QQQQQ: boundedAggregate: node %v, acc %v\n", mn.key, acc)
	if mn == nil {
		return acc, nil
	}

	// If we don't need to stop, or if the stop point is somewhere after
	// this subtree, we can just use the pre-calculated subtree fingerprint
	if f := mt.m.Op(acc, mn.fingerprint); !stop.Match(f) {
		return f, nil
	}

	// This function is not supposed to be called with acc already matching
	// the stop condition
	if stop(acc) {
		panic("BUG: boundedAggregate: initial fingerprint is matched before the first node")
	}

	if mn.left != nil {
		// See if we can skip recursion on the left branch
		f := mt.m.Op(acc, mn.left.fingerprint)
		if !stop(f) {
			// fmt.Fprintf(os.Stderr, "QQQQQ: boundedAggregate: mn Left non-nil and no-stop %v, f %v, left fingerprint %v\n", mn.key, f, mn.Left.Fingerprint)
			acc = f
		} else {
			// The target node must be contained in the left subtree
			var node *monoidTreeNode
			acc, node = mt.boundedAggregate(mn.left, acc, stop)
			if node == nil {
				panic("BUG: boundedAggregate: bad subtree fingerprint on the left branch")
			}
			// fmt.Fprintf(os.Stderr, "QQQQQ: boundedAggregate: mn Left non-nil and stop %v, new node %v, acc %v\n", mn.key, node.key, acc)
			return acc, node
		}
	}

	f := mt.m.Op(acc, mt.m.Fingerprint(mn.key))

	switch {
	case stop(f):
		// fmt.Fprintf(os.Stderr, "QQQQQ: boundedAggregate: stop at this node %v, fp %v\n", mn.key, acc)
		return acc, mn
	case mn.right != nil:
		f1 := mt.m.Op(f, mn.right.fingerprint)
		if !stop(f1) {
			// The right branch is still below the target fingerprint
			// fmt.Fprintf(os.Stderr, "QQQQQ: boundedAggregate: mn Right non-nil and no-stop %v, acc %v\n", mn.key, acc)
			acc = f1
		} else {
			// The target node must be contained in the right subtree
			var node *monoidTreeNode
			acc, node := mt.boundedAggregate(mn.right, f, stop)
			if node == nil {
				panic("BUG: boundedAggregate: bad subtree fingerprint on the right branch")
			}
			// fmt.Fprintf(os.Stderr, "QQQQQ: boundedAggregate: mn Right non-nil and stop %v, new node %v, acc %v\n", mn.key, node.key, acc)
			return acc, node
		}
	}
	// fmt.Fprintf(os.Stderr, "QQQQQ: boundedAggregate: %v -- return acc %v\n", mn.key, acc)
	// QQQQQ: ZXXXXXX: return acc, nil !!!!
	return f, nil
}

func (mt *monoidTree) Next(node MonoidTreeNode) MonoidTreeNode {
	next := node.(*monoidTreeNode).next()
	if next == nil {
		return nil
	}
	return next
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
// TBD: maybe: persistent rbtree -- note that MonoidTree will be immutable,
// too, in this case (Insert returns a new tree => no problem with thread safety
// or cached min/max)

// Persistent tree:
// In 'derived' mode, along with the color, use 2 more bits:
//   * whether this node is new
//   * whether this node has new descendants
// Note that any combination possible (a new node MAY NOT have any new descendants)
// Derived trees are created with Derive(). The persistent mode may only be used
// for derived trees (need to see if this is worth it).
// With these bits, it is fairly easy to grab the newly added items w/o
// using the local sync algorithm
