package dbsync

import (
	"context"
	"errors"

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
		ft:        newFPTree(np, dbStore, keyLen, maxDepth),
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

// GetRangeInfo implements hashsync.ItemStore.
func (d *DBItemStore) GetRangeInfo(
	preceding hashsync.Iterator,
	x, y hashsync.Ordered,
	count int,
) (hashsync.RangeInfo, error) {
	fpr, err := d.ft.fingerprintInterval(x.(KeyBytes), y.(KeyBytes), count)
	if err != nil {
		return hashsync.RangeInfo{}, err
	}
	return hashsync.RangeInfo{
		Fingerprint: fpr.fp,
		Count:       count,
		Start:       fpr.start,
		End:         fpr.end,
	}, nil
}

// Min implements hashsync.ItemStore.
func (d *DBItemStore) Min() (hashsync.Iterator, error) {
	it, err := d.ft.start()
	switch {
	case err == nil:
		return it, nil
	case errors.Is(err, errEmptySet):
		return nil, nil
	default:
		return nil, err
	}
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
	it, err := d.ft.iter(k.(KeyBytes))
	if err == nil {
		return k.Compare(it.Key()) == 0, nil
	}
	if err != errEmptySet {
		return false, err
	}
	return false, nil
}
