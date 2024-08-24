package dbsync

import (
	"bytes"
	"context"

	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sync2/hashsync"
)

const sqlMaxChunkSize = 1024

type dbExecKey struct{}

func WithSQLExec(ctx context.Context, db sql.Executor) context.Context {
	return context.WithValue(ctx, dbExecKey{}, db)
}

func ContextSQLExec(ctx context.Context, db sql.Executor) sql.Executor {
	v := ctx.Value(dbExecKey{})
	if v == nil {
		return db
	}
	return v.(sql.Executor)
}

func WithSQLTransaction(ctx context.Context, db sql.Database, toCall func(context.Context) error) error {
	return db.WithTx(ctx, func(tx sql.Transaction) error {
		return toCall(WithSQLExec(ctx, tx))
	})
}

type sqlIDStore struct {
	db     sql.Executor
	sts    *SyncedTableSnapshot
	keyLen int
	cache  *lru
}

var _ idStore = &sqlIDStore{}

func newSQLIDStore(db sql.Executor, sts *SyncedTableSnapshot, keyLen int) *sqlIDStore {
	return &sqlIDStore{
		db:     db,
		sts:    sts,
		keyLen: keyLen,
		cache:  newLRU(),
	}
}

func (s *sqlIDStore) clone() idStore {
	return newSQLIDStore(s.db, s.sts, s.keyLen)
}

func (s *sqlIDStore) registerHash(h KeyBytes) error {
	// should be registered by the handler code
	return nil
}

func (s *sqlIDStore) start(ctx context.Context) hashsync.Iterator {
	// TODO: should probably use a different query to get the first key
	return s.iter(ctx, make(KeyBytes, s.keyLen))
}

func (s *sqlIDStore) iter(ctx context.Context, from KeyBytes) hashsync.Iterator {
	if len(from) != s.keyLen {
		panic("BUG: invalid key length")
	}
	return newDBRangeIterator(ContextSQLExec(ctx, s.db), s.sts, from, sqlMaxChunkSize, s.cache)
}

type dbBackedStore struct {
	*sqlIDStore
	*inMemIDStore
}

var _ idStore = &dbBackedStore{}

func newDBBackedStore(db sql.Executor, sts *SyncedTableSnapshot, keyLen int) *dbBackedStore {
	return &dbBackedStore{
		sqlIDStore:   newSQLIDStore(db, sts, keyLen),
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

func (s *dbBackedStore) start(ctx context.Context) hashsync.Iterator {
	dbIt := s.sqlIDStore.start(ctx)
	memIt := s.inMemIDStore.start(ctx)
	return combineIterators(nil, dbIt, memIt)
}

func (s *dbBackedStore) iter(ctx context.Context, from KeyBytes) hashsync.Iterator {
	dbIt := s.sqlIDStore.iter(ctx, from)
	memIt := s.inMemIDStore.iter(ctx, from)
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
