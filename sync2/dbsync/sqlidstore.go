package dbsync

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sync2/types"
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

func (s *sqlIDStore) registerHash(h types.KeyBytes) error {
	// should be registered by the handler code
	return nil
}

func (s *sqlIDStore) all(ctx context.Context) (types.Seq, error) {
	// TODO: should probably use a different query to get the first key
	return s.from(ctx, make(types.KeyBytes, s.keyLen))
}

func (s *sqlIDStore) from(ctx context.Context, from types.KeyBytes) (types.Seq, error) {
	if len(from) != s.keyLen {
		panic("BUG: invalid key length")
	}
	return idsFromTable(ContextSQLExec(ctx, s.db), s.sts, from, -1, sqlMaxChunkSize, s.cache), nil
}

func (s *sqlIDStore) since(ctx context.Context, from types.KeyBytes, since int64) (types.Seq, int, error) {
	if len(from) != s.keyLen {
		panic("BUG: invalid key length")
	}
	db := ContextSQLExec(ctx, s.db)
	count, err := s.sts.loadRecentCount(db, since)
	if err != nil {
		return nil, 0, err
	}
	if count == 0 {
		return nil, 0, nil
	}
	return idsFromTable(db, s.sts, from, since, sqlMaxChunkSize, nil), count, nil
}

func (s *sqlIDStore) setSnapshot(sts *SyncedTableSnapshot) {
	s.sts = sts
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

func (s *dbBackedStore) registerHash(h types.KeyBytes) error {
	return s.inMemIDStore.registerHash(h)
}

func (s *dbBackedStore) all(ctx context.Context) (types.Seq, error) {
	dbSeq, err := s.sqlIDStore.all(ctx)
	if err != nil {
		return nil, err
	}
	memSeq, err := s.inMemIDStore.all(ctx)
	if err != nil {
		return nil, err
	}
	return combineSeqs(nil, dbSeq, memSeq), nil
}

func (s *dbBackedStore) from(ctx context.Context, from types.KeyBytes) (types.Seq, error) {
	dbSeq, err := s.sqlIDStore.from(ctx, from)
	if err != nil {
		return nil, err
	}
	memSeq, err := s.inMemIDStore.from(ctx, from)
	if err != nil {
		return nil, err
	}
	return combineSeqs(from, dbSeq, memSeq), nil
}
