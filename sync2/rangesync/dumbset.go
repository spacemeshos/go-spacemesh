package rangesync

import (
	"crypto/md5"
	"errors"
	"slices"
	"time"

	"github.com/spacemeshos/go-spacemesh/hash"
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

func naiveFPFunc(items []KeyBytes) Fingerprint {
	s := ""
	for _, k := range items {
		s += string(k)
	}
	return stringToFP(s)
}

func realFPFunc(items []KeyBytes) Fingerprint {
	hasher := hash.GetHasher()
	defer func() {
		hasher.Reset()
		hash.PutHasher(hasher)
	}()
	for _, h := range items {
		hasher.Write(h[:])
	}
	var hashRes [32]byte
	hasher.Sum(hashRes[:0])
	var r Fingerprint
	copy(r[:], hashRes[:])
	return r
}

// DumbSet is a simple OrderedSet implementation that doesn't include any optimizations.
// It is intended to be only used in tests.
type DumbSet struct {
	keys             []KeyBytes
	received         map[string]int
	added            map[string]bool
	allowMutiReceive bool
	FPFunc           func(items []KeyBytes) Fingerprint
}

var _ OrderedSet = &DumbSet{}

// SetAllowMultiReceive sets whether the set allows receiving the same item multiple times.
func (ds *DumbSet) SetAllowMultiReceive(allow bool) {
	ds.allowMutiReceive = allow
}

// AddUnchecked adds an item to the set without registerting the item for checks
// as in case of Add and Receive.
func (ds *DumbSet) AddUnchecked(id KeyBytes) {
	if len(ds.keys) == 0 {
		ds.keys = []KeyBytes{id}
	}
	p := slices.IndexFunc(ds.keys, func(other KeyBytes) bool {
		return other.Compare(id) >= 0
	})
	switch {
	case p < 0:
		ds.keys = append(ds.keys, id)
	case id.Compare(ds.keys[p]) == 0:
		// already present
	default:
		ds.keys = slices.Insert(ds.keys, p, id)
	}
}

// AddReceived adds all the received items to the set.
func (ds *DumbSet) AddReceived() {
	sr := ds.Received()
	for k := range sr.Seq {
		ds.AddUnchecked(KeyBytes(k))
	}
	// DumbSet's Received implementation should never return an error
	if sr.Error() != nil {
		panic("unexpected error in Received")
	}
}

// Add implements the OrderedSet.
func (ds *DumbSet) Add(id KeyBytes) error {
	if ds.added == nil {
		ds.added = make(map[string]bool)
	}
	sid := string(id)
	if ds.added[sid] {
		panic("item already added via Add: " + id.String())
	}
	ds.added[sid] = true
	// Add is invoked during recent sync.
	// If the item was already received, this means it was received as part of the
	// recent sync, and thus may be received again as the algorithm does not guarantee
	// that items already in the set are not received from the remote peer, only that
	// no item is received twice (with exception of recent sync).
	if ds.received[sid] != 0 {
		ds.received[sid] = 0
	}
	ds.AddUnchecked(id)
	return nil
}

// Receive implements the OrderedSet.
func (ds *DumbSet) Receive(id KeyBytes) error {
	if ds.received == nil {
		ds.received = make(map[string]int)
	}
	sid := string(id)
	ds.received[sid]++
	if !ds.allowMutiReceive && ds.received[sid] > 1 {
		panic("item already received: " + id.String())
	}
	return nil
}

// Received implements the OrderedSet.
func (ds *DumbSet) Received() SeqResult {
	return SeqResult{
		Seq: func(yield func(KeyBytes) bool) {
			for k := range ds.received {
				if !yield(KeyBytes(k)) {
					break
				}
			}
		},
		Error: NoSeqError,
	}
}

// seq returns an endless sequence as a SeqResult starting from the given index.
func (ds *DumbSet) seq(n int) SeqResult {
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
func (ds *DumbSet) seqFor(s KeyBytes) SeqResult {
	n := slices.IndexFunc(ds.keys, func(k KeyBytes) bool {
		return k.Compare(s) == 0
	})
	if n == -1 {
		panic("item not found: " + s.String())
	}
	return ds.seq(n)
}

func (ds *DumbSet) getRangeInfo(
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
	fpFunc := ds.FPFunc
	if fpFunc == nil {
		fpFunc = realFPFunc
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
func (ds *DumbSet) GetRangeInfo(x, y KeyBytes) (RangeInfo, error) {
	ri, _, err := ds.getRangeInfo(x, y, -1)
	return ri, err
}

// SplitRange implements OrderedSet.
func (ds *DumbSet) SplitRange(x, y KeyBytes, count int) (SplitInfo, error) {
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
	part1, _, err := ds.getRangeInfo(middle, y, -1)
	if err != nil {
		return SplitInfo{}, err
	}
	return SplitInfo{
		Parts:  [2]RangeInfo{part0, part1},
		Middle: middle,
	}, nil
}

// Empty implements OrderedSet.
func (ds *DumbSet) Empty() (bool, error) {
	return len(ds.keys) == 0, nil
}

// Items implements OrderedSet.
func (ds *DumbSet) Items() SeqResult {
	if len(ds.keys) == 0 {
		return EmptySeqResult()
	}
	return ds.seq(0)
}

// Copy implements OrderedSet.
func (ds *DumbSet) Copy(syncScope bool) OrderedSet {
	return &DumbSet{
		keys: slices.Clone(ds.keys),
	}
}

// Recent implements OrderedSet.
func (ds *DumbSet) Recent(since time.Time) (SeqResult, int) {
	return EmptySeqResult(), 0
}
