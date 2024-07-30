package dbsync

import (
	"bytes"
	"encoding/hex"
	"errors"
	"slices"

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

type dbRangeIterator struct {
	db           sql.Database
	from         KeyBytes
	query        string
	chunkSize    int
	maxChunkSize int
	chunk        []KeyBytes
	pos          int
	keyLen       int
	singleChunk  bool
}

var _ iterator = &dbRangeIterator{}

// makeDBIterator creates a dbRangeIterator and initializes it from the database.
// If query returns no rows even after starting from zero ID, errEmptySet error is returned.
func newDBRangeIterator(
	db sql.Database,
	query string,
	from KeyBytes,
	maxChunkSize int,
) (iterator, error) {
	if from == nil {
		panic("BUG: makeDBIterator: nil from")
	}
	if maxChunkSize <= 0 {
		panic("BUG: makeDBIterator: chunkSize must be > 0")
	}
	it := &dbRangeIterator{
		db:           db,
		from:         from.Clone(),
		query:        query,
		chunkSize:    1,
		maxChunkSize: maxChunkSize,
		keyLen:       len(from),
		chunk:        make([]KeyBytes, maxChunkSize),
		singleChunk:  false,
	}
	// panic("TBD: QQQQQ: do not preload the iterator! Key should panic upon no entries. With from > max item, iterator should work, wrapping around (TEST)!")
	// panic("TBD: QQQQQ: Key() should return an error!")
	if err := it.load(); err != nil {
		return nil, err
	}
	return it, nil
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
	var ierr error
	_, err := it.db.Exec(
		it.query, func(stmt *sql.Statement) {
			stmt.BindBytes(1, it.from)
			stmt.BindInt64(2, int64(it.chunkSize))
		},
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

func (it *dbRangeIterator) Key() hashsync.Ordered {
	if it.pos < len(it.chunk) {
		return slices.Clone(it.chunk[it.pos])
	}
	return nil
}

func (it *dbRangeIterator) Next() error {
	if it.pos >= len(it.chunk) {
		return nil
	}
	it.pos++
	if it.pos < len(it.chunk) {
		return nil
	}
	return it.load()
}

func (it *dbRangeIterator) clone() iterator {
	cloned := *it
	cloned.from = slices.Clone(it.from)
	cloned.chunk = make([]KeyBytes, len(it.chunk))
	for i, k := range it.chunk {
		cloned.chunk[i] = slices.Clone(k)
	}
	return &cloned
}

type combinedIterator struct {
	iters    []iterator
	wrapped  []iterator
	ahead    iterator
	aheadIdx int
}

// combineIterators combines multiple iterators into one, returning the smallest current
// key among all iterators at each step.
func combineIterators(startingPoint hashsync.Ordered, iters ...iterator) iterator {
	var c combinedIterator
	// Some of the iterators may already be wrapped around.
	// This corresponds to the case when we ask an idStore for iterator
	// with a starting point beyond the last key in the store.
	if startingPoint == nil {
		c.iters = iters
	} else {
		for _, it := range iters {
			if it.Key().Compare(startingPoint) < 0 {
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
	}
	return &c
}

func (c *combinedIterator) aheadIterator() iterator {
	if c.ahead == nil {
		if len(c.iters) == 0 {
			if len(c.wrapped) == 0 {
				return nil
			}
			c.iters = c.wrapped
			c.wrapped = nil
		}
		c.ahead = c.iters[0]
		c.aheadIdx = 0
		for i := 1; i < len(c.iters); i++ {
			if c.iters[i].Key() != nil {
				if c.ahead.Key() == nil || c.iters[i].Key().Compare(c.ahead.Key()) < 0 {
					c.ahead = c.iters[i]
					c.aheadIdx = i
				}
			}
		}
	}
	return c.ahead
}

func (c *combinedIterator) Key() hashsync.Ordered {
	return c.aheadIterator().Key()
}

func (c *combinedIterator) Next() error {
	it := c.aheadIterator()
	oldKey := it.Key()
	if err := it.Next(); err != nil {
		return err
	}
	c.ahead = nil
	if oldKey.Compare(it.Key()) >= 0 {
		// the iterator has wrapped around, move it to the wrapped list
		// which will be used after all the iterators have wrapped around
		c.wrapped = append(c.wrapped, it)
		c.iters = append(c.iters[:c.aheadIdx], c.iters[c.aheadIdx+1:]...)
	}
	return nil
}

func (c *combinedIterator) clone() iterator {
	cloned := &combinedIterator{
		iters: make([]iterator, len(c.iters)),
	}
	for i, it := range c.iters {
		cloned.iters[i] = it.clone()
	}
	return cloned
}
