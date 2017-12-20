package table

import (
	"container/list"
	"github.com/UnrulyOS/go-unruly/p2p/dht"
	node "github.com/UnrulyOS/go-unruly/p2p/node"
)


// Bucket is a dht kbucket type
// Bucket NOT thread safe.
// RoutingTable (or other clients) are responsible for serializing access to a Bucket
type Bucket interface {
	Peers() []node.RemoteNodeData
	Has(n node.RemoteNodeData) bool
	Remove(n node.RemoteNodeData)
	MoveToFront(n node.RemoteNodeData)
	PushFront(n node.RemoteNodeData)
	PushBack(n node.RemoteNodeData)
	PopBack() node.RemoteNodeData
	Len() int
	Split(cpl int, target dht.ID) Bucket
	List() *list.List
}

type bucketimpl struct {
	list *list.List
}

func NewBucket() Bucket {
	return &bucketimpl{
		list: list.New(),
	}
}

func (b *bucketimpl) Peers() []node.RemoteNodeData {
	ps := make([]node.RemoteNodeData, 0, b.list.Len())
	for e := b.list.Front(); e != nil; e = e.Next() {
		n := e.Value.(node.RemoteNodeData)
		ps = append(ps, n)
	}
	return ps
}

func (b *bucketimpl) List() *list.List {
	return b.list
}

func (b *bucketimpl) Has(n node.RemoteNodeData) bool {
	for e := b.list.Front(); e != nil; e = e.Next() {
		n1 := e.Value.(node.RemoteNodeData)
		if n1.Id() == n.Id() {
			return true
		}
	}
	return false
}

func (b *bucketimpl) Remove(n node.RemoteNodeData) {
	for e := b.list.Front(); e != nil; e = e.Next() {
		if e.Value.(node.RemoteNodeData).Id() == n.Id() {
			b.list.Remove(e)
		}
	}
}

func (b *bucketimpl) MoveToFront(n node.RemoteNodeData) {
	for e := b.list.Front(); e != nil; e = e.Next() {
		if e.Value.(node.RemoteNodeData).Id() == n.Id() {
			b.list.MoveToFront(e)
		}
	}
}

func (b *bucketimpl) PushFront(n node.RemoteNodeData) {
	b.list.PushFront(n)
}

func (b *bucketimpl) PushBack(n node.RemoteNodeData) {
	b.list.PushBack(n)
}

func (b *bucketimpl) PopBack() node.RemoteNodeData {
	last := b.list.Back()
	if last == nil {
		return nil
	}
	b.list.Remove(last)
	return last.Value.(node.RemoteNodeData)
}

func (b *bucketimpl) Len() int {
	return b.list.Len()
}

// Split bucket's nodes into two buckets.
// The receiver bucket will have peers with CPL equal to cpl.
// The returned bucket will have peers with CPL greater than cpl (returned bucket has closer peers)

func (b *bucketimpl) Split(cpl int, target dht.ID) Bucket {

	newbucket := NewBucket()

	e := b.list.Front()

	for e != nil {
		node := e.Value.(node.RemoteNodeData)
		peerCPL := node.DhtId().CommonPrefixLen(target)
		if peerCPL > cpl {
			newbucket.PushBack(node)
			curr := e
			e = e.Next()
			b.list.Remove(curr)
		} else {
			e = e.Next()
		}
	}
	return newbucket
}
