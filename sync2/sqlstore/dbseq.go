package sqlstore

import (
	"errors"
	"slices"

	"github.com/hashicorp/golang-lru/v2/simplelru"

	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

// dbIDKey is a key for the LRU cache of ID chunks.
type dbIDKey struct {
	//nolint:unused
	id string
	//nolint:unused
	chunkSize int
}

// LRU cache for ID chunks.
type lru = simplelru.LRU[dbIDKey, []rangesync.KeyBytes]

const lruCacheSize = 1024 * 1024

// newLRU creates a new LRU cache for ID chunks.
func newLRU() *lru {
	cache, err := simplelru.NewLRU[dbIDKey, []rangesync.KeyBytes](lruCacheSize, nil)
	if err != nil {
		panic("BUG: failed to create LRU cache: " + err.Error())
	}
	return cache
}

// dbSeq represents a sequence of IDs from a database table.
type dbSeq struct {
	// database
	db sql.Executor
	// starting point
	from rangesync.KeyBytes
	// table snapshot to use
	sts *SyncedTableSnapshot
	// currently used chunk size
	chunkSize int
	// timestamp used to fetch recent IDs
	// (nanoseconds since epoch, 0 if not in the "recent" mode)
	ts int64
	// maximum value for chunkSize
	maxChunkSize int
	// current chunk of items
	chunk []rangesync.KeyBytes
	// position within the current chunk
	pos int
	// lentgh of each key in bytes
	keyLen int
	// true if there is only a single chunk in the sequence.
	// It is set after loading the initial chunk and finding that it's the only one.
	singleChunk bool
	// LRU cache for ID chunks
	cache *lru
}

// idsFromTable iterates over the id field values in a database table.
func idsFromTable(
	db sql.Executor,
	sts *SyncedTableSnapshot,
	from rangesync.KeyBytes,
	ts int64,
	chunkSize int,
	maxChunkSize int,
	lru *lru,
) rangesync.SeqResult {
	if from == nil {
		panic("BUG: makeDBIterator: nil from")
	}
	if maxChunkSize <= 0 {
		panic("BUG: makeDBIterator: chunkSize must be > 0")
	}
	if chunkSize <= 0 {
		chunkSize = 1
	} else if chunkSize > maxChunkSize {
		chunkSize = maxChunkSize
	}
	var err error
	return rangesync.SeqResult{
		Seq: func(yield func(k rangesync.KeyBytes) bool) {
			s := &dbSeq{
				db:           db,
				from:         from.Clone(),
				sts:          sts,
				chunkSize:    chunkSize,
				ts:           ts,
				maxChunkSize: maxChunkSize,
				keyLen:       len(from),
				chunk:        make([]rangesync.KeyBytes, maxChunkSize),
				singleChunk:  false,
				cache:        lru,
			}
			if err = s.load(); err != nil {
				return
			}
			err = s.iterate(yield)
		},
		Error: func() error {
			return err
		},
	}
}

// loadCached loads a chunk of IDs from the LRU cache,
// if possible.
func (s *dbSeq) loadCached(key dbIDKey) (bool, int) {
	if s.cache == nil {
		return false, 0
	}
	chunk, ok := s.cache.Get(key)
	if !ok {
		return false, 0
	}

	for n, id := range s.chunk[:len(chunk)] {
		if id == nil {
			id = make([]byte, s.keyLen)
			s.chunk[n] = id
		}
		copy(id, chunk[n])
	}
	return true, len(chunk)
}

// load makes sure the current chunk is loaded.
func (s *dbSeq) load() error {
	s.pos = 0
	if s.singleChunk {
		// we have a single-chunk DB sequence, don't need to reload,
		// just wrap around
		return nil
	}

	n := 0
	// if the chunk size was reduced due to a short chunk before wraparound, we need
	// to extend it back
	if cap(s.chunk) < s.chunkSize {
		s.chunk = make([]rangesync.KeyBytes, s.chunkSize)
	} else {
		s.chunk = s.chunk[:s.chunkSize]
	}
	key := dbIDKey{string(s.from), s.chunkSize}
	var ierr, err error
	found, n := s.loadCached(key)
	if !found {
		dec := func(stmt *sql.Statement) bool {
			if n >= len(s.chunk) {
				ierr = errors.New("too many rows")
				return false
			}
			// we reuse existing slices when possible for retrieving new IDs
			id := s.chunk[n]
			if id == nil {
				id = make([]byte, s.keyLen)
				s.chunk[n] = id
			}
			stmt.ColumnBytes(0, id)
			n++
			return true
		}
		if s.ts <= 0 {
			err = s.sts.LoadRange(s.db, s.from, s.chunkSize, dec)
		} else {
			err = s.sts.LoadRecent(s.db, s.from, s.chunkSize, s.ts, dec)
		}
		if err == nil && ierr == nil && s.cache != nil {
			cached := make([]rangesync.KeyBytes, n)
			for n, id := range s.chunk[:n] {
				cached[n] = slices.Clone(id)
			}
			s.cache.Add(key, cached)
		}
	}
	fromZero := s.from.IsZero()
	s.chunkSize = min(s.chunkSize*2, s.maxChunkSize)
	switch {
	case err != nil || ierr != nil:
		return errors.Join(ierr, err)
	case n == 0:
		// empty chunk
		if fromZero {
			// already wrapped around or started from 0,
			// the set is empty
			s.chunk = nil
			return nil
		}
		// wrap around
		s.from.Zero()
		return s.load()
	case n < len(s.chunk):
		// short chunk means there are no more items after it,
		// start the next chunk from 0
		s.from.Zero()
		s.chunk = s.chunk[:n]
		// wrapping around on an incomplete chunk that started
		// from 0 means we have just a single chunk
		s.singleChunk = fromZero
	default:
		// use last item incremented by 1 as the start of the next chunk
		copy(s.from, s.chunk[n-1])
		// inc may wrap around if it's 0xffff...fff, but it's fine
		if s.from.Inc() {
			// if we wrapped around and the current chunk started from 0,
			// we have just a single chunk
			s.singleChunk = fromZero
		}
	}
	return nil
}

// iterate iterates over the table rows.
func (s *dbSeq) iterate(yield func(k rangesync.KeyBytes) bool) error {
	if len(s.chunk) == 0 {
		return nil
	}
	for {
		if s.pos >= len(s.chunk) {
			panic("BUG: bad dbSeq position")
		}
		if !yield(slices.Clone(s.chunk[s.pos])) {
			return nil
		}
		s.pos++
		if s.pos >= len(s.chunk) {
			if err := s.load(); err != nil {
				return err
			}
		}
	}
}
