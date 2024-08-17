package dbsync

import (
	"context"
	"errors"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
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
	if err := d.EnsureLoaded(); err != nil {
		return err
	}
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
	if err := d.EnsureLoaded(); err != nil {
		return hashsync.RangeInfo{}, err
	}
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

func (d *DBItemStore) SplitRange(
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

	if err := d.EnsureLoaded(); err != nil {
		return hashsync.SplitInfo{}, err
	}

	sr, err := d.ft.easySplit(x.(KeyBytes), y.(KeyBytes), count)
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
	part0, err := d.GetRangeInfo(preceding, x, y, count)
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
	part1, err := d.GetRangeInfo(part0.End.Clone(), middle, y, -1)
	if err != nil {
		return hashsync.SplitInfo{}, err
	}
	return hashsync.SplitInfo{
		Parts:  [2]hashsync.RangeInfo{part0, part1},
		Middle: middle,
	}, nil
}

// Min implements hashsync.ItemStore.
func (d *DBItemStore) Min() (hashsync.Iterator, error) {
	if err := d.EnsureLoaded(); err != nil {
		return nil, err
	}
	if d.ft.count() == 0 {
		return nil, nil
	}
	it := d.ft.start()
	if _, err := it.Key(); err != nil {
		return nil, err
	}
	return it, nil
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
	if err := d.EnsureLoaded(); err != nil {
		return false, err
	}
	if d.ft.count() == 0 {
		return false, nil
	}
	// TODO: should often be able to avoid querying the database if we check the key
	// against the fptree
	it := d.ft.iter(k.(KeyBytes))
	itK, err := it.Key()
	if err != nil {
		return false, err
	}
	return itK.Compare(k) == 0, nil
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
func (a *ItemStoreAdapter) GetRangeInfo(preceding hashsync.Iterator, x hashsync.Ordered, y hashsync.Ordered, count int) (hashsync.RangeInfo, error) {
	hx := x.(types.Hash32)
	hy := y.(types.Hash32)
	info, err := a.s.GetRangeInfo(preceding, KeyBytes(hx[:]), KeyBytes(hy[:]), count)
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
		preceding, KeyBytes(hx[:]), KeyBytes(hy[:]), count)
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
func (a *ItemStoreAdapter) Has(k hashsync.Ordered) (bool, error) {
	h := k.(types.Hash32)
	return a.s.Has(KeyBytes(h[:]))
}

// Min implements hashsync.ItemStore.
func (a *ItemStoreAdapter) Min() (hashsync.Iterator, error) {
	it, err := a.s.Min()
	if err != nil {
		return nil, err
	}
	return a.wrapIterator(it), nil
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
