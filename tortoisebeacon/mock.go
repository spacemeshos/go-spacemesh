package tortoisebeacon

import (
	"math/rand"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type mockWeakCoin struct{}

func (mockWeakCoin) WeakCoin(types.EpochID, uint64) bool {
	if rand.Intn(2) == 0 {
		return false
	}

	return true
}

type mockWeakCoinPublisher struct{}

func (m mockWeakCoinPublisher) PublishWeakCoinMessage(byte) error {
	return nil
}
