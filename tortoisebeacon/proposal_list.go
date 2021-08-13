package tortoisebeacon

import (
	"sort"
	"strings"

	"github.com/spacemeshos/sha256-simd"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type proposalList []proposal

func (hl proposalList) sort() []proposal {
	sort.Slice(hl, func(i, j int) bool {
		return strings.Compare(hl[i], hl[j]) == -1
	})

	return hl
}

func (hl proposalList) hash() types.Hash32 {
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
