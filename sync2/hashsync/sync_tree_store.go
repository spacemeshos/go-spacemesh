package hashsync

import (
	"context"
	"errors"
	"time"
)

type syncTreeIterator struct {
	st  SyncTree
	ptr SyncTreePointer
}

var _ Iterator = &syncTreeIterator{}

func (it *syncTreeIterator) Equal(other Iterator) bool {
	o := other.(*syncTreeIterator)
	if it.st != o.st {
		panic("comparing iterators from different SyncTreeStore")
	}
	return it.ptr.Equal(o.ptr)
}

func (it *syncTreeIterator) Key() (Ordered, error) {
	return it.ptr.Key(), nil
}

func (it *syncTreeIterator) Next() error {
	it.ptr.Next()
	if it.ptr.Key() == nil {
		it.ptr = it.st.Min()
	}
	return nil
}

func (it *syncTreeIterator) Clone() Iterator {
	return &syncTreeIterator{
		st:  it.st,
		ptr: it.ptr.Clone(),
	}
}

type SyncTreeStore struct {
	st       SyncTree
	identity any
}

var _ ItemStore = &SyncTreeStore{}

func NewSyncTreeStore(m Monoid) ItemStore {
	return &SyncTreeStore{
		st:       NewSyncTree(CombineMonoids(m, CountingMonoid{})),
		identity: m.Identity(),
	}
}

// Add implements ItemStore.
func (sts *SyncTreeStore) Add(ctx context.Context, k Ordered) error {
	sts.st.Set(k, nil)
	return nil
}

func (sts *SyncTreeStore) iter(ptr SyncTreePointer) Iterator {
	if ptr == nil {
		return nil
	}
	return &syncTreeIterator{
		st:  sts.st,
		ptr: ptr,
	}
}

// GetRangeInfo implements ItemStore.
func (sts *SyncTreeStore) GetRangeInfo(
	ctx context.Context,
	preceding Iterator,
	x, y Ordered,
	count int,
) (RangeInfo, error) {
	if x == nil && y == nil {
		it, err := sts.Min(ctx)
		if err != nil {
			return RangeInfo{}, err
		}
		if it == nil {
			return RangeInfo{
				Fingerprint: sts.identity,
			}, nil
		} else {
			x, err = it.Key()
			if err != nil {
				return RangeInfo{}, err
			}
			y = x
		}
	} else if x == nil || y == nil {
		panic("BUG: bad X or Y")
	}
	var stop FingerprintPredicate
	var node SyncTreePointer
	if preceding != nil {
		p := preceding.(*syncTreeIterator)
		if p.st != sts.st {
			panic("GetRangeInfo: preceding iterator from a wrong SyncTreeStore")
		}
		node = p.ptr
	}
	if count >= 0 {
		stop = func(fp any) bool {
			return CombinedSecond[int](fp) > count
		}
	}
	fp, startPtr, endPtr := sts.st.RangeFingerprint(node, x, y, stop)
	cfp := fp.(CombinedFingerprint)
	return RangeInfo{
		Fingerprint: cfp.First,
		Count:       cfp.Second.(int),
		Start:       sts.iter(startPtr),
		End:         sts.iter(endPtr),
	}, nil
}

// SplitRange implements ItemStore.
func (sts *SyncTreeStore) SplitRange(
	ctx context.Context,
	preceding Iterator,
	x, y Ordered,
	count int,
) (SplitInfo, error) {
	if count <= 0 {
		panic("BUG: bad split count")
	}
	part0, err := sts.GetRangeInfo(ctx, preceding, x, y, count)
	if err != nil {
		return SplitInfo{}, err
	}
	if part0.Count == 0 {
		return SplitInfo{}, errors.New("can't split empty range")
	}
	middle, err := part0.End.Key()
	if err != nil {
		return SplitInfo{}, err
	}
	part1, err := sts.GetRangeInfo(ctx, part0.End.Clone(), middle, y, -1)
	if err != nil {
		return SplitInfo{}, err
	}
	return SplitInfo{
		Parts:  [2]RangeInfo{part0, part1},
		Middle: middle,
	}, nil
}

// Min implements ItemStore.
func (sts *SyncTreeStore) Min(ctx context.Context) (Iterator, error) {
	return sts.iter(sts.st.Min()), nil
}

// Copy implements ItemStore.
func (sts *SyncTreeStore) Copy() ItemStore {
	return &SyncTreeStore{
		st:       sts.st.Copy(),
		identity: sts.identity,
	}
}

// Has implements ItemStore.
func (sts *SyncTreeStore) Has(ctx context.Context, k Ordered) (bool, error) {
	_, found := sts.st.Lookup(k)
	return found, nil
}

func (sts *SyncTreeStore) Recent(ctx context.Context, since time.Time) (Iterator, int, error) {
	return nil, 0, nil
}
