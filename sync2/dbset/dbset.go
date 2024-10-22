package dbset

import (
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sync2/fptree"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
	"github.com/spacemeshos/go-spacemesh/sync2/sqlstore"
)

// DBSet is an implementation of rangesync.OrderedSet that uses an SQL database
// as its backing store. It uses an FPTree to perform efficient range queries.
type DBSet struct {
	loadMtx  sync.Mutex
	db       sql.Executor
	ft       *fptree.FPTree
	st       *sqlstore.SyncedTable
	snapshot *sqlstore.SyncedTableSnapshot
	dbStore  *fptree.DBBackedStore
	keyLen   int
	maxDepth int
	received map[string]struct{}
}

var _ rangesync.OrderedSet = &DBSet{}

// NewDBSet creates a new DBSet.
func NewDBSet(
	db sql.Executor,
	st *sqlstore.SyncedTable,
	keyLen, maxDepth int,
) *DBSet {
	return &DBSet{
		db:       db,
		st:       st,
		keyLen:   keyLen,
		maxDepth: maxDepth,
	}
}

func (d *DBSet) handleIDfromDB(stmt *sql.Statement) bool {
	id := make(rangesync.KeyBytes, d.keyLen)
	stmt.ColumnBytes(0, id[:])
	d.ft.AddStoredKey(id)
	return true
}

// EnsureLoaded ensures that the DBSet is loaded and ready to be used.
func (d *DBSet) EnsureLoaded() error {
	d.loadMtx.Lock()
	defer d.loadMtx.Unlock()
	if d.ft != nil {
		return nil
	}
	var err error
	d.snapshot, err = d.st.Snapshot(d.db)
	if err != nil {
		return fmt.Errorf("error taking snapshot: %w", err)
	}
	count, err := d.snapshot.LoadCount(d.db)
	if err != nil {
		return fmt.Errorf("error loading count: %w", err)
	}
	d.dbStore = fptree.NewDBBackedStore(d.db, d.snapshot, count, d.keyLen)
	d.ft = fptree.NewFPTree(count, d.dbStore, d.keyLen, d.maxDepth)
	return d.snapshot.Load(d.db, d.handleIDfromDB)
}

// Received returns a sequence of all items that have been received.
// Implements rangesync.OrderedSet.
func (d *DBSet) Received() rangesync.SeqResult {
	return rangesync.SeqResult{
		Seq: func(yield func(k rangesync.KeyBytes) bool) {
			for k := range d.received {
				if !yield(rangesync.KeyBytes(k)) {
					return
				}
			}
		},
		Error: rangesync.NoSeqError,
	}
}

// Add adds an item to the DBSet.
// Implements rangesync.OrderedSet.
func (d *DBSet) Add(k rangesync.KeyBytes) error {
	if has, err := d.Has(k); err != nil {
		return fmt.Errorf("checking if item exists: %w", err)
	} else if has {
		return nil
	}
	d.ft.RegisterKey(k)
	return nil
}

// Receive handles a newly received item, arranging for it to be returned as part of the
// sequence returned by Received.
// Implements rangesync.OrderedSet.
func (d *DBSet) Receive(k rangesync.KeyBytes) error {
	if d.received == nil {
		d.received = make(map[string]struct{})
	}
	d.received[string(k)] = struct{}{}
	return nil
}

func (d *DBSet) firstItem() (rangesync.KeyBytes, error) {
	if err := d.EnsureLoaded(); err != nil {
		return nil, err
	}
	return d.ft.All().First()
}

// GetRangeInfo returns information about the range of items in the DBSet.
// Implements rangesync.OrderedSet.
func (d *DBSet) GetRangeInfo(x, y rangesync.KeyBytes) (rangesync.RangeInfo, error) {
	if err := d.EnsureLoaded(); err != nil {
		return rangesync.RangeInfo{}, err
	}
	if d.ft.Count() == 0 {
		return rangesync.RangeInfo{
			Items: rangesync.EmptySeqResult(),
		}, nil
	}
	if x == nil || y == nil {
		if x != nil || y != nil {
			panic("BUG: GetRangeInfo called with one of x/y nil but not both")
		}
		var err error
		x, err = d.firstItem()
		if err != nil {
			return rangesync.RangeInfo{}, fmt.Errorf("getting first item: %w", err)
		}
		y = x
	}
	fpr, err := d.ft.FingerprintInterval(x, y, -1)
	if err != nil {
		return rangesync.RangeInfo{}, err
	}
	return rangesync.RangeInfo{
		Fingerprint: fpr.FP,
		Count:       int(fpr.Count),
		Items:       fpr.Items,
	}, nil
}

// SplitRange splits the range of items in the DBSet into two parts,
// returning information about eachn part and the middle item.
// Implements rangesync.OrderedSet.
func (d *DBSet) SplitRange(x, y rangesync.KeyBytes, count int) (rangesync.SplitInfo, error) {
	if count <= 0 {
		panic("BUG: bad split count")
	}

	if err := d.EnsureLoaded(); err != nil {
		return rangesync.SplitInfo{}, err
	}

	sr, err := d.ft.Split(x, y, count)
	if err != nil {
		return rangesync.SplitInfo{}, err
	}

	return rangesync.SplitInfo{
		Parts: [2]rangesync.RangeInfo{
			{
				Fingerprint: sr.Part0.FP,
				Count:       int(sr.Part0.Count),
				Items:       sr.Part0.Items,
			},
			{
				Fingerprint: sr.Part1.FP,
				Count:       int(sr.Part1.Count),
				Items:       sr.Part1.Items,
			},
		},
		Middle: sr.Middle,
	}, nil
}

// Items returns a sequence of all items in the DBSet.
// Implements rangesync.OrderedSet.
func (d *DBSet) Items() rangesync.SeqResult {
	if err := d.EnsureLoaded(); err != nil {
		return rangesync.ErrorSeqResult(err)
	}
	return d.ft.All()
}

// Empty returns true if the DBSet is empty.
// Implements rangesync.OrderedSet.
func (d *DBSet) Empty() (bool, error) {
	if err := d.EnsureLoaded(); err != nil {
		return false, err
	}
	return d.ft.Count() == 0, nil
}

// Advance advances the DBSet to the latest state of the underlying database table.
func (d *DBSet) Advance() error {
	d.loadMtx.Lock()
	defer d.loadMtx.Unlock()
	if d.ft == nil {
		// FIXME
		panic("BUG: can't advance the DBItemStore before it's loaded")
	}
	oldSnapshot := d.snapshot
	var err error
	d.snapshot, err = d.st.Snapshot(d.db)
	if err != nil {
		return fmt.Errorf("error taking snapshot: %w", err)
	}
	d.dbStore.SetSnapshot(d.snapshot)
	return d.snapshot.LoadSinceSnapshot(d.db, oldSnapshot, d.handleIDfromDB)
}

// Copy creates a copy of the DBSet.
// Implements rangesync.OrderedSet.
func (d *DBSet) Copy(syncScope bool) rangesync.OrderedSet {
	d.loadMtx.Lock()
	defer d.loadMtx.Unlock()
	if d.ft == nil {
		// FIXME
		panic("BUG: can't copy the DBItemStore before it's loaded")
	}
	ft := d.ft.Clone().(*fptree.FPTree)
	return &DBSet{
		db:       d.db,
		ft:       ft,
		st:       d.st,
		keyLen:   d.keyLen,
		maxDepth: d.maxDepth,
		dbStore:  d.dbStore,
		received: maps.Clone(d.received),
	}
}

// Has returns true if the DBSet contains the given item.
func (d *DBSet) Has(k rangesync.KeyBytes) (bool, error) {
	if err := d.EnsureLoaded(); err != nil {
		return false, err
	}

	// checkKey may have false positives, but not false negatives, and it's much
	// faster than querying the database
	if !d.ft.CheckKey(k) {
		return false, nil
	}

	first, err := d.dbStore.From(k, 1).First()
	if err != nil {
		return false, err
	}
	return first.Compare(k) == 0, nil
}

// Recent returns a sequence of items that have been added to the DBSet since the given time.
func (d *DBSet) Recent(since time.Time) (rangesync.SeqResult, int) {
	return d.dbStore.Since(make(rangesync.KeyBytes, d.keyLen), since.UnixNano())
}

// Release releases resources associated with the DBSet.
func (d *DBSet) Release() error {
	d.loadMtx.Lock()
	defer d.loadMtx.Unlock()
	if d.ft != nil {
		d.ft.Release()
		d.ft = nil
	}
	return nil
}
