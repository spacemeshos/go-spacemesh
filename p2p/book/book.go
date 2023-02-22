package book

import (
	"container/heap"
	"math/rand"
	"time"
)

type class uint32

const (
	deleted class = iota
	unknown
	stable
	good
)

func getClass(addr *address) class {
	switch addr.Class {
	case unknown:
		// unknown entries should be deleted after being unavailable for some time
		// or as a replacement for new unknown entries

		// upgraded to stable after couple of succesful tries
	case stable:
		// stable entries can be shared with other, but are more likely t
		// degrade into unknown
	case good:
		// good entries are "good" and should not degrade into unkown after
		// couple offailures
	}
	return unknown
}

type bucket int

const (
	public = iota
	private
	local
)

func getBucket(raw string) bucket {
	return public
}

type address struct {
	Raw       string
	Class     class
	Connected bool

	protected bool

	lastTry  time.Time
	failures struct {
		first, last time.Time
	}
	success struct {
		first, last time.Time
	}
}

func New() *Book {
	return &Book{
		known:     map[string]*address{},
		queue:     make([]*address, 0, 1000),
		rng:       rand.New(rand.NewSource(time.Now().Unix())),
		shareable: []*address{},
	}
}

type Book struct {
	limit int
	known map[string]*address

	// priority queue with all addresses except connected
	queue queue

	// randomized data structures with good and stable connections
	rng *rand.Rand
	// when adding and selecting connections verify that they came from same bucket
	// namely:
	// - routable addresses can be delievered from any peer
	// - private addresses can be delivered only by private peers
	// - local address can be delivered only by local peers
	shareable []*address
}

// Add
func (b *Book) Add(id, raw string) {
	addr := b.known[id]
	if addr == nil {
		if len(b.known) == b.limit {
			return
		}
		addr = &address{
			Raw:     raw,
			Class:   unknown,
			lastTry: time.Now(),
		}
		heap.Push(&b.queue, addr)
		b.known[id] = addr
	}
	addr.Raw = raw
}

type Event uint

const (
	Connected = 1 << iota
	Disconnected
	Success
	Fail
	Protect
)

func (b *Book) Event(id string, event Event) {
	addr := b.known[id]
	if addr == nil {
		return
	}
	now := time.Now()
	if event&Protect > 0 && !addr.protected {
		addr.protected = true
		addr.Class = good
		b.shareable = append(b.shareable, addr)
		heap.Push(&b.queue, addr)
		return
	}
	if event&Connected > 0 {
		addr.Connected = true
	} else if event&Disconnected > 0 {
		addr.Connected = false
	}
	if event&Success == 0 && event&Fail == 0 {
		return
	}
	heap.Push(&b.queue, addr)
	if addr.protected {
		return
	}
	addr.lastTry = now
	if event&Success > 0 {
		addr.success.last = now
	} else if event&Fail > 0 {
		addr.failures.last = now
	}
	c := getClass(addr)
	if addr.Class == unknown && c > unknown {
		b.shareable = append(b.shareable, addr)
	} else if addr.Class == unknown && c < unknown {
		delete(b.known, id)
	}
	addr.Class = c
}

func (b *Book) iterShareable() iterator {
	b.rng.Shuffle(len(b.shareable), func(i, j int) {
		b.shareable[i], b.shareable[j] = b.shareable[j], b.shareable[i]
	})
	i := 0
	return func() string {
		for {
			if i == len(b.shareable) {
				return ""
			}
			rst := b.shareable[i]
			if rst.Class <= unknown {
				copy(b.shareable[i:], b.shareable[i+1:])
			} else {
				i++
				return rst.Raw
			}
		}
	}
}

func (b *Book) drainQueue() iterator {
	return func() string {
		for {
			rst := heap.Pop(&b.queue)
			if rst == nil {
				return ""
			}
			if rst.(*address).Class == deleted {
				continue
			}
			return rst.(*address).Raw
		}
	}
}

type iterator func() string

type queue []*address

func (q queue) Len() int           { return len(q) }
func (q queue) Less(i, j int) bool { return q[i].lastTry.Before(q[j].lastTry) }
func (q queue) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }
func (q *queue) Push(x any)        { *q = append(*q, x.(*address)) }
func (q *queue) Pop() any {
	old := *q
	n := len(old)
	x := old[n-1]
	old[n-1] = nil
	*q = old[0 : n-1]
	return x
}
