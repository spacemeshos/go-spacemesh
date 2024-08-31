package dbsync

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sync2/hashsync"
)

type DBItemStore struct {
	loadMtx  sync.Mutex
	db       sql.Database
	ft       *fpTree
	st       *SyncedTable
	snapshot *SyncedTableSnapshot
	dbStore  *dbBackedStore
	keyLen   int
	maxDepth int
}

var _ hashsync.ItemStore = &DBItemStore{}

func NewDBItemStore(
	db sql.Database,
	st *SyncedTable,
	keyLen, maxDepth int,
) *DBItemStore {
	// var np nodePool
	// dbStore := newDBBackedStore(db, iterQuery, keyLen)
	return &DBItemStore{
		db: db,
		// ft:       newFPTree(&np, dbStore, keyLen, maxDepth),
		st:       st,
		keyLen:   keyLen,
		maxDepth: maxDepth,
	}
}

func (d *DBItemStore) decodeID(stmt *sql.Statement) bool {
	id := make(KeyBytes, d.keyLen) // TODO: don't allocate new ID
	stmt.ColumnBytes(0, id[:])
	d.ft.addStoredHash(id)
	return true
}

func (d *DBItemStore) EnsureLoaded(ctx context.Context) error {
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
func (d *DBItemStore) Add(ctx context.Context, k hashsync.Ordered) error {
	if err := d.EnsureLoaded(ctx); err != nil {
		return err
	}
	has, err := d.Has(ctx, k) // TODO: this check shouldn't be needed
	if has || err != nil {
		return err
	}
	return d.ft.addHash(k.(KeyBytes))
}

// GetRangeInfo implements hashsync.ItemStore.
func (d *DBItemStore) GetRangeInfo(
	ctx context.Context,
	preceding hashsync.Iterator,
	x, y hashsync.Ordered,
	count int,
) (hashsync.RangeInfo, error) {
	if err := d.EnsureLoaded(ctx); err != nil {
		return hashsync.RangeInfo{}, err
	}
	fpr, err := d.ft.fingerprintInterval(ctx, x.(KeyBytes), y.(KeyBytes), count)
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

func (d *DBItemStore) SplitRange(
	ctx context.Context,
	preceding hashsync.Iterator,
	x, y hashsync.Ordered,
	count int,
) (
	hashsync.SplitInfo,
	error,
) {
	if count <= 0 {
		panic("BUG: bad split count")
	}

	if err := d.EnsureLoaded(ctx); err != nil {
		return hashsync.SplitInfo{}, err
	}

	sr, err := d.ft.easySplit(ctx, x.(KeyBytes), y.(KeyBytes), count)
	if err == nil {
		// fmt.Fprintf(os.Stderr, "QQQQQ: fast split, middle: %s\n", sr.middle.String())
		return hashsync.SplitInfo{
			Parts: [2]hashsync.RangeInfo{
				{
					Fingerprint: sr.part0.fp,
					Count:       int(sr.part0.count),
					Start:       sr.part0.start,
					End:         sr.part0.end,
				},
				{
					Fingerprint: sr.part1.fp,
					Count:       int(sr.part1.count),
					Start:       sr.part1.start,
					End:         sr.part1.end,
				},
			},
			Middle: sr.middle,
		}, nil
	}

	if !errors.Is(err, errEasySplitFailed) {
		return hashsync.SplitInfo{}, err
	}

	// fmt.Fprintf(os.Stderr, "QQQQQ: slow split x %s y %s\n", x.(fmt.Stringer), y.(fmt.Stringer))
	part0, err := d.GetRangeInfo(ctx, preceding, x, y, count)
	if err != nil {
		return hashsync.SplitInfo{}, err
	}
	if part0.Count == 0 {
		return hashsync.SplitInfo{}, errors.New("can't split empty range")
	}
	middle, err := part0.End.Key()
	if err != nil {
		return hashsync.SplitInfo{}, err
	}
	part1, err := d.GetRangeInfo(ctx, part0.End.Clone(), middle, y, -1)
	if err != nil {
		return hashsync.SplitInfo{}, err
	}
	return hashsync.SplitInfo{
		Parts:  [2]hashsync.RangeInfo{part0, part1},
		Middle: middle,
	}, nil
}

// Min implements hashsync.ItemStore.
func (d *DBItemStore) Min(ctx context.Context) (hashsync.Iterator, error) {
	if err := d.EnsureLoaded(ctx); err != nil {
		return nil, err
	}
	if d.ft.count() == 0 {
		return nil, nil
	}
	it := d.ft.start(ctx)
	if _, err := it.Key(); err != nil {
		return nil, err
	}
	return it, nil
}

func (d *DBItemStore) Advance(ctx context.Context) error {
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
func (d *DBItemStore) Copy() hashsync.ItemStore {
	d.loadMtx.Lock()
	d.loadMtx.Unlock()
	if d.ft == nil {
		// FIXME
		panic("BUG: can't copy the DBItemStore before it's loaded")
	}
	return &DBItemStore{
		db:       d.db,
		ft:       d.ft.clone(),
		st:       d.st,
		keyLen:   d.keyLen,
		maxDepth: d.maxDepth,
	}
}

// Has implements hashsync.ItemStore.
func (d *DBItemStore) Has(ctx context.Context, k hashsync.Ordered) (bool, error) {
	if err := d.EnsureLoaded(ctx); err != nil {
		return false, err
	}
	if d.ft.count() == 0 {
		return false, nil
	}
	// TODO: should often be able to avoid querying the database if we check the key
	// against the fptree
	it := d.ft.iter(ctx, k.(KeyBytes))
	itK, err := it.Key()
	if err != nil {
		return false, err
	}
	return itK.Compare(k) == 0, nil
}

// Recent implements hashsync.ItemStore.
func (d *DBItemStore) Recent(ctx context.Context, since time.Time) (hashsync.Iterator, int, error) {
	return d.dbStore.iterSince(ctx, make(KeyBytes, d.keyLen), since.UnixNano())
}

// TODO: get rid of ItemStoreAdapter, it shouldn't be needed
type ItemStoreAdapter struct {
	s *DBItemStore
}

var _ hashsync.ItemStore = &ItemStoreAdapter{}

func NewItemStoreAdapter(s *DBItemStore) *ItemStoreAdapter {
	return &ItemStoreAdapter{s: s}
}

func (a *ItemStoreAdapter) wrapIterator(it hashsync.Iterator) hashsync.Iterator {
	if it == nil {
		return nil
	}
	return &iteratorAdapter{it: it}
}

// Add implements hashsync.ItemStore.
func (a *ItemStoreAdapter) Add(ctx context.Context, k hashsync.Ordered) error {
	h := k.(types.Hash32)
	return a.s.Add(ctx, KeyBytes(h[:]))
}

// Copy implements hashsync.ItemStore.
func (a *ItemStoreAdapter) Copy() hashsync.ItemStore {
	return &ItemStoreAdapter{s: a.s.Copy().(*DBItemStore)}
}

// GetRangeInfo implements hashsync.ItemStore.
func (a *ItemStoreAdapter) GetRangeInfo(
	ctx context.Context,
	preceding hashsync.Iterator,
	x hashsync.Ordered,
	y hashsync.Ordered,
	count int,
) (hashsync.RangeInfo, error) {
	hx := x.(types.Hash32)
	hy := y.(types.Hash32)
	info, err := a.s.GetRangeInfo(ctx, preceding, KeyBytes(hx[:]), KeyBytes(hy[:]), count)
	if err != nil {
		return hashsync.RangeInfo{}, err
	}
	var fp types.Hash12
	src := info.Fingerprint.(fingerprint)
	copy(fp[:], src[:])
	return hashsync.RangeInfo{
		Fingerprint: fp,
		Count:       info.Count,
		Start:       a.wrapIterator(info.Start),
		End:         a.wrapIterator(info.End),
	}, nil
}

func (a *ItemStoreAdapter) SplitRange(
	ctx context.Context,
	preceding hashsync.Iterator,
	x hashsync.Ordered,
	y hashsync.Ordered,
	count int,
) (
	hashsync.SplitInfo,
	error,
) {
	hx := x.(types.Hash32)
	hy := y.(types.Hash32)
	si, err := a.s.SplitRange(
		ctx, preceding, KeyBytes(hx[:]), KeyBytes(hy[:]), count)
	if err != nil {
		return hashsync.SplitInfo{}, err
	}
	var fp1, fp2 types.Hash12
	src1 := si.Parts[0].Fingerprint.(fingerprint)
	src2 := si.Parts[1].Fingerprint.(fingerprint)
	copy(fp1[:], src1[:])
	copy(fp2[:], src2[:])
	var middle types.Hash32
	copy(middle[:], si.Middle.(KeyBytes))
	return hashsync.SplitInfo{
		Parts: [2]hashsync.RangeInfo{
			{
				Fingerprint: fp1,
				Count:       si.Parts[0].Count,
				Start:       a.wrapIterator(si.Parts[0].Start),
				End:         a.wrapIterator(si.Parts[0].End),
			},
			{
				Fingerprint: fp2,
				Count:       si.Parts[1].Count,
				Start:       a.wrapIterator(si.Parts[1].Start),
				End:         a.wrapIterator(si.Parts[1].End),
			},
		},
		Middle: middle,
	}, nil
}

// Has implements hashsync.ItemStore.
func (a *ItemStoreAdapter) Has(ctx context.Context, k hashsync.Ordered) (bool, error) {
	h := k.(types.Hash32)
	return a.s.Has(ctx, KeyBytes(h[:]))
}

// Min implements hashsync.ItemStore.
func (a *ItemStoreAdapter) Min(ctx context.Context) (hashsync.Iterator, error) {
	it, err := a.s.Min(ctx)
	if err != nil {
		return nil, err
	}
	return a.wrapIterator(it), nil
}

// Recent implements hashsync.ItemStore.
func (d *ItemStoreAdapter) Recent(ctx context.Context, since time.Time) (hashsync.Iterator, int, error) {
	it, count, err := d.s.Recent(ctx, since)
	if err != nil {
		return nil, 0, err
	}
	return d.wrapIterator(it), count, nil
}

type iteratorAdapter struct {
	it hashsync.Iterator
}

var _ hashsync.Iterator = &iteratorAdapter{}

func (ia *iteratorAdapter) Key() (hashsync.Ordered, error) {
	k, err := ia.it.Key()
	if err != nil {
		return nil, err
	}
	var h types.Hash32
	copy(h[:], k.(KeyBytes))
	return h, nil
}

func (ia *iteratorAdapter) Next() error {
	return ia.it.Next()
}

func (ia *iteratorAdapter) Clone() hashsync.Iterator {
	return &iteratorAdapter{it: ia.it.Clone()}
}
