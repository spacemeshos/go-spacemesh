package table

import (
	"container/list"
	"github.com/spacemeshos/go-spacemesh/p2p/dht"
	node "github.com/spacemeshos/go-spacemesh/p2p/node"
)

// Bucket is a dht k-bucket type. Bucket methods are NOT thread safe.
// RoutingTable (or other clients) is responsible for serializing access to Bucket's methods.
type Bucket interface {
	Peers() []node.RemoteNodeData
	Has(n node.RemoteNodeData) bool
	Remove(n node.RemoteNodeData) bool
	MoveToFront(n node.RemoteNodeData)
	PushFront(n node.RemoteNodeData)
	PushBack(n node.RemoteNodeData)
	PopBack() node.RemoteNodeData
	Len() int
	Split(cpl int, target dht.ID) Bucket
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
func (b *bucketimpl) Peers() []node.RemoteNodeData {
	ps := make([]node.RemoteNodeData, 0, b.list.Len())
	for e := b.list.Front(); e != nil; e = e.Next() {
		n := e.Value.(node.RemoteNodeData)
		ps = append(ps, n)
	}
	return ps
}

// List returns a list of RemoteNodeData stored in this bucket.
func (b *bucketimpl) List() *list.List {
	return b.list
}

// Has returns true iff the bucket stores n.
func (b *bucketimpl) Has(n node.RemoteNodeData) bool {
	for e := b.list.Front(); e != nil; e = e.Next() {
		n1 := e.Value.(node.RemoteNodeData)
		if n1.ID() == n.ID() {
			return true
		}
	}
	return false
}

// Remove removes n from the bucket if it is stored in it.
// It returns true if n was in the bucket and was removed and false otherwise.
func (b *bucketimpl) Remove(n node.RemoteNodeData) bool {
	for e := b.list.Front(); e != nil; e = e.Next() {
		if e.Value.(node.RemoteNodeData).ID() == n.ID() {
			b.list.Remove(e)
			return true
		}
	}
	return false
}

// MoveToFront moves n to the front of the bucket.
func (b *bucketimpl) MoveToFront(n node.RemoteNodeData) {
	for e := b.list.Front(); e != nil; e = e.Next() {
		if e.Value.(node.RemoteNodeData).ID() == n.ID() {
			b.list.MoveToFront(e)
		}
	}
}

// PushFront adds a new node to the front of the bucket.
func (b *bucketimpl) PushFront(n node.RemoteNodeData) {
	b.list.PushFront(n)
}

// PushBack adds a new node to the back of the bucket.
func (b *bucketimpl) PushBack(n node.RemoteNodeData) {
	b.list.PushBack(n)
}

// PopBack removes the node at the back of the bucket from the bucket and returns it.
func (b *bucketimpl) PopBack() node.RemoteNodeData {
	last := b.list.Back()
	if last == nil {
		return nil
	}
	b.list.Remove(last)
	return last.Value.(node.RemoteNodeData)
}

// Len returns the number of nodes stored in the bucket.
func (b *bucketimpl) Len() int {
	return b.list.Len()
}

// Split splits bucket stored nodes into two buckets.
// The receiver bucket will have peers with cpl equal to cpl with target.
// The returned bucket will have peers with cpl greater than cpl with target (closer peers).
func (b *bucketimpl) Split(cpl int, target dht.ID) Bucket {
	newbucket := NewBucket()
	e := b.list.Front()
	for e != nil {
		n := e.Value.(node.RemoteNodeData)
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
