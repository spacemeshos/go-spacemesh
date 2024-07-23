package skiplist

import (
	"bytes"
	"hash/maphash"
	"math"
	"slices"
)

const (
	maxSLHeight = 24
	pValue      = 1 / math.E // ref: https://www.sciencedirect.com/science/article/pii/030439759400296U
)

func randUint32() uint32 {
	// this effectively calls runtime.rand which is much faster than math/rand
	return uint32(new(maphash.Hash).Sum64())
}

// inspired by https://www.cloudcentric.dev/implementing-a-skip-list-in-go/
var probabilities = calcProbabilities()

func calcProbabilities() [maxSLHeight]uint32 {
	var probs [maxSLHeight]uint32
	probability := 1.0
	for level := 0; level < maxSLHeight; level++ {
		probs[level] = uint32(probability * float64(math.MaxUint32))
		probability *= pValue
	}
	return probs
}

func randomHeight() int {
	v := randUint32()
	height := 1
	for height < maxSLHeight && v <= probabilities[height] {
		height++
	}
	return height
}

type Node struct {
	key       []byte
	nextNodes []*Node
}

func (n *Node) height() int {
	return len(n.nextNodes)
}

// Key returns the key of the node.
func (n *Node) Key() []byte {
	return n.key
}

// Next returns the node following this one, or nil if there's
// no next node.
func (n *Node) Next() *Node {
	return n.nextNodes[0]
}

// SkipList represents an insert-only skip list.
type SkipList struct {
	keySize int
	head    *Node
	// TBD: rm: no much sense in this pool as nodes aren't
	// released. Global non-sync.Pool pool might make sense
	// nodePools [maxSLHeight]sync.Pool
}

func New(keySize int) *SkipList {
	sl := &SkipList{
		keySize: keySize,
		head:    &Node{},
	}
	// for n := range sl.nodePools {
	// 	sl.nodePools[n].New = func() interface{} {
	// 		return &Node{
	// 			key:       make([]byte, keySize),
	// 			nextNodes: make([]*Node, n+1),
	// 		}
	// 	}
	// }
	return sl
}

func (sl *SkipList) FindGTENode(key []byte) *Node {
	var next, candidate *Node
	node := sl.head
OUTER:
	for l := sl.head.height() - 1; l >= 0; l-- {
		next = node.nextNodes[l]
		for next != nil {
			switch bytes.Compare(next.key, key) {
			case -1:
				// The next node is still below target key, advance to it
				node = next
				next = node.nextNodes[l]
			case 0:
				// Found an exact match
				return next
			default:
				// The next node is beyond the target key, try to find a
				// smaller key that's >= target key on a lower level.
				// Failing that, stick with what we found so far.
				candidate = next
				continue OUTER
			}
		}
	}

	return candidate
}

func (sl *SkipList) newNode(height int, key []byte) *Node {
	// newNode := sl.nodePools[height-1].Get().(*Node)
	// copy(newNode.key, key)
	newNode := &Node{
		key:       slices.Clone(key),
		nextNodes: make([]*Node, height),
	}
	return newNode
}

// First returns the first node in the skip list.
func (sl *SkipList) First() *Node {
	if sl.head.height() == 0 {
		return nil
	}
	return sl.head.Next()
}

// Add adds key to the skiplist if it's not yet present there.
func (sl *SkipList) Add(key []byte) {
	var (
		prevs [maxSLHeight]*Node
		next  *Node
	)

	height := randomHeight()
	newNode := sl.newNode(height, key)
	prev := sl.head
	oldHeight := sl.head.height()
	for l := oldHeight - 1; l >= 0; l-- {
		next = prev.nextNodes[l]
	INNER:
		for next != nil {
			switch bytes.Compare(next.key, key) {
			case -1:
				// The next node is still below target key, advance to it
				prev = next
				next = next.nextNodes[l]
			case 0:
				// Exact match, skip adding duplicate entry
				return
			case 1:
				// The next node is beyond the target key, record it
				// as the previous node at this level and proceed
				// to the lower level.
				break INNER
			}
		}
		prevs[l] = prev
	}

	sl.grow(height)
	for l := range min(height, oldHeight) {
		newNode.nextNodes[l] = prevs[l].nextNodes[l]
		prevs[l].nextNodes[l] = newNode
	}
	for l := oldHeight; l < height; l++ {
		newNode.nextNodes[l] = nil
		sl.head.nextNodes[l] = newNode
	}
}

func (sl *SkipList) grow(newHeight int) {
	if newHeight <= sl.head.height() {
		return
	}
	if newHeight > maxSLHeight {
		panic("BUG: skiplist height too high")
	}
	sl.head.nextNodes = append(sl.head.nextNodes, make([]*Node, newHeight-sl.head.height())...)
}
