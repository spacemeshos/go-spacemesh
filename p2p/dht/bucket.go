package dht

import (
	"container/list"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
)

// Bucket is a dht k-bucket type. Bucket methods are NOT thread safe.
// RoutingTable (or other clients) is responsible for serializing access to Bucket's methods.
type Bucket interface {
	Peers() []node.Node
	Has(n node.Node) bool
	Remove(n node.Node) bool
	MoveToFront(n node.Node)
	PushFront(n node.Node)
	PushBack(n node.Node)
	PopBack() node.Node
	Len() int
	Split(cpl int, target node.DhtID) Bucket
	List() *list.List
}

// Internal bucket implementation type
type bucketimpl struct {
	list *list.List
}

// NewBucket creates a new empty bucket.
func NewBucket() Bucket {
	return &bucketimpl{
		list: list.New(),
	}
}

// Peers returns a slice of RemoteNodeData for the peers stored in the bucket.
func (b *bucketimpl) Peers() []node.Node {
	ps := make([]node.Node, 0, b.list.Len())
	for e := b.list.Front(); e != nil; e = e.Next() {
		n := e.Value.(node.Node)
		ps = append(ps, n)
	}
	return ps
}

// List returns a list of RemoteNodeData stored in this bucket.
func (b *bucketimpl) List() *list.List {
	return b.list
}

// Has returns true iff the bucket stores n.
func (b *bucketimpl) Has(n node.Node) bool {
	for e := b.list.Front(); e != nil; e = e.Next() {
		n1 := e.Value.(node.Node)
		if n1.DhtID().Equals(n.DhtID()) {
			return true
		}
	}
	return false
}

// Remove removes n from the bucket if it is stored in it.
// It returns true if n was in the bucket and was removed and false otherwise.
func (b *bucketimpl) Remove(n node.Node) bool {
	for e := b.list.Front(); e != nil; e = e.Next() {
		if e.Value.(node.Node).DhtID().Equals(n.DhtID()) {
			b.list.Remove(e)
			return true
		}
	}
	return false
}

// MoveToFront moves n to the front of the bucket.
func (b *bucketimpl) MoveToFront(n node.Node) {
	for e := b.list.Front(); e != nil; e = e.Next() {
		if e.Value.(node.Node).DhtID().Equals(n.DhtID()) {
			b.list.MoveToFront(e)
		}
	}
}

// PushFront adds a new identity to the front of the bucket.
func (b *bucketimpl) PushFront(n node.Node) {
	b.list.PushFront(n)
}

// PushBack adds a new identity to the back of the bucket.
func (b *bucketimpl) PushBack(n node.Node) {
	b.list.PushBack(n)
}

// PopBack removes the identity at the back of the bucket from the bucket and returns it.
func (b *bucketimpl) PopBack() node.Node {
	last := b.list.Back()
	if last == nil {
		return node.EmptyNode
	}
	b.list.Remove(last)
	return last.Value.(node.Node)
}

// Len returns the number of nodes stored in the bucket.
func (b *bucketimpl) Len() int {
	return b.list.Len()
}

// Split splits bucket stored nodes into two buckets.
// The receiver bucket will have peers with cpl equal to cpl with target.
// The returned bucket will have peers with cpl greater than cpl with target (closer peers).
func (b *bucketimpl) Split(cpl int, target node.DhtID) Bucket {
	newbucket := NewBucket()
	e := b.list.Front()
	for e != nil {
		n := e.Value.(node.Node)
		peerCPL := n.DhtID().CommonPrefixLen(target)
		if peerCPL > cpl {
			newbucket.PushBack(n)
			curr := e
			e = e.Next()
			b.list.Remove(curr)
		} else {
			e = e.Next()
		}
	}
	return newbucket
}
