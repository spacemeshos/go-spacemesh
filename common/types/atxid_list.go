package types

import "github.com/spacemeshos/go-spacemesh/hash"

// ATXIDList defines ATX ID list.
type ATXIDList []ATXID

func ATXIDListFromPtr(a []*ATXID) ATXIDList {
	return ATXIDList(PtrSliceToSlice(a))
}

func PtrSliceToSlice[V any](a []*V) []V {
	if len(a) == 0 {
		return []V{}
	}
	b := make([]V, len(a))
	for i, val := range a {
		b[i] = *val
	}
	return b
}

func SliceToPtrSlice[V any](a []V) []*V {
	if len(a) == 0 {
		return []*V{}
	}
	b := make([]*V, len(a))
	for i, val := range a {
		b[i] = &val
	}
	return b
}

// Hash returns ATX ID list hash.
func (atxList ATXIDList) Hash() Hash32 {
	hasher := hash.GetHasher()
	defer hash.PutHasher(hasher)
	for _, id := range atxList {
		if _, err := hasher.Write(id.Bytes()); err != nil {
			panic("should not happen") // an error is never returned: https://golang.org/pkg/hash/#Hash
		}
	}

	var rst Hash32
	hasher.Sum(rst[:0])
	return rst
}
