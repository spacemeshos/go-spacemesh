package types

import "github.com/spacemeshos/sha256-simd"

// ATXIDList defines ATX ID list.
type ATXIDList []ATXID

// Hash returns ATX ID list hash.
func (atxList ATXIDList) Hash() Hash32 {
	hasher := sha256.New()

	for _, id := range atxList {
		if _, err := hasher.Write(id.Bytes()); err != nil {
			panic("should not happen") // an error is never returned: https://golang.org/pkg/hash/#Hash
		}
	}

	var res Hash32

	hasher.Sum(res[:0])

	return res
}
