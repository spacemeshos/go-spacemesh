package dbsync

import (
	"bytes"
	"errors"

	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sync2/hashsync"
)

type KeyBytes []byte

var _ hashsync.Ordered = KeyBytes(nil)

func (k KeyBytes) Compare(other any) int {
	return bytes.Compare(k, other.(KeyBytes))
}

type dbRangeIterator struct {
	db        sql.Database
	from, to  KeyBytes
	query     string
	chunkSize int
	chunk     []KeyBytes
	pos       int
	keyLen    int
}

var _ hashsync.Iterator = &dbRangeIterator{}

// makeDBIterator creates a dbRangeIterator and initializes it from the database.
// Note that [from, to] range is inclusive.
func newDBRangeIterator(
	db sql.Database,
	query string,
	from, to KeyBytes,
	chunkSize int,
) (hashsync.Iterator, error) {
	if from == nil {
		panic("BUG: makeDBIterator: nil from")
	}
	if to == nil {
		panic("BUG: makeDBIterator: nil to")
	}
	if chunkSize <= 0 {
		panic("BUG: makeDBIterator: chunkSize must be > 0")
	}
	it := &dbRangeIterator{
		db:        db,
		from:      from,
		to:        to,
		query:     query,
		chunkSize: chunkSize,
		keyLen:    len(from),
		chunk:     make([]KeyBytes, chunkSize),
	}
	if err := it.load(); err != nil {
		return nil, err
	}
	return it, nil
}

func (it *dbRangeIterator) load() error {
	n := 0
	var ierr error
	_, err := it.db.Exec(
		it.query, func(stmt *sql.Statement) {
			stmt.BindBytes(1, it.from)
			stmt.BindBytes(2, it.to)
			stmt.BindInt64(3, int64(it.chunkSize))
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
	if err != nil || ierr != nil {
		return errors.Join(ierr, err)
	}
	it.pos = 0
	if n < len(it.chunk) {
		// short chunk means there are no more data
		it.from = nil
		it.chunk = it.chunk[:n]
	} else {
		copy(it.from, it.chunk[n-1])
		if incID(it.from) || bytes.Compare(it.from, it.to) >= 0 {
			// no more items after this full chunk
			it.from = nil
		}
	}
	return nil
}

func (it *dbRangeIterator) Key() hashsync.Ordered {
	if it.pos < len(it.chunk) {
		key := make(KeyBytes, it.keyLen)
		copy(key, it.chunk[it.pos])
		return key
	}
	return nil
}

func (it *dbRangeIterator) Next() error {
	if it.pos >= len(it.chunk) {
		return nil
	}
	it.pos++
	if it.pos < len(it.chunk) || it.from == nil {
		return nil
	}
	return it.load()
}

func incID(id []byte) (overflow bool) {
	for i := len(id) - 1; i >= 0; i-- {
		id[i]++
		if id[i] != 0 {
			return false
		}
	}

	return true
}

type concatIterator struct {
	iters []hashsync.Iterator
}

var _ hashsync.Iterator = &concatIterator{}

// concatIterators concatenates multiple iterators into one.
// It assumes that the iterators follow one after another in the order of their keys.
func concatIterators(iters ...hashsync.Iterator) hashsync.Iterator {
	return &concatIterator{iters: iters}
}

func (c *concatIterator) Key() hashsync.Ordered {
	if len(c.iters) == 0 {
		return nil
	}
	return c.iters[0].Key()
}

func (c *concatIterator) Next() error {
	if len(c.iters) == 0 {
		return nil
	}
	if err := c.iters[0].Next(); err != nil {
		return err
	}
	for len(c.iters) > 0 {
		if c.iters[0].Key() != nil {
			break
		}
		c.iters = c.iters[1:]
	}
	return nil
}

type combinedIterator struct {
	iters []hashsync.Iterator
	ahead hashsync.Iterator
}

// combineIterators combines multiple iterators into one.
// Unlike concatIterator, it does not assume that the iterators follow one after another
// in the order of their keys. Instead, it always returns the smallest key among all
// iterators.
func combineIterators(iters ...hashsync.Iterator) hashsync.Iterator {
	return &combinedIterator{iters: iters}
}

func (c *combinedIterator) aheadIterator() hashsync.Iterator {
	if c.ahead == nil {
		if len(c.iters) == 0 {
			return nil
		}
		c.ahead = c.iters[0]
		for i := 1; i < len(c.iters); i++ {
			if c.iters[i].Key() != nil {
				if c.ahead.Key() == nil || c.iters[i].Key().Compare(c.ahead.Key()) < 0 {
					c.ahead = c.iters[i]
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
	if err := c.aheadIterator().Next(); err != nil {
		return err
	}
	c.ahead = nil
	return nil
}
