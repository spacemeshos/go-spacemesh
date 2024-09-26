package rangesync

import (
	"context"
	"crypto/md5"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/zeebo/blake3"

	"github.com/spacemeshos/go-spacemesh/sync2/types"
)

func stringToFP(s string) types.Fingerprint {
	h := md5.New()
	h.Write([]byte(s))
	return types.Fingerprint(h.Sum(nil))
}

func gtePos(all []types.KeyBytes, item types.KeyBytes) int {
	n := slices.IndexFunc(all, func(v types.KeyBytes) bool {
		return v.Compare(item) >= 0
	})
	if n >= 0 {
		return n
	}
	return len(all)
}

func naiveRange(
	all []types.KeyBytes,
	x, y types.KeyBytes,
	stopCount int,
) (
	items []types.KeyBytes,
	startID, endID types.KeyBytes,
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

var naiveFPFunc = func(items []types.KeyBytes) types.Fingerprint {
	s := ""
	for _, k := range items {
		s += string(k)
	}
	return stringToFP(s)
}

type dumbSet struct {
	keys         []types.KeyBytes
	disableReAdd bool
	added        map[string]bool
	fpFunc       func(items []types.KeyBytes) types.Fingerprint
}

var _ OrderedSet = &dumbSet{}

func (ds *dumbSet) Add(ctx context.Context, id types.KeyBytes) error {
	if len(ds.keys) == 0 {
		ds.keys = []types.KeyBytes{id}
		return nil
	}
	p := slices.IndexFunc(ds.keys, func(other types.KeyBytes) bool {
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

func (ds *dumbSet) seq(n int) types.Seq {
	if n < 0 || n > len(ds.keys) {
		panic("bad index")
	}
	return types.Seq(func(yield func(types.KeyBytes, error) bool) {
		n := n // make the sequence reusable
		for {
			if !yield(ds.keys[n], nil) {
				break
			}
			n = (n + 1) % len(ds.keys)
		}
	})
}

func (ds *dumbSet) seqFor(s types.KeyBytes) types.Seq {
	n := slices.IndexFunc(ds.keys, func(k types.KeyBytes) bool {
		return k.Compare(s) == 0
	})
	if n == -1 {
		panic("item not found: " + s.String())
	}
	return ds.seq(n)
}

func (ds *dumbSet) getRangeInfo(
	_ context.Context,
	x, y types.KeyBytes,
	count int,
) (r RangeInfo, end types.KeyBytes, err error) {
	if x == nil && y == nil {
		if len(ds.keys) == 0 {
			return RangeInfo{
				Fingerprint: types.EmptyFingerprint(),
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
	}
	return r, end, nil
}

func (ds *dumbSet) GetRangeInfo(
	ctx context.Context,
	x, y types.KeyBytes,
	count int,
) (RangeInfo, error) {
	ri, _, err := ds.getRangeInfo(ctx, x, y, count)
	return ri, err
}

func (ds *dumbSet) SplitRange(
	ctx context.Context,
	x, y types.KeyBytes,
	count int,
) (SplitInfo, error) {
	if count <= 0 {
		panic("BUG: bad split count")
	}
	part0, middle, err := ds.getRangeInfo(ctx, x, y, count)
	if err != nil {
		return SplitInfo{}, err
	}
	if part0.Count == 0 {
		return SplitInfo{}, errors.New("can't split empty range")
	}
	part1, err := ds.GetRangeInfo(ctx, middle, y, -1)
	if err != nil {
		return SplitInfo{}, err
	}
	return SplitInfo{
		Parts:  [2]RangeInfo{part0, part1},
		Middle: middle,
	}, nil
}

func (ds *dumbSet) Empty(ctx context.Context) (bool, error) {
	return len(ds.keys) == 0, nil
}

func (ds *dumbSet) Items(ctx context.Context) (types.Seq, error) {
	if len(ds.keys) == 0 {
		return types.EmptySeq(), nil
	}
	return ds.seq(0), nil
}

func (ds *dumbSet) Copy() OrderedSet {
	return &dumbSet{keys: slices.Clone(ds.keys)}
}

func (ds *dumbSet) Recent(ctx context.Context, since time.Time) (types.Seq, int, error) {
	return nil, 0, nil
}

var hashPool = &sync.Pool{
	New: func() any {
		return blake3.New()
	},
}

func NewDumbHashSet(disableReAdd bool) OrderedSet {
	return &dumbSet{
		disableReAdd: disableReAdd,
		fpFunc: func(items []types.KeyBytes) (r types.Fingerprint) {
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
