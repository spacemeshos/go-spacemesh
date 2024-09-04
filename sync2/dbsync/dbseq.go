package dbsync

import (
	"errors"
	"slices"

	"github.com/hashicorp/golang-lru/v2/simplelru"

	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sync2/types"
)

type dbIDKey struct {
	id        string
	chunkSize int
}

type lru = simplelru.LRU[dbIDKey, []types.KeyBytes]

const lruCacheSize = 1024 * 1024

func newLRU() *lru {
	cache, err := simplelru.NewLRU[dbIDKey, []types.KeyBytes](lruCacheSize, nil)
	if err != nil {
		panic("BUG: failed to create LRU cache: " + err.Error())
	}
	return cache
}

type dbSeq struct {
	db           sql.Executor
	from         types.KeyBytes
	sts          *SyncedTableSnapshot
	chunkSize    int
	ts           int64
	maxChunkSize int
	chunk        []types.KeyBytes
	pos          int
	keyLen       int
	singleChunk  bool
	cache        *lru
}

// idsFromTable iterates over the id field values in an SQLite table.
func idsFromTable(
	db sql.Executor,
	sts *SyncedTableSnapshot,
	from types.KeyBytes,
	ts int64,
	maxChunkSize int,
	lru *lru,
) types.Seq {
	if from == nil {
		panic("BUG: makeDBIterator: nil from")
	}
	if maxChunkSize <= 0 {
		panic("BUG: makeDBIterator: chunkSize must be > 0")
	}
	return func(yield func(k types.Ordered, err error) bool) {
		s := &dbSeq{
			db:           db,
			from:         from.Clone(),
			sts:          sts,
			chunkSize:    1,
			ts:           ts,
			maxChunkSize: maxChunkSize,
			keyLen:       len(from),
			chunk:        make([]types.KeyBytes, maxChunkSize),
			singleChunk:  false,
			cache:        lru,
		}
		if err := s.load(); err != nil {
			yield(nil, err)
		}
		s.iterate(yield)
	}
}

func (s *dbSeq) loadCached(key dbIDKey) (bool, int) {
	if s.cache == nil {
		return false, 0
	}
	chunk, ok := s.cache.Get(key)
	if !ok {
		// fmt.Fprintf(os.Stderr, "QQQQQ: cache miss\n")
		return false, 0
	}

	// fmt.Fprintf(os.Stderr, "QQQQQ: cache hit, chunk size %d\n", len(chunk))
	for n, id := range s.chunk[:len(chunk)] {
		if id == nil {
			id = make([]byte, s.keyLen)
			s.chunk[n] = id
		}
		copy(id, chunk[n])
	}
	return true, len(chunk)
}

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
		s.chunk = make([]types.KeyBytes, s.chunkSize)
	} else {
		s.chunk = s.chunk[:s.chunkSize]
	}
	// fmt.Fprintf(os.Stderr, "QQQQQ: from: %s chunkSize: %d\n", hex.EncodeToString(it.from), it.chunkSize)
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
			err = s.sts.loadIDRange(s.db, s.from, s.chunkSize, dec)
		} else {
			err = s.sts.loadRecent(s.db, s.from, s.chunkSize, s.ts, dec)
		}
		if err == nil && ierr == nil && s.cache != nil {
			cached := make([]types.KeyBytes, n)
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

func (s *dbSeq) iterate(yield func(k types.Ordered, err error) bool) {
	if len(s.chunk) == 0 {
		return
	}
	for {
		if s.pos >= len(s.chunk) {
			panic("BUG: bad dbSeq position")
		}
		if !yield(slices.Clone(s.chunk[s.pos]), nil) {
			break
		}
		s.pos++
		if s.pos >= len(s.chunk) {
			if err := s.load(); err != nil {
				yield(nil, err)
				return
			}
		}
	}
}
