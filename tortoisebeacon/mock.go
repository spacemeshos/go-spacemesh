package tortoisebeacon

import (
	"github.com/spacemeshos/sha256-simd"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type mockMessageReceiver struct {
	i        int
	messages []Message
}

func (mm *mockMessageReceiver) Receive() Message {
	m := mm.messages[mm.i%len(mm.messages)]
	mm.i++

	return m
}

type mockMessageSender struct{}

func (mockMessageSender) Send(Message) {
	return
}

type mockBeaconCalculator struct{}

func (mockBeaconCalculator) CalculateBeacon(m map[EpochRoundPair][]types.Hash32) types.Hash32 {
	hasher := sha256.New()

	for _, hashList := range m {
		for _, hash := range hashList {
			if _, err := hasher.Write(hash.Bytes()); err != nil {
				panic("should not happen") // an error is never returned: https://golang.org/pkg/hash/#Hash
			}
		}
	}

	var res types.Hash32
	hasher.Sum(res[:0])

	return res
}

func (mockBeaconCalculator) CountVotes(VotingMessage) error {
	return nil
}
