package hashsync

type monoidTreeIterator struct {
	mt  MonoidTree
	ptr MonoidTreePointer
}

var _ Iterator = &monoidTreeIterator{}

func (it *monoidTreeIterator) Equal(other Iterator) bool {
	o := other.(*monoidTreeIterator)
	if it.mt != o.mt {
		panic("comparing iterators from different MonoidTreeStore")
	}
	return it.ptr.Equal(o.ptr)
}

func (it *monoidTreeIterator) Key() Ordered {
	return it.ptr.Key()
}

func (it *monoidTreeIterator) Next() {
	it.ptr.Next()
	if it.ptr.Key() == nil {
		it.ptr = it.mt.Min()
	}
}

type MonoidTreeStore struct {
	mt MonoidTree
}

var _ ItemStore = &MonoidTreeStore{}

func NewMonoidTreeStore(m Monoid) ItemStore {
	return &MonoidTreeStore{
		mt: NewMonoidTree(CombineMonoids(m, CountingMonoid{})),
	}
}

// Add implements ItemStore.
func (mts *MonoidTreeStore) Add(k Ordered) {
	mts.mt.Add(k)
}

func (mts *MonoidTreeStore) iter(ptr MonoidTreePointer) Iterator {
	if ptr == nil {
		return nil
	}
	return &monoidTreeIterator{
		mt:  mts.mt,
		ptr: ptr,
	}
}

// GetRangeInfo implements ItemStore.
func (mts *MonoidTreeStore) GetRangeInfo(preceding Iterator, x Ordered, y Ordered, count int) RangeInfo {
	var stop FingerprintPredicate
	var node MonoidTreePointer
	if preceding != nil {
		p := preceding.(*monoidTreeIterator)
		if p.mt != mts.mt {
			panic("GetRangeInfo: preceding iterator from a wrong MonoidTreeStore")
		}
		node = p.ptr
	}
	if count >= 0 {
		stop = func(fp any) bool {
			return CombinedSecond[int](fp) > count
		}
	}
	fp, startPtr, endPtr := mts.mt.RangeFingerprint(node, x, y, stop)
	cfp := fp.(CombinedFingerprint)
	return RangeInfo{
		Fingerprint: cfp.First,
		Count:       cfp.Second.(int),
		Start:       mts.iter(startPtr),
		End:         mts.iter(endPtr),
	}
}

// Min implements ItemStore.
func (mts *MonoidTreeStore) Min() Iterator {
	return mts.iter(mts.mt.Min())
}

// Max implements ItemStore.
func (mts *MonoidTreeStore) Max() Iterator {
	return mts.iter(mts.mt.Max())
}
