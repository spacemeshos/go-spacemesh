package dbsync

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
	"github.com/spacemeshos/go-spacemesh/sync2/types"
)

type DBSet struct {
	loadMtx  sync.Mutex
	db       sql.Database
	ft       *fpTree
	st       *SyncedTable
	snapshot *SyncedTableSnapshot
	dbStore  *dbBackedStore
	keyLen   int
	maxDepth int
}

var _ rangesync.OrderedSet = &DBSet{}

func NewDBSet(
	db sql.Database,
	st *SyncedTable,
	keyLen, maxDepth int,
) *DBSet {
	return &DBSet{
		db:       db,
		st:       st,
		keyLen:   keyLen,
		maxDepth: maxDepth,
	}
}

func (d *DBSet) decodeID(stmt *sql.Statement) bool {
	id := make(types.KeyBytes, d.keyLen) // TODO: don't allocate new ID
	stmt.ColumnBytes(0, id[:])
	d.ft.addStoredHash(id)
	return true
}

func (d *DBSet) EnsureLoaded(ctx context.Context) error {
	d.loadMtx.Lock()
	defer d.loadMtx.Unlock()
	if d.ft != nil {
		return nil
	}
	db := ContextSQLExec(ctx, d.db)
	var err error
	d.snapshot, err = d.st.snapshot(db)
	if err != nil {
		return fmt.Errorf("error taking snapshot: %w", err)
	}
	var np nodePool
	d.dbStore = newDBBackedStore(db, d.snapshot, d.keyLen)
	d.ft = newFPTree(&np, d.dbStore, d.keyLen, d.maxDepth)
	return d.snapshot.loadIDs(db, d.decodeID)
}

// Add implements hashsync.ItemStore.
func (d *DBSet) Add(ctx context.Context, k types.Ordered) error {
	if err := d.EnsureLoaded(ctx); err != nil {
		return err
	}
	has, err := d.Has(ctx, k) // TODO: this check shouldn't be needed
	if has || err != nil {
		return err
	}
	return d.ft.addHash(k.(types.KeyBytes))
}

// GetRangeInfo implements hashsync.ItemStore.
func (d *DBSet) GetRangeInfo(
	ctx context.Context,
	x, y types.Ordered,
	count int,
) (rangesync.RangeInfo, error) {
	if err := d.EnsureLoaded(ctx); err != nil {
		return rangesync.RangeInfo{}, err
	}
	fpr, err := d.ft.fingerprintInterval(ctx, x.(types.KeyBytes), y.(types.KeyBytes), count)
	if err != nil {
		return rangesync.RangeInfo{}, err
	}
	return rangesync.RangeInfo{
		Fingerprint: fpr.fp,
		Count:       int(fpr.count),
		Items:       fpr.items,
	}, nil
}

func (d *DBSet) SplitRange(
	ctx context.Context,
	x, y types.Ordered,
	count int,
) (
	rangesync.SplitInfo,
	error,
) {
	if count <= 0 {
		panic("BUG: bad split count")
	}

	if err := d.EnsureLoaded(ctx); err != nil {
		return rangesync.SplitInfo{}, err
	}

	sr, err := d.ft.easySplit(ctx, x.(types.KeyBytes), y.(types.KeyBytes), count)
	if err == nil {
		// fmt.Fprintf(os.Stderr, "QQQQQ: fast split, middle: %s\n", sr.middle.String())
		return rangesync.SplitInfo{
			Parts: [2]rangesync.RangeInfo{
				{
					Fingerprint: sr.part0.fp,
					Count:       int(sr.part0.count),
					Items:       sr.part0.items,
				},
				{
					Fingerprint: sr.part1.fp,
					Count:       int(sr.part1.count),
					Items:       sr.part1.items,
				},
			},
			Middle: sr.middle,
		}, nil
	}

	if !errors.Is(err, errEasySplitFailed) {
		return rangesync.SplitInfo{}, err
	}

	fpr0, err := d.ft.fingerprintInterval(ctx, x.(types.KeyBytes), y.(types.KeyBytes), count)
	if err != nil {
		return rangesync.SplitInfo{}, err
	}

	if fpr0.count == 0 {
		return rangesync.SplitInfo{}, errors.New("can't split empty range")
	}

	fpr1, err := d.ft.fingerprintInterval(ctx, fpr0.next, y.(types.KeyBytes), -1)
	if err != nil {
		return rangesync.SplitInfo{}, err
	}

	return rangesync.SplitInfo{
		Parts: [2]rangesync.RangeInfo{
			{
				Fingerprint: fpr0.fp,
				Count:       int(fpr0.count),
				Items:       fpr0.items,
			},
			{
				Fingerprint: fpr1.fp,
				Count:       int(fpr1.count),
				Items:       fpr1.items,
			},
		},
		Middle: fpr0.next,
	}, nil
}

// Min implements hashsync.ItemStore.
func (d *DBSet) Items(ctx context.Context) (types.Seq, error) {
	if err := d.EnsureLoaded(ctx); err != nil {
		return nil, err
	}
	if d.ft.count() == 0 {
		return types.EmptySeq(), nil
	}
	return d.ft.all(ctx)
}

// Empty implements hashsync.ItemStore.
func (d *DBSet) Empty(ctx context.Context) (bool, error) {
	if err := d.EnsureLoaded(ctx); err != nil {
		return false, err
	}
	return d.ft.count() == 0, nil
}

func (d *DBSet) Advance(ctx context.Context) error {
	d.loadMtx.Lock()
	d.loadMtx.Unlock()
	if d.ft == nil {
		// FIXME
		panic("BUG: can't advance the DBItemStore before it's loaded")
	}
	oldSnapshot := d.snapshot
	var err error
	d.snapshot, err = d.st.snapshot(ContextSQLExec(ctx, d.db))
	if err != nil {
		return fmt.Errorf("error taking snapshot: %w", err)
	}
	d.dbStore.setSnapshot(d.snapshot)
	return d.snapshot.loadIDsSince(d.db, oldSnapshot, d.decodeID)
}

// Copy implements hashsync.ItemStore.
func (d *DBSet) Copy() rangesync.OrderedSet {
	d.loadMtx.Lock()
	d.loadMtx.Unlock()
	if d.ft == nil {
		// FIXME
		panic("BUG: can't copy the DBItemStore before it's loaded")
	}
	return &DBSet{
		db:       d.db,
		ft:       d.ft.clone(),
		st:       d.st,
		keyLen:   d.keyLen,
		maxDepth: d.maxDepth,
	}
}

// Has implements hashsync.ItemStore.
func (d *DBSet) Has(ctx context.Context, k types.Ordered) (bool, error) {
	if err := d.EnsureLoaded(ctx); err != nil {
		return false, err
	}
	if d.ft.count() == 0 {
		return false, nil
	}
	// TODO: should often be able to avoid querying the database if we check the key
	// against the fptree
	seq, err := d.ft.from(ctx, k.(types.KeyBytes))
	if err != nil {
		return false, err
	}
	for curK, err := range seq {
		if err != nil {
			return false, err
		}
		return curK.Compare(k) == 0, nil
	}
	panic("BUG: empty sequence with a non-zero count")
}

// Recent implements hashsync.ItemStore.
func (d *DBSet) Recent(ctx context.Context, since time.Time) (types.Seq, int, error) {
	return d.dbStore.since(ctx, make(types.KeyBytes, d.keyLen), since.UnixNano())
}
