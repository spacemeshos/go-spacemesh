package tortoisebeacon

import (
	"sort"
	"strings"

	"github.com/spacemeshos/sha256-simd"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type hashList []types.Hash32

// Hash hashes hash list.
func (hl hashList) Sort() []types.Hash32 {
	hashCopy := make(hashList, len(hl))

	copy(hashCopy, hl)

	sort.Slice(hashCopy, func(i, j int) bool {
		return strings.Compare(hashCopy[i].String(), hashCopy[j].String()) == -1
	})

	return hashCopy
}

// Hash hashes hash list.
func (hl hashList) Hash() types.Hash32 {
	hasher := sha256.New()

	for _, hash := range hl {
		if _, err := hasher.Write(hash.Bytes()); err != nil {
			panic("should not happen") // an error is never returned: https://golang.org/pkg/hash/#Hash
		}
	}

	var hash types.Hash32

	hasher.Sum(hash[:0])

	return hash
}

// Hash hashes hash list.
func (hl hashList) AsStrings() []string {
	hashes := make([]string, 0, len(hl))
	for _, hash := range hl {
		hashes = append(hashes, hash.String())
	}

	return hashes
}
