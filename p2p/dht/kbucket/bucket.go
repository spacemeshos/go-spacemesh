package kbucket

import (
	"container/list"
	"github.com/UnrulyOS/go-unruly/p2p"
	"sync"
)

// Bucket holds a list of peers.
type Bucket struct {
	lk   sync.RWMutex
	list *list.List
}

func newBucket() *Bucket {
	b := new(Bucket)
	b.list = list.New()
	return b
}

func (b *Bucket) Peers() []p2p.RemoteNodeData {
	b.lk.RLock()
	defer b.lk.RUnlock()
	ps := make([]p2p.RemoteNodeData, 0, b.list.Len())
	for e := b.list.Front(); e != nil; e = e.Next() {
		node := e.Value.(p2p.RemoteNodeData)
		ps = append(ps, node)
	}
	return ps
}

func (b *Bucket) Has(node p2p.RemoteNodeData) bool {
	b.lk.RLock()
	defer b.lk.RUnlock()
	for e := b.list.Front(); e != nil; e = e.Next() {
		if e.Value.(p2p.RemoteNodeData).Id() == node.Id() {
			return true
		}
	}
	return false
}

func (b *Bucket) Remove(node p2p.RemoteNodeData) {
	b.lk.Lock()
	defer b.lk.Unlock()
	for e := b.list.Front(); e != nil; e = e.Next() {
		if e.Value.(p2p.RemoteNodeData).Id() == node.Id() {
			b.list.Remove(e)
		}
	}
}

func (b *Bucket) MoveToFront(node p2p.RemoteNodeData) {
	b.lk.Lock()
	defer b.lk.Unlock()
	for e := b.list.Front(); e != nil; e = e.Next() {
		if e.Value.(p2p.RemoteNodeData).Id() == node.Id() {
			b.list.MoveToFront(e)
		}
	}
}

func (b *Bucket) PushFront(n p2p.RemoteNodeData) {
	b.lk.Lock()
	b.list.PushFront(n)
	b.lk.Unlock()
}

func (b *Bucket) PopBack() p2p.RemoteNodeData {
	b.lk.Lock()
	defer b.lk.Unlock()
	last := b.list.Back()
	b.list.Remove(last)
	return last.Value.(p2p.RemoteNodeData)
}

func (b *Bucket) Len() int {
	b.lk.RLock()
	defer b.lk.RUnlock()
	return b.list.Len()
}

// Split splits a buckets peers into two buckets, the methods receiver will have
// peers with CPL equal to cpl, the returned bucket will have peers with CPL
// greater than cpl (returned bucket has closer peers)
/*
func (b *Bucket) Split(cpl int, target p2p.RemoteNodeData) *Bucket {
	b.lk.Lock()
	defer b.lk.Unlock()

	out := list.New()
	newbuck := newBucket()
	newbuck.list = out
	e := b.list.Front()
	for e != nil {
		node := (e.Value.(p2p.RemoteNodeData))
		peerCPL := node.CommonPrefixLength(target)
		if peerCPL > cpl {
			cur := e
			out.PushBack(e.Value)
			e = e.Next()
			b.list.Remove(cur)
			continue
		}
		e = e.Next()
	}
	return newbuck
}
*/