package hashsync

type monoidTreeIterator struct {
	mt   MonoidTree
	node MonoidTreeNode
}

var _ Iterator = monoidTreeIterator{}

func (it monoidTreeIterator) Equal(other Iterator) bool {
	o := other.(monoidTreeIterator)
	if it.mt != o.mt {
		panic("comparing iterators from different MonoidTreeStore")
	}
	return it.node == o.node
}

func (it monoidTreeIterator) Key() Ordered {
	return it.node.Key()
}

func (it monoidTreeIterator) Next() Iterator {
	next := it.node.Next()
	if next == nil {
		next = it.mt.Min()
	}
	if next == nil {
		return nil
	}
	if next.(*monoidTreeNode) == nil {
		panic("QQQQQ: wrapped nil in Next")
	}
	return monoidTreeIterator{
		mt:   it.mt,
		node: next,
	}
}

// TBD: Lookup method
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
func (mts *MonoidTreeStore) Add(v Ordered) {
	mts.mt.Add(v)
}

func (mts *MonoidTreeStore) iter(node MonoidTreeNode) Iterator {
	if node == nil {
		return nil
	}
	if node.(*monoidTreeNode) == nil {
		panic("QQQQQ: wrapped nil")
	}
	return monoidTreeIterator{
		mt:   mts.mt,
		node: node,
	}
}

// GetRangeInfo implements ItemStore.
func (mts *MonoidTreeStore) GetRangeInfo(preceding Iterator, x Ordered, y Ordered, count int) RangeInfo {
	var stop FingerprintPredicate
	var node MonoidTreeNode
	if preceding != nil {
		p := preceding.(monoidTreeIterator)
		if p.mt != mts.mt {
			panic("GetRangeInfo: preceding iterator from a wrong MonoidTreeStore")
		}
		node = p.node
	}
	if count >= 0 {
		stop = func(fp any) bool {
			return CombinedSecond[int](fp) > count
		}
	}
	fp, startNode, endNode := mts.mt.RangeFingerprint(node, x, y, stop)
	// fmt.Fprintf(os.Stderr, "QQQQQ: fp %v, startNode %#v, endNode %#v\n", fp, startNode, endNode)
	cfp := fp.(CombinedFingerprint)
	return RangeInfo{
		Fingerprint: cfp.First,
		Count:       cfp.Second.(int),
		Start:       mts.iter(startNode),
		End:         mts.iter(endNode),
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
