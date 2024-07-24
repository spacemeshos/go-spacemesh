package dbsync

import (
	"bytes"
	"errors"

	"github.com/spacemeshos/go-spacemesh/sql"
)

const sqlMaxChunkSize = 1024

type sqlIDStore struct {
	db       sql.Database
	query    string
	keyLen   int
	maxDepth int // TBD: remove
}

var _ idStore = &sqlIDStore{}

func newSQLIDStore(db sql.Database, query string, keyLen, maxDepth int) *sqlIDStore {
	return &sqlIDStore{db: db, query: query, keyLen: keyLen, maxDepth: maxDepth}
}

func (s *sqlIDStore) clone() idStore {
	return newSQLIDStore(s.db, s.query, s.keyLen, s.maxDepth)
}

func (s *sqlIDStore) registerHash(h KeyBytes) error {
	// should be registered by the handler code
	return nil
}

func (s *sqlIDStore) start() (iterator, error) {
	return s.iter(make(KeyBytes, s.keyLen))
}

func (s *sqlIDStore) iter(from KeyBytes) (iterator, error) {
	if len(from) != s.keyLen {
		panic("BUG: invalid key length")
	}
	return newDBRangeIterator(s.db, s.query, from, sqlMaxChunkSize)
}

type dbBackedStore struct {
	*sqlIDStore
	*inMemIDStore
	maxDepth int
}

var _ idStore = &dbBackedStore{}

func newDBBackedStore(db sql.Database, query string, keyLen, maxDepth int) *dbBackedStore {
	return &dbBackedStore{
		sqlIDStore:   newSQLIDStore(db, query, keyLen, maxDepth),
		inMemIDStore: newInMemIDStore(keyLen, maxDepth),
		maxDepth:     maxDepth,
	}
}

func (s *dbBackedStore) clone() idStore {
	return &dbBackedStore{
		sqlIDStore:   s.sqlIDStore.clone().(*sqlIDStore),
		inMemIDStore: s.inMemIDStore.clone().(*inMemIDStore),
		maxDepth:     s.maxDepth,
	}
}

func (s *dbBackedStore) registerHash(h KeyBytes) error {
	return s.inMemIDStore.registerHash(h)
}

func (s *dbBackedStore) start() (iterator, error) {
	dbIt, err := s.sqlIDStore.start()
	if err != nil {
		if errors.Is(err, errEmptySet) {
			return s.inMemIDStore.start()
		}
		return nil, err
	}
	memIt, err := s.inMemIDStore.start()
	if err == nil {
		return combineIterators(dbIt, memIt), nil
	} else if errors.Is(err, errEmptySet) {
		return dbIt, nil
	}
	return nil, err
}

func (s *dbBackedStore) iter(from KeyBytes) (iterator, error) {
	dbIt, err := s.sqlIDStore.iter(from)
	if err != nil {
		if errors.Is(err, errEmptySet) {
			return s.inMemIDStore.iter(from)
		}
		return nil, err
	}
	memIt, err := s.inMemIDStore.iter(from)
	if err == nil {
		return combineIterators(dbIt, memIt), nil
	} else if errors.Is(err, errEmptySet) {
		return dbIt, nil
	}
	return nil, err
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
