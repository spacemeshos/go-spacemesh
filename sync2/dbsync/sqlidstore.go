package dbsync

import (
	"bytes"

	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sync2/hashsync"
)

const sqlMaxChunkSize = 1024

type sqlIDStore struct {
	db     sql.Database
	query  string
	keyLen int
	cache  *lru
}

var _ idStore = &sqlIDStore{}

func newSQLIDStore(db sql.Database, query string, keyLen int) *sqlIDStore {
	return &sqlIDStore{
		db:     db,
		query:  query,
		keyLen: keyLen,
		cache:  newLRU(),
	}
}

func (s *sqlIDStore) clone() idStore {
	return newSQLIDStore(s.db, s.query, s.keyLen)
}

func (s *sqlIDStore) registerHash(h KeyBytes) error {
	// should be registered by the handler code
	return nil
}

func (s *sqlIDStore) start() hashsync.Iterator {
	// TODO: should probably use a different query to get the first key
	return s.iter(make(KeyBytes, s.keyLen))
}

func (s *sqlIDStore) iter(from KeyBytes) hashsync.Iterator {
	if len(from) != s.keyLen {
		panic("BUG: invalid key length")
	}
	return newDBRangeIterator(s.db, s.query, from, sqlMaxChunkSize, s.cache)
}

type dbBackedStore struct {
	*sqlIDStore
	*inMemIDStore
}

var _ idStore = &dbBackedStore{}

func newDBBackedStore(db sql.Database, query string, keyLen int) *dbBackedStore {
	return &dbBackedStore{
		sqlIDStore:   newSQLIDStore(db, query, keyLen),
		inMemIDStore: newInMemIDStore(keyLen),
	}
}

func (s *dbBackedStore) clone() idStore {
	return &dbBackedStore{
		sqlIDStore:   s.sqlIDStore.clone().(*sqlIDStore),
		inMemIDStore: s.inMemIDStore.clone().(*inMemIDStore),
	}
}

func (s *dbBackedStore) registerHash(h KeyBytes) error {
	return s.inMemIDStore.registerHash(h)
}

func (s *dbBackedStore) start() hashsync.Iterator {
	dbIt := s.sqlIDStore.start()
	memIt := s.inMemIDStore.start()
	return combineIterators(nil, dbIt, memIt)
}

func (s *dbBackedStore) iter(from KeyBytes) hashsync.Iterator {
	dbIt := s.sqlIDStore.iter(from)
	memIt := s.inMemIDStore.iter(from)
	return combineIterators(from, dbIt, memIt)
}

func idWithinInterval(id, x, y KeyBytes, itype int) bool {
	switch itype {
	case 0:
		return true
	case -1:
		return bytes.Compare(id, x) >= 0 && bytes.Compare(id, y) < 0
	default:
		return bytes.Compare(id, y) < 0 || bytes.Compare(id, x) >= 0
	}
}
