package book

import (
	"bufio"
	"container/list"
	"encoding/json"
	"fmt"
	"hash/crc64"
	"io"
	"math/rand"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

const SELF ID = "SELF"

type ID = string

type AddrInfo = peer.AddrInfo

type Address = ma.Multiaddr

type class uint32

const (
	deleted class = iota
	stale
	learned
	stable
)

func classify(addr *addressInfo) class {
	switch addr.Class {
	case stale:
		if addr.success == 2 {
			return stable
		} else if addr.failures == 2 {
			return deleted
		}
	case learned:
		if addr.success == 2 {
			return stable
		} else if addr.failures == 1 {
			return stale
		}
	case stable:
		if addr.failures == 2 {
			return stale
		}
	}
	return addr.Class
}

type bucket int

const (
	private = iota
	public
)

func isDns(raw Address) bool {
	for _, protocol := range raw.Protocols() {
		if protocol.Code == ma.P_DNS ||
			protocol.Code == ma.P_DNS4 ||
			protocol.Code == ma.P_DNS6 ||
			protocol.Code == ma.P_DNSADDR {
			return true
		}
	}
	return false
}

func bucketize(raw Address) bucket {
	if manet.IsPublicAddr(raw) || isDns(raw) {
		return public
	}
	return private
}

type addressInfo struct {
	// exported values will be persisted
	ID        ID          `json:"id"`
	Raw       jsonAddress `json:"raw"`
	Class     class       `json:"class"`
	Connected bool        `json:"connected"`

	shareable bool // true if item is in shareable array
	bucket    bucket
	protected bool
	failures  int
	success   int
}

type jsonAddress struct {
	Address
}

func (u *jsonAddress) UnmarshalJSON(data []byte) error {
	// i didn't manage to find a way to use UnmarshalJSON method on the
	// private multiaddr type
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	addr, err := ma.NewMultiaddr(s)
	if err != nil {
		return err
	}
	u.Address = addr
	return nil
}

type Opt func(*Book)

// WithLimit updates default book limit.
func WithLimit(limit int) Opt {
	return func(b *Book) {
		b.limit = limit
	}
}

// WithRand overwrites default seed for determinism in tests.
func WithRand(seed int64) Opt {
	return func(b *Book) {
		b.rng = rand.New(rand.NewSource(seed))
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
	mu        sync.Mutex
	limit     int
	known     map[ID]*addressInfo
	queue     *list.List
	rng       *rand.Rand
	shareable []*addressInfo
}

func (b *Book) Add(src, id ID, raw Address) {
	b.mu.Lock()
	defer b.mu.Unlock()
	addr := b.known[id]
	bucket := bucketize(raw)
	if src != SELF {
		if source := b.known[src]; source == nil || source.bucket != bucket {
			return
		}
	}
	if addr == nil && len(b.known) == b.limit {
		// future improvement is to find any stale and replace it with learned
		return
	} else if addr == nil {
		addr = &addressInfo{
			Raw:       jsonAddress{raw},
			Class:     learned,
			ID:        id,
			bucket:    bucket,
			shareable: true,
		}
		b.shareable = append(b.shareable, addr)
		b.queue.PushBack(addr)
		b.known[id] = addr
	} else if addr.Raw.Address != raw {
		addr.Raw.Address = raw
		addr.bucket = bucket
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

func (b *Book) Update(id ID, events ...Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	addr := b.known[id]
	if addr == nil {
		return
	}
	for _, event := range events {
		switch event {
		case Protect:
			addr.protected = true
			addr.Class = stable
			if !addr.shareable {
				addr.shareable = true
				b.shareable = append(b.shareable, addr)
			}
		case Connected:
			addr.Connected = true
		case Disconnected:
			addr.Connected = false
		case Success, Fail:
			b.queue.PushBack(addr)
			if addr.protected {
				continue
			}
			if event == Success {
				addr.success++
				addr.failures = 0
			} else if event == Fail {
				addr.failures++
				addr.success = 0
			}
			c := classify(addr)
			if addr.Class != c {
				addr.failures = 0
				addr.success = 0
			}
			if addr.Class == stale && c > stale && !addr.shareable {
				addr.shareable = true
				b.shareable = append(b.shareable, addr)
			} else if addr.Class == stale && c < stale {
				delete(b.known, id)
			}
			addr.Class = c
		}
	}
}

func (b *Book) DrainQueue(n int) []Address {
	b.mu.Lock()
	defer b.mu.Unlock()
	return take(n, b.drainQueue())
}

func (b *Book) TakeShareable(src ID, n int) []Address {
	b.mu.Lock()
	defer b.mu.Unlock()
	addr := b.known[src]
	if addr == nil {
		return nil
	}
	return take(n, b.iterShareable(src, addr.bucket))
}

func (b *Book) Persist(w io.Writer) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return persist(b.known, w)
}

func (b *Book) Recover(r io.Reader) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := recover(b.known, r); err != nil {
		return err
	}
	queue := []*addressInfo{}
	for _, addr := range b.known {
		addr.bucket = bucketize(addr.Raw.Address)
		queue = append(queue, addr)
		if addr.Class >= learned {
			b.shareable = append(b.shareable, addr)
			addr.shareable = true
		}
	}
	// prioritize establishing connections with previously connected addresses
	sort.Slice(queue, func(i, j int) bool {
		if queue[i].Connected && !queue[j].Connected {
			return true
		}
		return queue[i].Class < queue[j].Class
	})
	for _, addr := range queue {
		b.queue.PushBack(addr)
	}
	return nil
}

func (b *Book) iterShareable(src ID, bucket bucket) iterator {
	b.rng.Shuffle(len(b.shareable), func(i, j int) {
		b.shareable[i], b.shareable[j] = b.shareable[j], b.shareable[i]
	})
	i := 0
	return func() Address {
		for {
			if i == len(b.shareable) {
				return nil
			}
			rst := b.shareable[i]
			if rst.Class <= stale {
				copy(b.shareable[i:], b.shareable[i+1:])
				b.shareable[len(b.shareable)-1] = nil
				b.shareable = b.shareable[:len(b.shareable)-1]
				rst.shareable = false
			} else {
				i++
				if rst.bucket == bucket && rst.ID != src {
					return rst.Raw.Address
				}
			}
		}
	}
}

func (b *Book) drainQueue() iterator {
	return func() Address {
		for {
			if b.queue.Len() == 0 {
				return nil
			}
			rst := b.queue.Remove(b.queue.Front())
			if rst.(*addressInfo).Class == deleted {
				continue
			}
			return rst.(*addressInfo).Raw.Address
		}
	}
}

type iterator func() Address

func take(n int, next iterator) []Address {
	rst := make([]Address, 0, n)
	for addr := next(); addr != nil; addr = next() {
		rst = append(rst, addr)
		if len(rst) == cap(rst) {
			return rst
		}
	}
	return rst
}

func persist(known map[ID]*addressInfo, w io.Writer) error {
	checksum := crc64.New(crc64.MakeTable(crc64.ISO))
	encoder := json.NewEncoder(io.MultiWriter(w, checksum))
	sorted := make([]ID, 0, len(known))
	for addr := range known {
		sorted = append(sorted, addr)
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})
	for _, id := range sorted {
		addr := known[id]
		if err := encoder.Encode(addr); err != nil {
			return fmt.Errorf("json encoder failure for obj (%v): %w", addr, err)
		}
	}
	if _, err := fmt.Fprintf(w, "%s", strconv.FormatUint(checksum.Sum64(), 10)); err != nil {
		return fmt.Errorf("write checksum: %v", err)
	}
	return nil
}

func recover(known map[ID]*addressInfo, r io.Reader) error {
	checksum := crc64.New(crc64.MakeTable(crc64.ISO))
	scanner := bufio.NewScanner(r)
	stored := uint64(0)
	for scanner.Scan() {
		if len(scanner.Bytes()) == 0 {
			return fmt.Errorf("corrupted data: empty lines are not expected")
		}
		if scanner.Bytes()[0] != '{' {
			var err error
			stored, err = strconv.ParseUint(scanner.Text(), 10, 64)
			if err != nil {
				return fmt.Errorf("parse uint %s: %w", scanner.Text(), err)
			}
			break
		}
		addr := &addressInfo{}
		if err := json.Unmarshal(scanner.Bytes(), addr); err != nil {
			return fmt.Errorf("unmarshal %s: %w", scanner.Text(), err)
		}
		checksum.Write(scanner.Bytes())
		checksum.Write([]byte{'\n'})
		known[addr.ID] = addr
	}
	if stored != checksum.Sum64() {
		return fmt.Errorf("stored checksum %d doesn't match computed %d", stored, checksum.Sum64())
	}
	return nil
}
