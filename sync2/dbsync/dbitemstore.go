package dbsync

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sync2/hashsync"
)

type DBItemStore struct {
	db        sql.Database
	ft        *fpTree
	query     string
	keyLen    int
	maxDepth  int
	chunkSize int
}

var _ hashsync.ItemStore = &DBItemStore{}

func NewDBItemStore(
	np *nodePool,
	db sql.Database,
	query string,
	keyLen, maxDepth, chunkSize int,
) *DBItemStore {
	dbStore := newDBBackedStore(db, query, keyLen, maxDepth)
	return &DBItemStore{
		db:        db,
		ft:        newFPTree(np, dbStore, maxDepth),
		query:     query,
		keyLen:    keyLen,
		maxDepth:  maxDepth,
		chunkSize: chunkSize,
	}
}

// Add implements hashsync.ItemStore.
func (d *DBItemStore) Add(ctx context.Context, k hashsync.Ordered) error {
	return d.ft.addHash(k.(KeyBytes))
}

func (d *DBItemStore) iter(min, max KeyBytes) (hashsync.Iterator, error) {
	panic("TBD")
	// return newDBRangeIterator(d.db, d.query, min, max, d.chunkSize)
}

// GetRangeInfo implements hashsync.ItemStore.
func (d *DBItemStore) GetRangeInfo(
	preceding hashsync.Iterator,
	x, y hashsync.Ordered,
	count int,
) (hashsync.RangeInfo, error) {
	// QQQQQ: note: iter's max is inclusive!!!!
	// TBD: QQQQQ: need count limiting in ft.fingerprintInterval
	panic("unimplemented")
}

// Min implements hashsync.ItemStore.
func (d *DBItemStore) Min() (hashsync.Iterator, error) {
	// INCORRECT !!! should return nil if the store is empty
	it1 := make(KeyBytes, d.keyLen)
	it2 := make(KeyBytes, d.keyLen)
	for i := range it2 {
		it2[i] = 0xff
	}
	return d.iter(it1, it2)
}

// Copy implements hashsync.ItemStore.
func (d *DBItemStore) Copy() hashsync.ItemStore {
	return &DBItemStore{
		db:        d.db,
		ft:        d.ft.clone(),
		query:     d.query,
		keyLen:    d.keyLen,
		maxDepth:  d.maxDepth,
		chunkSize: d.chunkSize,
	}
}

// Has implements hashsync.ItemStore.
func (d *DBItemStore) Has(k hashsync.Ordered) (bool, error) {
	id := k.(KeyBytes)
	if len(id) < d.keyLen {
		panic("BUG: short key passed")
	}
	tailRefs := []tailRef{
		{ref: load64(id) >> (64 - d.maxDepth), limit: -1},
	}
	found := false
	if err := d.ft.iterateIDs(tailRefs, func(_ tailRef, cur KeyBytes) bool {
		c := id.Compare(cur)
		found = c == 0
		return c > 0
	}); err != nil {
		return false, err
	}
	return found, nil
}
