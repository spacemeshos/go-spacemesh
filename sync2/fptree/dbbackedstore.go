package fptree

import (
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
	"github.com/spacemeshos/go-spacemesh/sync2/sqlstore"
)

// DBBackedStore is an implementation of IDStore that keeps track of the rows in a
// database table, using an FPTree to store items that have arrived from a sync peer.
type DBBackedStore struct {
	*sqlstore.SQLIDStore
	*FPTree
}

var _ sqlstore.IDStore = &DBBackedStore{}

// NewDBBackedStore creates a new DB-backed store.
// sizeHint is the expected number of items added to the store via RegisterHash _after_
// the store is created.
func NewDBBackedStore(
	db sql.Executor,
	sts *sqlstore.SyncedTableSnapshot,
	sizeHint int,
	keyLen int,
) *DBBackedStore {
	return &DBBackedStore{
		SQLIDStore: sqlstore.NewSQLIDStore(db, sts, keyLen),
		FPTree:     NewFPTreeWithValues(sizeHint, keyLen),
	}
}

// Clone creates a copy of the store.
// Implements IDStore.Clone.
func (s *DBBackedStore) Clone() sqlstore.IDStore {
	return &DBBackedStore{
		SQLIDStore: s.SQLIDStore.Clone().(*sqlstore.SQLIDStore),
		FPTree:     s.FPTree.Clone().(*FPTree),
	}
}

// RegisterKey adds a hash to the store, using the FPTree so that the underlying database
// table is unchanged.
// Implements IDStore.
func (s *DBBackedStore) RegisterKey(k rangesync.KeyBytes) error {
	return s.FPTree.RegisterKey(k)
}

// All returns all the items currently in the store.
// Implements IDStore.
func (s *DBBackedStore) All() rangesync.SeqResult {
	return rangesync.CombineSeqs(nil, s.SQLIDStore.All(), s.FPTree.All())
}

// From returns all the items in the store that are greater than or equal to the given key.
// Implements IDStore.
func (s *DBBackedStore) From(from rangesync.KeyBytes, sizeHint int) rangesync.SeqResult {
	return rangesync.CombineSeqs(
		from,
		// There may be fewer than sizeHint to be loaded from the database as some
		// may be in FPTree, but for most cases that will do.
		s.SQLIDStore.From(from, sizeHint),
		s.FPTree.From(from, sizeHint))
}

// SetSnapshot sets the table snapshot to be used by the store.
func (s *DBBackedStore) SetSnapshot(sts *sqlstore.SyncedTableSnapshot) {
	s.SQLIDStore.SetSnapshot(sts)
	s.FPTree.Clear()
}

// Release releases resources used by the store.
func (s *DBBackedStore) Release() {
	s.FPTree.Release()
}
