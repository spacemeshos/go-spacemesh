package book

import (
	"container/list"
	"math/rand"
	"time"
)

const SELF ID = "SELF"

type ID string
type Address string

type class uint32

const (
	deleted class = iota
	unknown
	stable
	good
)

func classify(addr *addressInfo) class {
	switch addr.Class {
	case unknown:
		// unknown entries should be deleted after being unavailable for some time
		// or as a replacement for new unknown entries
		// upgraded to stable after couple of succesful tries
		if addr.success == 2 {
			return stable
		} else if addr.failures == 2 {
			return deleted
		}
	case stable:
		// stable entries can be shared with other, but are more likely t
		// degrade into unknown
		if addr.success == 10 {
			return stable
		} else if addr.failures == 4 {
			return unknown
		}
	case good:
		// good entries are "good" and should not degrade into unkown after
		// couple of failures
		if addr.failures == 10 {
			return unknown
		}
	}
	return addr.Class
}

type bucket int

const (
	public = iota
	private
	local
)

func bucketize(raw Address) bucket {
	return public
}

type addressInfo struct {
	// exported values will be persisted
	Raw       Address
	Class     class
	Connected bool

	id        ID
	bucket    bucket
	protected bool
	failures  int
	success   int
}

type Opt func(*Book)

// WithLimit updates default book limit.
func WithLimit(limit int) Opt {
	return func(b *Book) {
		b.limit = limit
	}
}

func New(opts ...Opt) *Book {
	b := &Book{
		limit:     50000,
		known:     map[ID]*addressInfo{},
		queue:     list.New(),
		rng:       rand.New(rand.NewSource(time.Now().Unix())),
		shareable: []*addressInfo{},
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

type Book struct {
	limit     int
	known     map[ID]*addressInfo
	queue     *list.List
	rng       *rand.Rand
	shareable []*addressInfo
}

func (b *Book) Add(src, id ID, raw Address) {
	addr := b.known[id]
	bucket := bucketize(raw)
	if src != SELF {
		source := b.known[src]
		if source == nil {
			return
		}
		if bucket != source.bucket {
			return
		}
	}
	if addr == nil {
		if len(b.known) == b.limit {
			return
		}
		addr = &addressInfo{
			Raw:    raw,
			Class:  unknown,
			id:     id,
			bucket: bucket,
		}
		b.queue.PushBack(addr)
		b.known[id] = addr
	}
	if addr.Raw != raw {
		addr.Raw = raw
		addr.Class = unknown
		addr.success = 0
		addr.failures = 0
	}
}

type Event int

const (
	Protect Event = iota
	Connected
	Disconnected
	Success
	Fail
)

func (b *Book) Update(id ID, event Event) {
	addr := b.known[id]
	if addr == nil {
		return
	}
	switch event {
	case Protect:
		addr.protected = true
		addr.Class = good
		b.shareable = append(b.shareable, addr)
	case Connected:
		addr.Connected = true
	case Disconnected:
		addr.Connected = false
	case Success, Fail:
		b.queue.PushBack(addr)
		if addr.protected {
			return
		}
		if event == Success {
			addr.success++
			addr.failures = 0
		} else if event == Fail {
			addr.failures++
			addr.success = 0
		}
		c := classify(addr)
		if addr.Class == unknown && c > unknown {
			b.shareable = append(b.shareable, addr)
		} else if addr.Class == unknown && c < unknown {
			delete(b.known, id)
		}
		addr.Class = c
	}

}

func (b *Book) DrainQueue(n int) []Address {
	return take(n, b.drainQueue())
}

func (b *Book) TakeShareable(src ID, n int) []Address {
	addr := b.known[src]
	if addr == nil {
		return nil
	}
	return take(n, b.iterShareable(src, addr.bucket))
}

func (b *Book) iterShareable(src ID, bucket bucket) iterator {
	b.rng.Shuffle(len(b.shareable), func(i, j int) {
		b.shareable[i], b.shareable[j] = b.shareable[j], b.shareable[i]
	})
	i := 0
	return func() Address {
		for {
			if i == len(b.shareable) {
				return ""
			}
			rst := b.shareable[i]
			if rst.Class <= unknown {
				copy(b.shareable[i:], b.shareable[i+1:])
				b.shareable[len(b.shareable)-1] = nil
				b.shareable = b.shareable[:len(b.shareable)-1]
			}
			i++
			if rst.bucket == bucket && rst.id != src {
				return rst.Raw
			}
		}
	}
}

func (b *Book) drainQueue() iterator {
	return func() Address {
		for {
			if b.queue.Len() == 0 {
				return ""
			}
			rst := b.queue.Remove(b.queue.Front())
			if rst.(*addressInfo).Class == deleted {
				continue
			}
			return rst.(*addressInfo).Raw
		}
	}
}

func take(n int, next iterator) []Address {
	rst := make([]Address, 0, n)
	for addr := next(); addr != ""; addr = next() {
		rst = append(rst, addr)
	}
	return rst
}

type iterator func() Address
