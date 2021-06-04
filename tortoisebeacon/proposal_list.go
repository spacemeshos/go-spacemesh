package tortoisebeacon

import (
	"sort"
	"strings"

	"github.com/spacemeshos/sha256-simd"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type proposalList []proposal

// Sort sorts proposal list.
func (hl proposalList) Sort() []proposal {
	hashCopy := make(proposalList, len(hl))

	copy(hashCopy, hl)

	sort.Slice(hashCopy, func(i, j int) bool {
		return strings.Compare(hashCopy[i], hashCopy[j]) == -1
	})

	return hashCopy
}

// Hash hashes proposal list.
func (hl proposalList) Hash() types.Hash32 {
	hasher := sha256.New()

	for _, hash := range hl {
		if _, err := hasher.Write([]byte(hash)); err != nil {
			panic("should not happen") // an error is never returned: https://golang.org/pkg/hash/#Hash
		}
	}

	var hash types.Hash32

	hasher.Sum(hash[:0])

	return hash
}
