package beacon

import (
	"bytes"
	"sort"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hash"
)

type proposalList [][]byte

func (hl proposalList) sort() [][]byte {
	sort.Slice(hl, func(i, j int) bool {
		return bytes.Compare(hl[i], hl[j]) == -1
	})

	return hl
}

func (hl proposalList) hash() types.Hash32 {
	hasher := hash.New()

	for _, proposal := range hl {
		if _, err := hasher.Write(proposal); err != nil {
			panic("should not happen") // an error is never returned: https://golang.org/pkg/hash/#Hash
		}
	}

	var rst types.Hash32
	hasher.Sum(rst[:0])
	return rst
}
