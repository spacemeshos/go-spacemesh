package kbucket

import (
	"container/list"
	"github.com/UnrulyOS/go-unruly/p2p"
	"github.com/UnrulyOS/go-unruly/p2p/dht"
	"sync"
)

// A dht kbucket
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
	lk   sync.RWMutex
	list *list.List
}

func newBucket() Bucket {
	return &bucketimpl{
		list: list.New(),
	}
}

func (b *bucketimpl) Peers() []p2p.RemoteNodeData {
	b.lk.RLock()
	defer b.lk.RUnlock()
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
	b.lk.RLock()
	defer b.lk.RUnlock()
	for e := b.list.Front(); e != nil; e = e.Next() {
		if e.Value.(p2p.RemoteNodeData).Id() == node.Id() {
			return true
		}
	}
	return false
}

func (b *bucketimpl) Remove(node p2p.RemoteNodeData) {
	b.lk.Lock()
	defer b.lk.Unlock()
	for e := b.list.Front(); e != nil; e = e.Next() {
		if e.Value.(p2p.RemoteNodeData).Id() == node.Id() {
			b.list.Remove(e)
		}
	}
}

func (b *bucketimpl) MoveToFront(node p2p.RemoteNodeData) {
	b.lk.Lock()
	defer b.lk.Unlock()
	for e := b.list.Front(); e != nil; e = e.Next() {
		if e.Value.(p2p.RemoteNodeData).Id() == node.Id() {
			b.list.MoveToFront(e)
		}
	}
}

func (b *bucketimpl) PushFront(n p2p.RemoteNodeData) {
	b.lk.Lock()
	b.list.PushFront(n)
	b.lk.Unlock()
}

func (b *bucketimpl) PushBack(n p2p.RemoteNodeData) {
	b.lk.Lock()
	b.list.PushBack(n)
	b.lk.Unlock()
}

func (b *bucketimpl) PopBack() p2p.RemoteNodeData {
	b.lk.Lock()
	defer b.lk.Unlock()
	last := b.list.Back()
	b.list.Remove(last)
	return last.Value.(p2p.RemoteNodeData)
}

func (b *bucketimpl) Len() int {
	b.lk.RLock()
	defer b.lk.RUnlock()
	return b.list.Len()
}

// Split splits a buckets peers into two buckets, the methods receiver will have
// peers with CPL equal to cpl, the returned bucket will have peers with CPL
// greater than cpl (returned bucket has closer peers)

func (b *bucketimpl) Split(cpl int, target dht.ID) Bucket {
	b.lk.Lock()
	defer b.lk.Unlock()

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
