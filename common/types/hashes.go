package types

import (
	"github.com/spacemeshos/go-spacemesh/common/types/primitive"
	"github.com/spacemeshos/go-spacemesh/hash"
)

// Reexports.
type (
	Hash20 = primitive.Hash20
	Hash32 = primitive.Hash32
)

const (
	Hash20Length = primitive.Hash20Length
	Hash32Length = primitive.Hash32Length
)

var (
	BytesToHash = primitive.BytesToHash
	CalcHash20  = primitive.CalcHash20
	CalcHash32  = primitive.CalcHash32
	HexToHash32 = primitive.HexToHash32
)

// CalcProposalsHash32 returns the 32-byte blake3 sum of the IDs, sorted in lexicographic order. The pre-image is
// prefixed with additionalBytes.
func CalcProposalsHash32(view []ProposalID, additionalBytes []byte) Hash32 {
	sortedView := make([]ProposalID, len(view))
	copy(sortedView, view)
	SortProposalIDs(sortedView)
	return CalcProposalHash32Presorted(sortedView, additionalBytes)
}

// CalcProposalHash32Presorted returns the 32-byte blake3 sum of the IDs, in the order given. The pre-image is
// prefixed with additionalBytes.
func CalcProposalHash32Presorted(sortedView []ProposalID, additionalBytes []byte) Hash32 {
	hasher := hash.New()
	hasher.Write(additionalBytes)
	for _, id := range sortedView {
		hasher.Write(id.Bytes()) // this never returns an error: https://golang.org/pkg/hash/#Hash
	}
	var res Hash32
	hasher.Sum(res[:0])
	return res
}

// CalcBlockHash32Presorted returns the 32-byte blake3 sum of the IDs, in the order given. The pre-image is
// prefixed with additionalBytes.
func CalcBlockHash32Presorted(sortedView []BlockID, additionalBytes []byte) Hash32 {
	hash := hash.New()
	hash.Write(additionalBytes)
	for _, id := range sortedView {
		hash.Write(id.Bytes()) // this never returns an error: https://golang.org/pkg/hash/#Hash
	}
	var res Hash32
	hash.Sum(res[:0])
	return res
}
