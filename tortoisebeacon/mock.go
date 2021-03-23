package tortoisebeacon

import (
	"math/rand"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// MockWeakCoin mocks weak coin.
type MockWeakCoin struct{}

// WeakCoin returns a weak coin value for a certain epoch and round.
func (MockWeakCoin) WeakCoin(types.EpochID, uint64) bool {
	if rand.Intn(2) == 0 {
		return false
	}

	return true
}
