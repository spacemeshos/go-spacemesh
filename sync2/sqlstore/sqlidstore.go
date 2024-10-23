package sqlstore

import (
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

// max chunk size to use for dbSeq.
const sqlMaxChunkSize = 1024

// SQLIDStore is an implementation of IDStore that is based on a database table snapshot.
type SQLIDStore struct {
	db     sql.Executor
	sts    *SyncedTableSnapshot
	keyLen int
	cache  *lru
}

var _ IDStore = &SQLIDStore{}

// NewSQLIDStore creates a new SQLIDStore.
func NewSQLIDStore(db sql.Executor, sts *SyncedTableSnapshot, keyLen int) *SQLIDStore {
	return &SQLIDStore{
		db:     db,
		sts:    sts,
		keyLen: keyLen,
		cache:  newLRU(),
	}
}

// Clone creates a new SQLIDStore that shares the same database connection and table snapshot.
// Implements IDStore.
func (s *SQLIDStore) Clone() IDStore {
	return NewSQLIDStore(s.db, s.sts, s.keyLen)
}

// RegisterKey is a no-op for SQLIDStore, as the underlying table is never immediately
// updated upon receiving new items.
// Implements IDStore.
func (s *SQLIDStore) RegisterKey(k rangesync.KeyBytes) error {
	// should be registered by the handler code
	return nil
}

// All returns all IDs in the store.
// Implements IDStore.
func (s *SQLIDStore) All() rangesync.SeqResult {
	return s.From(make(rangesync.KeyBytes, s.keyLen), 1)
}

// From returns IDs in the store starting from the given key.
// Implements IDStore.
func (s *SQLIDStore) From(from rangesync.KeyBytes, sizeHint int) rangesync.SeqResult {
	if len(from) != s.keyLen {
		panic("BUG: invalid key length")
	}
	return idsFromTable(s.db, s.sts, from, -1, sizeHint, sqlMaxChunkSize, s.cache)
}

// Since returns IDs in the store starting from the given key and timestamp.
func (s *SQLIDStore) Since(from rangesync.KeyBytes, since int64) (rangesync.SeqResult, int) {
	if len(from) != s.keyLen {
		panic("BUG: invalid key length")
	}
	count, err := s.sts.LoadRecentCount(s.db, since)
	if err != nil {
		return rangesync.ErrorSeqResult(err), 0
	}
	if count == 0 {
		return rangesync.EmptySeqResult(), 0
	}
	return idsFromTable(s.db, s.sts, from, since, 1, sqlMaxChunkSize, nil), count
}

// Sets the table snapshot to use for the store.
func (s *SQLIDStore) SetSnapshot(sts *SyncedTableSnapshot) {
	s.sts = sts
	s.cache.Purge()
}

// Release is a no-op for SQLIDStore.
// Implements IDStore.
func (s *SQLIDStore) Release() {}
