package dbsync

import (
	"context"
	"errors"
	"sync"

	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sync2/hashsync"
)

type DBItemStore struct {
	loadMtx   sync.Mutex
	loaded    bool
	db        sql.Database
	ft        *fpTree
	loadQuery string
	iterQuery string
	keyLen    int
	maxDepth  int
}

var _ hashsync.ItemStore = &DBItemStore{}

func NewDBItemStore(
	db sql.Database,
	loadQuery, iterQuery string,
	keyLen, maxDepth int,
) *DBItemStore {
	var np nodePool
	dbStore := newDBBackedStore(db, iterQuery, keyLen)
	return &DBItemStore{
		db:        db,
		ft:        newFPTree(&np, dbStore, keyLen, maxDepth),
		loadQuery: loadQuery,
		iterQuery: iterQuery,
		keyLen:    keyLen,
		maxDepth:  maxDepth,
	}
}

func (d *DBItemStore) load() error {
	_, err := d.db.Exec(d.loadQuery, nil,
		func(stmt *sql.Statement) bool {
			id := make(KeyBytes, d.keyLen) // TODO: don't allocate new ID
			stmt.ColumnBytes(0, id[:])
			d.ft.addStoredHash(id)
			return true
		})
	return err
}

func (d *DBItemStore) EnsureLoaded() error {
	d.loadMtx.Lock()
	defer d.loadMtx.Unlock()
	if !d.loaded {
		if err := d.load(); err != nil {
			return err
		}
		d.loaded = true
	}
	return nil
}

// Add implements hashsync.ItemStore.
func (d *DBItemStore) Add(ctx context.Context, k hashsync.Ordered) error {
	d.EnsureLoaded()
	has, err := d.Has(k) // TODO: this check shouldn't be needed
	if has || err != nil {
		return err
	}
	return d.ft.addHash(k.(KeyBytes))
}

// GetRangeInfo implements hashsync.ItemStore.
func (d *DBItemStore) GetRangeInfo(
	preceding hashsync.Iterator,
	x, y hashsync.Ordered,
	count int,
) (hashsync.RangeInfo, error) {
	d.EnsureLoaded()
	fpr, err := d.ft.fingerprintInterval(x.(KeyBytes), y.(KeyBytes), count)
	if err != nil {
		return hashsync.RangeInfo{}, err
	}
	return hashsync.RangeInfo{
		Fingerprint: fpr.fp,
		Count:       int(fpr.count),
		Start:       fpr.start,
		End:         fpr.end,
	}, nil
}

// Min implements hashsync.ItemStore.
func (d *DBItemStore) Min() (hashsync.Iterator, error) {
	d.EnsureLoaded()
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
	d.EnsureLoaded()
	return &DBItemStore{
		db:        d.db,
		ft:        d.ft.clone(),
		loadQuery: d.loadQuery,
		iterQuery: d.iterQuery,
		keyLen:    d.keyLen,
		maxDepth:  d.maxDepth,
		loaded:    true,
	}
}

// Has implements hashsync.ItemStore.
func (d *DBItemStore) Has(k hashsync.Ordered) (bool, error) {
	d.EnsureLoaded()
	// TODO: should often be able to avoid querying the database if we check the key
	// against the fptree
	it, err := d.ft.iter(k.(KeyBytes))
	if err == nil {
		return k.Compare(it.Key()) == 0, nil
	}
	if err != errEmptySet {
		return false, err
	}
	return false, nil
}
