package rangesync

import (
	"crypto/md5"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/zeebo/blake3"
)

// stringToFP conversts a string to a Fingerprint.
// It is only used in tests.
func stringToFP(s string) Fingerprint {
	h := md5.New()
	h.Write([]byte(s))
	return Fingerprint(h.Sum(nil))
}

// gtePos returns the position of the first item in the given list that is greater than or
// equal to the given item. If no such item is found, it returns the length of the list.
func gtePos(all []KeyBytes, item KeyBytes) int {
	n := slices.IndexFunc(all, func(v KeyBytes) bool {
		return v.Compare(item) >= 0
	})
	if n >= 0 {
		return n
	}
	return len(all)
}

// naiveRange returns the items in the range [x, y) from the given list of items,
// supporting wraparound (x > y) and full (x == y) ranges.
// It also returns the first and last item falling within the range.
func naiveRange(
	all []KeyBytes,
	x, y KeyBytes,
	stopCount int,
) (
	items []KeyBytes,
	startID, endID KeyBytes,
) {
	if len(all) == 0 {
		return nil, nil, nil
	}
	start := gtePos(all, x)
	end := gtePos(all, y)
	if x.Compare(y) < 0 {
		if stopCount >= 0 && end-start > stopCount {
			end = start + stopCount
		}
		if end < len(all) {
			endID = all[end]
		} else {
			endID = all[0]
		}
		if start < len(all) {
			startID = all[start]
		} else {
			startID = all[0]
		}
		return all[start:end], startID, endID
	} else {
		r := append(all[start:], all[:end]...)
		if len(r) == 0 {
			return nil, all[0], all[0]
		}
		if stopCount >= 0 && len(r) > stopCount {
			return r[:stopCount], r[0], r[stopCount]
		}
		if end < len(all) {
			endID = all[end]
		} else {
			endID = all[0]
		}
		startID = nil
		if len(r) != 0 {
			startID = r[0]
		}
		return r, startID, endID
	}
}

var naiveFPFunc = func(items []KeyBytes) Fingerprint {
	s := ""
	for _, k := range items {
		s += string(k)
	}
	return stringToFP(s)
}

// dumbSet is a simple OrderedSet implementation that doesn't include any optimizations.
// It is intended to be only used in tests.
type dumbSet struct {
	keys         []KeyBytes
	DisableReAdd bool
	added        map[string]bool
	fpFunc       func(items []KeyBytes) Fingerprint
}

var _ OrderedSet = &dumbSet{}

// Receive implements the OrderedSet.
func (ds *dumbSet) Receive(id KeyBytes) error {
	if len(ds.keys) == 0 {
		ds.keys = []KeyBytes{id}
		return nil
	}
	p := slices.IndexFunc(ds.keys, func(other KeyBytes) bool {
		return other.Compare(id) >= 0
	})
	switch {
	case p < 0:
		ds.keys = append(ds.keys, id)
	case id.Compare(ds.keys[p]) == 0:
		if ds.DisableReAdd {
			if ds.added[string(id)] {
				panic("hash sent twice: " + id.String())
			}
			if ds.added == nil {
				ds.added = make(map[string]bool)
			}
			ds.added[string(id)] = true
		}
		// already present
	default:
		ds.keys = slices.Insert(ds.keys, p, id)
	}

	return nil
}

// seq returns an endless sequence as a SeqResult starting from the given index.
func (ds *dumbSet) seq(n int) SeqResult {
	if n < 0 || n > len(ds.keys) {
		panic("bad index")
	}
	return SeqResult{
		Seq: Seq(func(yield func(KeyBytes) bool) {
			n := n // make the sequence reusable
			for {
				if !yield(ds.keys[n]) {
					break
				}
				n = (n + 1) % len(ds.keys)
			}
		}),
		Error: NoSeqError,
	}
}

// seqFor returns an endless sequence as a SeqResult starting from the given key, or the
// lowest key greater than the given key if the key is not present in the set.
func (ds *dumbSet) seqFor(s KeyBytes) SeqResult {
	n := slices.IndexFunc(ds.keys, func(k KeyBytes) bool {
		return k.Compare(s) == 0
	})
	if n == -1 {
		panic("item not found: " + s.String())
	}
	return ds.seq(n)
}

func (ds *dumbSet) getRangeInfo(
	x, y KeyBytes,
	count int,
) (r RangeInfo, end KeyBytes, err error) {
	if x == nil && y == nil {
		if len(ds.keys) == 0 {
			return RangeInfo{
				Fingerprint: EmptyFingerprint(),
			}, nil, nil
		}
		x = ds.keys[0]
		y = x
	} else if x == nil || y == nil {
		panic("BUG: bad X or Y")
	}
	rangeItems, start, end := naiveRange(ds.keys, x, y, count)
	fpFunc := ds.fpFunc
	if fpFunc == nil {
		fpFunc = naiveFPFunc
	}
	r = RangeInfo{
		Fingerprint: fpFunc(rangeItems),
		Count:       len(rangeItems),
	}
	if r.Count != 0 {
		if start == nil || end == nil {
			panic("empty start/end from naiveRange")
		}
		r.Items = ds.seqFor(start)
	} else {
		r.Items = EmptySeqResult()
	}
	return r, end, nil
}

// GetRangeInfo implements OrderedSet.
func (ds *dumbSet) GetRangeInfo(x, y KeyBytes, count int) (RangeInfo, error) {
	ri, _, err := ds.getRangeInfo(x, y, count)
	return ri, err
}

// SplitRange implements OrderedSet.
func (ds *dumbSet) SplitRange(x, y KeyBytes, count int) (SplitInfo, error) {
	if count <= 0 {
		panic("BUG: bad split count")
	}
	part0, middle, err := ds.getRangeInfo(x, y, count)
	if err != nil {
		return SplitInfo{}, err
	}
	if part0.Count == 0 {
		return SplitInfo{}, errors.New("can't split empty range")
	}
	part1, err := ds.GetRangeInfo(middle, y, -1)
	if err != nil {
		return SplitInfo{}, err
	}
	return SplitInfo{
		Parts:  [2]RangeInfo{part0, part1},
		Middle: middle,
	}, nil
}

// Empty implements OrderedSet.
func (ds *dumbSet) Empty() (bool, error) {
	return len(ds.keys) == 0, nil
}

// Items implements OrderedSet.
func (ds *dumbSet) Items() SeqResult {
	if len(ds.keys) == 0 {
		return EmptySeqResult()
	}
	return ds.seq(0)
}

// Copy implements OrderedSet.
func (ds *dumbSet) Copy(syncScope bool) OrderedSet {
	return &dumbSet{keys: slices.Clone(ds.keys)}
}

// Recent implements OrderedSet.
func (ds *dumbSet) Recent(since time.Time) (SeqResult, int) {
	return EmptySeqResult(), 0
}

var hashPool = &sync.Pool{
	New: func() any {
		return blake3.New()
	},
}

// NewDumbSet creates a new dumbSet instance.
// If disableReAdd is true, the set will panic if the same item is received twice.
func NewDumbSet(disableReAdd bool) OrderedSet {
	return &dumbSet{
		DisableReAdd: disableReAdd,
		fpFunc: func(items []KeyBytes) (r Fingerprint) {
			hasher := hashPool.Get().(*blake3.Hasher)
			defer func() {
				hasher.Reset()
				hashPool.Put(hasher)
			}()
			var hashRes [32]byte
			for _, h := range items {
				hasher.Write(h[:])
			}
			hasher.Sum(hashRes[:0])
			copy(r[:], hashRes[:])
			return r
		},
	}
}
