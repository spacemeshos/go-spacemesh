package hashsync

type ValueHandler interface {
	Load(k Ordered, treeValue any) (v any)
	Store(k Ordered, v any) (treeValue any)
}

type defaultValueHandler struct{}

func (vh defaultValueHandler) Load(k Ordered, treeValue any) (v any) {
	return treeValue
}

func (vh defaultValueHandler) Store(k Ordered, v any) (treeValue any) {
	return v
}

type syncTreeIterator struct {
	st  SyncTree
	ptr SyncTreePointer
	vh  ValueHandler
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

func (it *syncTreeIterator) Value() any {
	return it.vh.Load(it.ptr.Key(), it.ptr.Value())
}

func (it *syncTreeIterator) Next() {
	it.ptr.Next()
	if it.ptr.Key() == nil {
		it.ptr = it.st.Min()
	}
}

type SyncTreeStore struct {
	st       SyncTree
	vh       ValueHandler
	newValue NewValueFunc
}

var _ ItemStore = &SyncTreeStore{}

func NewSyncTreeStore(m Monoid, vh ValueHandler, newValue NewValueFunc) ItemStore {
	if vh == nil {
		vh = defaultValueHandler{}
	}
	return &SyncTreeStore{
		st:       NewSyncTree(CombineMonoids(m, CountingMonoid{})),
		vh:       vh,
		newValue: newValue,
	}
}

// Add implements ItemStore.
func (sts *SyncTreeStore) Add(k Ordered, v any) {
	treeValue := sts.vh.Store(k, v)
	sts.st.Set(k, treeValue)
}

func (sts *SyncTreeStore) iter(ptr SyncTreePointer) Iterator {
	if ptr == nil {
		return nil
	}
	return &syncTreeIterator{
		st:  sts.st,
		ptr: ptr,
		vh:  sts.vh,
	}
}

// GetRangeInfo implements ItemStore.
func (sts *SyncTreeStore) GetRangeInfo(preceding Iterator, x Ordered, y Ordered, count int) RangeInfo {
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
	}
}

// Min implements ItemStore.
func (sts *SyncTreeStore) Min() Iterator {
	return sts.iter(sts.st.Min())
}

// Max implements ItemStore.
func (sts *SyncTreeStore) Max() Iterator {
	return sts.iter(sts.st.Max())
}

// New implements ItemStore.
func (sts *SyncTreeStore) New() any {
	return sts.newValue()
}
