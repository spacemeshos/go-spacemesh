package dbsync

import (
	"bytes"
	"errors"
	"slices"

	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sync2/hashsync"
)

type KeyBytes []byte

var _ hashsync.Ordered = KeyBytes(nil)

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
	db          sql.Database
	from        KeyBytes
	query       string
	chunkSize   int
	chunk       []KeyBytes
	pos         int
	keyLen      int
	singleChunk bool
}

var _ hashsync.Iterator = &dbRangeIterator{}

// makeDBIterator creates a dbRangeIterator and initializes it from the database.
// If query returns no rows even after starting from zero ID, errEmptySet error is returned.
func newDBRangeIterator(
	db sql.Database,
	query string,
	from KeyBytes,
	chunkSize int,
) (hashsync.Iterator, error) {
	if from == nil {
		panic("BUG: makeDBIterator: nil from")
	}
	if chunkSize <= 0 {
		panic("BUG: makeDBIterator: chunkSize must be > 0")
	}
	it := &dbRangeIterator{
		db:          db,
		from:        from.Clone(),
		query:       query,
		chunkSize:   chunkSize,
		keyLen:      len(from),
		chunk:       make([]KeyBytes, chunkSize),
		singleChunk: false,
	}
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
	it.chunk = it.chunk[:it.chunkSize]
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
	if it.pos < len(it.chunk) {
		return nil
	}
	return it.load()
}
