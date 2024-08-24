package dbsync

import (
	"bytes"
	"encoding/hex"
	"errors"
	"slices"

	"github.com/hashicorp/golang-lru/v2/simplelru"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sync2/hashsync"
)

type KeyBytes []byte

var _ hashsync.Ordered = KeyBytes(nil)

func (k KeyBytes) String() string {
	return hex.EncodeToString(k)
}

func (k KeyBytes) Clone() KeyBytes {
	return slices.Clone(k)
}

func (k KeyBytes) Compare(other any) int {
	return bytes.Compare(k, other.(KeyBytes))
}

func (k KeyBytes) inc() (overflow bool) {
	for i := len(k) - 1; i >= 0; i-- {
		k[i]++
		if k[i] != 0 {
			return false
		}
	}

	return true
}

func (k KeyBytes) zero() {
	for i := range k {
		k[i] = 0
	}
}

func (k KeyBytes) isZero() bool {
	for _, b := range k {
		if b != 0 {
			return false
		}
	}
	return true
}

var errEmptySet = errors.New("empty range")

type dbIDKey struct {
	id        string
	chunkSize int
}

type lru = simplelru.LRU[dbIDKey, []KeyBytes]

const lruCacheSize = 1024 * 1024

func newLRU() *lru {
	cache, err := simplelru.NewLRU[dbIDKey, []KeyBytes](lruCacheSize, nil)
	if err != nil {
		panic("BUG: failed to create LRU cache: " + err.Error())
	}
	return cache
}

type dbRangeIterator struct {
	db           sql.Executor
	from         KeyBytes
	sts          *SyncedTableSnapshot
	chunkSize    int
	maxChunkSize int
	chunk        []KeyBytes
	pos          int
	keyLen       int
	singleChunk  bool
	loaded       bool
	cache        *lru
}

var _ hashsync.Iterator = &dbRangeIterator{}

// makeDBIterator creates a dbRangeIterator and initializes it from the database.
// If query returns no rows even after starting from zero ID, errEmptySet error is returned.
func newDBRangeIterator(
	db sql.Executor,
	sts *SyncedTableSnapshot,
	from KeyBytes,
	maxChunkSize int,
	lru *lru,
) hashsync.Iterator {
	if from == nil {
		panic("BUG: makeDBIterator: nil from")
	}
	if maxChunkSize <= 0 {
		panic("BUG: makeDBIterator: chunkSize must be > 0")
	}
	return &dbRangeIterator{
		db:           db,
		from:         from.Clone(),
		sts:          sts,
		chunkSize:    1,
		maxChunkSize: maxChunkSize,
		keyLen:       len(from),
		chunk:        make([]KeyBytes, maxChunkSize),
		singleChunk:  false,
		loaded:       false,
		cache:        lru,
	}
}

func (it *dbRangeIterator) loadCached(key dbIDKey) (bool, int) {
	chunk, ok := it.cache.Get(key)
	if !ok {
		// fmt.Fprintf(os.Stderr, "QQQQQ: cache miss\n")
		return false, 0
	}

	// fmt.Fprintf(os.Stderr, "QQQQQ: cache hit, chunk size %d\n", len(chunk))
	for n, id := range it.chunk[:len(chunk)] {
		if id == nil {
			id = make([]byte, it.keyLen)
			it.chunk[n] = id
		}
		copy(id, chunk[n])
	}
	return true, len(chunk)
}

func (it *dbRangeIterator) load() error {
	it.pos = 0
	if it.singleChunk {
		// we have a single-chunk DB iterator, don't need to reload,
		// just wrap around
		return nil
	}

	n := 0
	// if the chunk size was reduced due to a short chunk before wraparound, we need
	// to extend it back
	if cap(it.chunk) < it.chunkSize {
		it.chunk = make([]KeyBytes, it.chunkSize)
	} else {
		it.chunk = it.chunk[:it.chunkSize]
	}
	// fmt.Fprintf(os.Stderr, "QQQQQ: from: %s chunkSize: %d\n", hex.EncodeToString(it.from), it.chunkSize)
	key := dbIDKey{string(it.from), it.chunkSize}
	var ierr, err error
	found, n := it.loadCached(key)
	if !found {
		err := it.sts.loadIDRange(
			it.db, it.from, it.chunkSize,
			func(stmt *sql.Statement) bool {
				if n >= len(it.chunk) {
					ierr = errors.New("too many rows")
					return false
				}
				// we reuse existing slices when possible for retrieving new IDs
				id := it.chunk[n]
				if id == nil {
					id = make([]byte, it.keyLen)
					it.chunk[n] = id
				}
				stmt.ColumnBytes(0, id)
				n++
				return true
			})
		if err == nil && ierr == nil {
			cached := make([]KeyBytes, n)
			for n, id := range it.chunk[:n] {
				cached[n] = slices.Clone(id)
			}
			it.cache.Add(key, cached)
		}
	}
	fromZero := it.from.isZero()
	it.chunkSize = min(it.chunkSize*2, it.maxChunkSize)
	switch {
	case err != nil || ierr != nil:
		return errors.Join(ierr, err)
	case n == 0:
		// empty chunk
		if fromZero {
			// already wrapped around or started from 0,
			// the set is empty
			return errEmptySet
		}
		// wrap around
		it.from.zero()
		return it.load()
	case n < len(it.chunk):
		// short chunk means there are no more items after it,
		// start the next chunk from 0
		it.from.zero()
		it.chunk = it.chunk[:n]
		// wrapping around on an incomplete chunk that started
		// from 0 means we have just a single chunk
		it.singleChunk = fromZero
	default:
		// use last item incremented by 1 as the start of the next chunk
		copy(it.from, it.chunk[n-1])
		// inc may wrap around if it's 0xffff...fff, but it's fine
		if it.from.inc() {
			// if we wrapped around and the current chunk started from 0,
			// we have just a single chunk
			it.singleChunk = fromZero
		}
	}
	return nil
}

func (it *dbRangeIterator) Key() (hashsync.Ordered, error) {
	if !it.loaded {
		if err := it.load(); err != nil {
			return nil, err
		}
		it.loaded = true
	}
	if it.pos < len(it.chunk) {
		return slices.Clone(it.chunk[it.pos]), nil
	}
	return nil, errEmptySet
}

func (it *dbRangeIterator) Next() error {
	if !it.loaded {
		if err := it.load(); err != nil {
			return err
		}
		it.loaded = true
		if len(it.chunk) == 0 || it.pos != 0 {
			panic("BUG: load didn't report empty set or set a wrong pos")
		}
		it.pos++
		return nil
	}
	it.pos++
	if it.pos < len(it.chunk) {
		return nil
	}
	return it.load()
}

func (it *dbRangeIterator) Clone() hashsync.Iterator {
	cloned := *it
	cloned.from = slices.Clone(it.from)
	cloned.chunk = make([]KeyBytes, len(it.chunk))
	for i, k := range it.chunk {
		cloned.chunk[i] = slices.Clone(k)
	}
	return &cloned
}

type combinedIterator struct {
	startingPoint hashsync.Ordered
	iters         []hashsync.Iterator
	wrapped       []hashsync.Iterator
	ahead         hashsync.Iterator
	aheadIdx      int
}

// combineIterators combines multiple iterators into one, returning the smallest current
// key among all iterators at each step.
func combineIterators(startingPoint hashsync.Ordered, iters ...hashsync.Iterator) hashsync.Iterator {
	return &combinedIterator{startingPoint: startingPoint, iters: iters}
}

func (c *combinedIterator) begin() error {
	// Some of the iterators may already be wrapped around.
	// This corresponds to the case when we ask an idStore for iterator
	// with a starting point beyond the last key in the store.
	iters := c.iters
	c.iters = nil
	for _, it := range iters {
		k, err := it.Key()
		if err != nil {
			if errors.Is(err, errEmptySet) {
				// ignore empty iterators
				continue
			}
			return err
		}
		if c.startingPoint != nil && k.Compare(c.startingPoint) < 0 {
			c.wrapped = append(c.wrapped, it)
		} else {
			c.iters = append(c.iters, it)
		}
	}
	if len(c.iters) == 0 {
		// all iterators wrapped around
		c.iters = c.wrapped
		c.wrapped = nil
	}
	c.startingPoint = nil
	return nil
}

func (c *combinedIterator) aheadIterator() (hashsync.Iterator, error) {
	if err := c.begin(); err != nil {
		return nil, err
	}
	if c.ahead == nil {
		if len(c.iters) == 0 {
			if len(c.wrapped) == 0 {
				return nil, nil
			}
			c.iters = c.wrapped
			c.wrapped = nil
		}
		c.ahead = c.iters[0]
		c.aheadIdx = 0
		for i := 1; i < len(c.iters); i++ {
			curK, err := c.iters[i].Key()
			if err != nil {
				return nil, err
			}
			if curK != nil {
				aK, err := c.ahead.Key()
				if err != nil {
					return nil, err
				}
				if curK.Compare(aK) < 0 {
					c.ahead = c.iters[i]
					c.aheadIdx = i
				}
			}
		}
	}
	return c.ahead, nil
}

func (c *combinedIterator) Key() (hashsync.Ordered, error) {
	it, err := c.aheadIterator()
	if err != nil {
		return nil, err
	}
	return it.Key()
}

func (c *combinedIterator) Next() error {
	it, err := c.aheadIterator()
	if err != nil {
		return err
	}
	oldKey, err := it.Key()
	if err != nil {
		return err
	}
	if err := it.Next(); err != nil {
		return err
	}
	c.ahead = nil
	newKey, err := it.Key()
	if err != nil {
		return err
	}
	if oldKey.Compare(newKey) >= 0 {
		// the iterator has wrapped around, move it to the wrapped list
		// which will be used after all the iterators have wrapped around
		c.wrapped = append(c.wrapped, it)
		c.iters = append(c.iters[:c.aheadIdx], c.iters[c.aheadIdx+1:]...)
	}
	return nil
}

func (c *combinedIterator) Clone() hashsync.Iterator {
	cloned := &combinedIterator{
		iters:         make([]hashsync.Iterator, len(c.iters)),
		wrapped:       make([]hashsync.Iterator, len(c.wrapped)),
		startingPoint: c.startingPoint,
	}
	for i, it := range c.iters {
		cloned.iters[i] = it.Clone()
	}
	for i, it := range c.wrapped {
		cloned.wrapped[i] = it.Clone()
	}
	return cloned
}
