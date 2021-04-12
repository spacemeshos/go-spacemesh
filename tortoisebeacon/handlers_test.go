package tortoisebeacon

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
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

func TestTortoiseBeacon_handleProposalMessage(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	const epoch = 3
	const round = 5

	hash := types.HexToHash32("0x01")

	tt := []struct {
		name          string
		epoch         types.EpochID
		currentRounds map[types.EpochID]types.RoundID
		message       ProposalMessage
		expected      proposalsMap
	}{
		{
			name:  "Case 1",
			epoch: epoch,
			currentRounds: map[types.EpochID]types.RoundID{
				epoch: round,
			},
			message: ProposalMessage{
				EpochID:      epoch,
				ProposalList: []types.ATXID{types.ATXID(hash)},
			},
			expected: proposalsMap{
				epoch: map[types.Hash32]struct{}{
					ATXIDList([]types.ATXID{types.ATXID(hash)}).Hash(): {},
				},
			},
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				Log:             log.NewDefault("TortoiseBeacon"),
				timelyProposals: proposalsMap{},
			}

			err := tb.handleProposalMessage(tc.message)
			r.NoError(err)

			r.EqualValues(tc.expected, tb.timelyProposals)
		})
	}
}

func TestTortoiseBeacon_handleVotingMessage(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	_, pk1, err := p2pcrypto.GenerateKeyPair()
	r.NoError(err)

	const epoch = 3
	const round = 5

	hash := types.HexToHash32("0x01")

	tt := []struct {
		name          string
		epoch         types.EpochID
		currentRounds map[types.EpochID]types.RoundID
		from          p2pcrypto.PublicKey
		message       VotingMessage
		expected      map[epochRoundPair]votesPerPK
	}{
		{
			name:  "Current round and message round equal",
			epoch: epoch,
			currentRounds: map[types.EpochID]types.RoundID{
				epoch: round,
			},
			from: pk1,
			message: VotingMessage{
				EpochID:      epoch,
				RoundID:      round,
				VotesFor:     []types.Hash32{hash},
				VotesAgainst: nil,
			},
			expected: map[epochRoundPair]votesPerPK{
				epochRoundPair{EpochID: epoch, Round: round}: {
					pk1: votesSetPair{
						VotesFor:     map[types.Hash32]struct{}{hash: {}},
						VotesAgainst: map[types.Hash32]struct{}{},
					},
				},
			},
		},
		{
			name:  "Current round and message round differ",
			epoch: epoch,
			currentRounds: map[types.EpochID]types.RoundID{
				epoch: round + 1,
			},
			from: pk1,
			message: VotingMessage{
				EpochID:      epoch,
				RoundID:      round,
				VotesFor:     []types.Hash32{hash},
				VotesAgainst: nil,
			},
			expected: map[epochRoundPair]votesPerPK{
				epochRoundPair{EpochID: epoch, Round: round}: {
					pk1: votesSetPair{
						VotesFor:     map[types.Hash32]struct{}{hash: {}},
						VotesAgainst: map[types.Hash32]struct{}{},
					},
				},
			},
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				Log:           log.NewDefault("TortoiseBeacon"),
				incomingVotes: map[epochRoundPair]votesPerPK{},
			}

			err := tb.handleVotingMessage(tc.from, tc.message)
			r.NoError(err)

			r.EqualValues(tc.expected, tb.incomingVotes)
		})
	}
}
