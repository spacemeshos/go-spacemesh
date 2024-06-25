package hashsync

import "context"

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

func (it *syncTreeIterator) Key() Ordered {
	return it.ptr.Key()
}

func (it *syncTreeIterator) Next() error {
	it.ptr.Next()
	if it.ptr.Key() == nil {
		it.ptr = it.st.Min()
	}
	return nil
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
func (sts *SyncTreeStore) GetRangeInfo(preceding Iterator, x, y Ordered, count int) (RangeInfo, error) {
	if x == nil && y == nil {
		it, err := sts.Min()
		if err != nil {
			return RangeInfo{}, err
		}
		if it == nil {
			return RangeInfo{
				Fingerprint: sts.identity,
			}, nil
		} else {
			x = it.Key()
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

// Min implements ItemStore.
func (sts *SyncTreeStore) Min() (Iterator, error) {
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
func (sts *SyncTreeStore) Has(k Ordered) (bool, error) {
	_, found := sts.st.Lookup(k)
	return found, nil
}
