package tortoisebeacon

import (
	"math/rand"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type MockWeakCoin struct{}

func (MockWeakCoin) WeakCoin(types.EpochID, uint64) bool {
	if rand.Intn(2) == 0 {
		return false
	}

	return true
}
