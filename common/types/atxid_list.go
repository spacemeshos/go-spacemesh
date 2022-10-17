package types

import "github.com/spacemeshos/go-spacemesh/hash"

// ATXIDList defines ATX ID list.
type ATXIDList []ATXID

// Hash returns ATX ID list hash.
func (atxList ATXIDList) Hash() Hash32 {
	hasher := hash.New()

	for _, id := range atxList {
		if _, err := hasher.Write(id.Bytes()); err != nil {
			panic("should not happen") // an error is never returned: https://golang.org/pkg/hash/#Hash
		}
	}

	var rst Hash32
	hasher.Sum(rst[:0])
	return rst
}
