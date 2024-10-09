package rangesync

import (
	"crypto/md5"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/zeebo/blake3"
)

func stringToFP(s string) Fingerprint {
	h := md5.New()
	h.Write([]byte(s))
	return Fingerprint(h.Sum(nil))
}

func gtePos(all []KeyBytes, item KeyBytes) int {
	n := slices.IndexFunc(all, func(v KeyBytes) bool {
		return v.Compare(item) >= 0
	})
	if n >= 0 {
		return n
	}
	return len(all)
}

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

type dumbSet struct {
	keys         []KeyBytes
	disableReAdd bool
	added        map[string]bool
	fpFunc       func(items []KeyBytes) Fingerprint
}

var _ OrderedSet = &dumbSet{}

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
		if ds.disableReAdd {
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

func (ds *dumbSet) GetRangeInfo(x, y KeyBytes, count int) (RangeInfo, error) {
	ri, _, err := ds.getRangeInfo(x, y, count)
	return ri, err
}

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

func (ds *dumbSet) Empty() (bool, error) {
	return len(ds.keys) == 0, nil
}

func (ds *dumbSet) Items() SeqResult {
	if len(ds.keys) == 0 {
		return EmptySeqResult()
	}
	return ds.seq(0)
}

func (ds *dumbSet) Copy(syncScope bool) OrderedSet {
	return &dumbSet{keys: slices.Clone(ds.keys)}
}

func (ds *dumbSet) Recent(since time.Time) (SeqResult, int) {
	return EmptySeqResult(), 0
}

var hashPool = &sync.Pool{
	New: func() any {
		return blake3.New()
	},
}

func NewDumbHashSet(disableReAdd bool) OrderedSet {
	return &dumbSet{
		disableReAdd: disableReAdd,
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

type deferredAddSet struct {
	OrderedSet
	added map[string]struct{}
}

func (das *deferredAddSet) Receive(id KeyBytes) error {
	if das.added == nil {
		das.added = make(map[string]struct{})
	}
	das.added[string(id)] = struct{}{}
	return nil
}

func (das *deferredAddSet) addAll() error {
	for k := range das.added {
		if err := das.OrderedSet.Receive(KeyBytes(k)); err != nil {
			return err
		}
	}
	return nil
}
