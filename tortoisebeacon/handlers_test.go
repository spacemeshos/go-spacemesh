package tortoisebeacon

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

func TestTortoiseBeacon_classifyMessage(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	epoch := types.EpochID(3)
	round := types.RoundID(4)

	tt := []struct {
		name         string
		currentRound types.RoundID
		messageRound types.RoundID
		msgType      MessageType
	}{
		{"Timely 1", 0, 0, TimelyMessage},
		{"Late 1", round, 0, LateMessage},
		{"Late 2", round, 1, LateMessage},
		{"Delayed 1", round, 2, DelayedMessage},
		{"Timely 2", round, 3, TimelyMessage},
		{"Timely 3", round, 4, TimelyMessage},
		{"Timely 4", round, 5, TimelyMessage},
		{"Timely 5", round, 6, TimelyMessage},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				Log: log.NewDefault("TortoiseBeacon"),
				currentRounds: map[types.EpochID]types.RoundID{
					epoch: tc.currentRound,
				},
			}

			m := VotingMessage{RoundID: tc.messageRound}
			result := tb.classifyMessage(m, epoch)
			r.Equal(tc.msgType, result)
		})
	}
}
