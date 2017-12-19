package table

import (
	"container/list"
	"github.com/UnrulyOS/go-unruly/p2p"
	"github.com/UnrulyOS/go-unruly/p2p/dht"
)

// Buket is a dht kbucket type
// This type is NOT thread safe. It is desinged as an internal data structure
// for RoutingTable. RoutingTable is responsible for thread safety

type Bucket interface {
	Peers() []p2p.RemoteNodeData
	Has(node p2p.RemoteNodeData) bool
	Remove(node p2p.RemoteNodeData)
	MoveToFront(node p2p.RemoteNodeData)
	PushFront(n p2p.RemoteNodeData)
	PushBack(n p2p.RemoteNodeData)

	PopBack() p2p.RemoteNodeData
	Len() int
	Split(cpl int, target dht.ID) Bucket
	List() *list.List
}

type bucketimpl struct {
	list *list.List
}

func newBucket() Bucket {
	return &bucketimpl{
		list: list.New(),
	}
}

func (b *bucketimpl) Peers() []p2p.RemoteNodeData {
	ps := make([]p2p.RemoteNodeData, 0, b.list.Len())
	for e := b.list.Front(); e != nil; e = e.Next() {
		node := e.Value.(p2p.RemoteNodeData)
		ps = append(ps, node)
	}
	return ps
}

func (b *bucketimpl) List() *list.List {
	return b.list
}

func (b *bucketimpl) Has(node p2p.RemoteNodeData) bool {
	for e := b.list.Front(); e != nil; e = e.Next() {
		if e.Value.(p2p.RemoteNodeData).Id() == node.Id() {
			return true
		}
	}
	return false
}

func (b *bucketimpl) Remove(node p2p.RemoteNodeData) {
	for e := b.list.Front(); e != nil; e = e.Next() {
		if e.Value.(p2p.RemoteNodeData).Id() == node.Id() {
			b.list.Remove(e)
		}
	}
}

func (b *bucketimpl) MoveToFront(node p2p.RemoteNodeData) {
	for e := b.list.Front(); e != nil; e = e.Next() {
		if e.Value.(p2p.RemoteNodeData).Id() == node.Id() {
			b.list.MoveToFront(e)
		}
	}
}

func (b *bucketimpl) PushFront(n p2p.RemoteNodeData) {
	b.list.PushFront(n)
}

func (b *bucketimpl) PushBack(n p2p.RemoteNodeData) {
	b.list.PushBack(n)
}

func (b *bucketimpl) PopBack() p2p.RemoteNodeData {
	last := b.list.Back()
	b.list.Remove(last)
	return last.Value.(p2p.RemoteNodeData)
}

func (b *bucketimpl) Len() int {
	return b.list.Len()
}

// Split splits a buckets peers into two buckets, the methods receiver will have
// peers with CPL equal to cpl, the returned bucket will have peers with CPL
// greater than cpl (returned bucket has closer peers)

func (b *bucketimpl) Split(cpl int, target dht.ID) Bucket {
	newbuck := newBucket()
	e := b.list.Front()
	for e != nil {
		node := (e.Value.(p2p.RemoteNodeData))
		peerCPL := node.DhtId().CommonPrefixLen(target)
		if peerCPL > cpl {
			cur := e
			newbuck.PushBack(node)
			e = e.Next()
			b.Remove(cur.Value.(p2p.RemoteNodeData))
			continue
		}
		e = e.Next()
	}
	return newbuck
}
