package tortoisebeacon

import (
	"math/rand"
	"time"

	"github.com/spacemeshos/sha256-simd"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

const timelyToDelayedMessageRatio = 4

type mockMessageReceiver struct {
	i        int
	messages []Message
}

func (mm *mockMessageReceiver) Receive() Message {
	m := mm.messages[mm.i%len(mm.messages)]
	mm.i++
	time.Sleep(100 * time.Millisecond) // emulate network delay

	return m
}

type mockMessageSender struct {
	rnd *rand.Rand
}

func (mms mockMessageSender) Send(Message) {
	num := mms.rnd.Intn(timelyToDelayedMessageRatio) // every 4th message is delayed
	if num == 0 {
		time.Sleep(time.Duration(TestConfig().WakeupDelta) * time.Second)
	}

	return
}

type mockBeaconCalculator struct{}

func (mockBeaconCalculator) CalculateBeacon(m map[EpochRoundPair]map[types.Hash32]struct{}) types.Hash32 {
	hasher := sha256.New()

	for _, hashList := range m {
		for hash := range hashList {
			if _, err := hasher.Write(hash.Bytes()); err != nil {
				panic("should not happen") // an error is never returned: https://golang.org/pkg/hash/#Hash
			}
		}
	}

	var res types.Hash32
	hasher.Sum(res[:0])

	return res
}

type mockWeakCoin struct{}

func (mockWeakCoin) WeakCoin(types.EpochID, int) bool {
	if rand.Intn(2) == 0 {
		return false
	}

	return true
}

type mockWeakCoinPublisher struct{}

func (m mockWeakCoinPublisher) PublishWeakCoinMessage(byte) error {
	return nil
}
