package tortoisebeacon

import (
	"github.com/spacemeshos/sha256-simd"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// ATXIDList defines ATX ID list.
type ATXIDList []types.ATXID

// Hash returns ATX ID list hash.
func (atxList ATXIDList) Hash() types.Hash32 {
	hasher := sha256.New()

	for _, id := range atxList {
		if _, err := hasher.Write(id.Bytes()); err != nil {
			panic("should not happen") // an error is never returned: https://golang.org/pkg/hash/#Hash
		}
	}

	var res types.Hash32

	hasher.Sum(res[:0])

	return res
}
