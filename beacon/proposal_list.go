package beacon

import (
	"bytes"
	"encoding/hex"
	"sort"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hash"
	"go.uber.org/zap/zapcore"
)

type proposalList []Proposal

func (hl proposalList) sort() []Proposal {
	sort.Slice(hl, func(i, j int) bool {
		return bytes.Compare(hl[i][:], hl[j][:]) == -1
	})

	return hl
}

func (hl proposalList) hash() types.Hash32 {
	hasher := hash.New()

	for _, proposal := range hl {
		// an error is never returned: https://golang.org/pkg/hash/#Hash
		hasher.Write(proposal[:])
	}

	var rst types.Hash32
	hasher.Sum(rst[:0])
	return rst
}

func (hl proposalList) MarshalLogArray(enc zapcore.ArrayEncoder) error {
	for _, proposal := range hl {
		enc.AppendString(hex.EncodeToString(proposal[:]))
	}
	return nil
}
