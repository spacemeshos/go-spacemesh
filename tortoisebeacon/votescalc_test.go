package tortoisebeacon

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/tortoisebeacon/weakcoin"
)

func TestTortoiseBeacon_calcVotesFromProposals(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	const epoch = 1

	tt := []struct {
		name             string
		epoch            types.EpochID
		timelyProposals  proposalsMap
		delayedProposals proposalsMap
		votesFor         hashList
		votesAgainst     hashList
	}{
		{
			name:  "Case 1",
			epoch: epoch,
			timelyProposals: proposalsMap{
				epoch: map[types.Hash32]struct{}{
					types.HexToHash32("0x1"): {},
					types.HexToHash32("0x2"): {},
					types.HexToHash32("0x3"): {},
				},
			},
			delayedProposals: proposalsMap{
				epoch: map[types.Hash32]struct{}{
					types.HexToHash32("0x4"): {},
					types.HexToHash32("0x5"): {},
					types.HexToHash32("0x6"): {},
				},
			},
			votesFor: hashList{
				types.HexToHash32("0x1"),
				types.HexToHash32("0x2"),
				types.HexToHash32("0x3"),
			},
			votesAgainst: hashList{
				types.HexToHash32("0x4"),
				types.HexToHash32("0x5"),
				types.HexToHash32("0x6"),
			},
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				Log:              log.NewDefault("TortoiseBeacon"),
				timelyProposals:  tc.timelyProposals,
				delayedProposals: tc.delayedProposals,
				votesCache:       map[epochRoundPair]votesPerPK{},
				votesCountCache:  map[epochRoundPair]map[types.Hash32]int{},
			}

			votesFor, votesAgainst := tb.calcVotesFromProposals(tc.epoch)
			r.EqualValues(tc.votesFor.Sort(), votesFor.Sort())
			r.EqualValues(tc.votesAgainst.Sort(), votesAgainst.Sort())
		})
	}
}

func TestTortoiseBeacon_calcVotesDelta(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	_, pk1, err := p2pcrypto.GenerateKeyPair()
	r.NoError(err)

	_, pk2, err := p2pcrypto.GenerateKeyPair()
	r.NoError(err)

	const epoch = 5
	const round = 3

	tt := []struct {
		name          string
		epoch         types.EpochID
		round         types.RoundID
		incomingVotes map[epochRoundPair]votesPerPK
		forDiff       hashList
		againstDiff   hashList
	}{
		{
			name:  "Case 1",
			epoch: epoch,
			round: round,
			incomingVotes: map[epochRoundPair]votesPerPK{
				epochRoundPair{EpochID: epoch, Round: 1}: {
					pk1: votesSetPair{
						VotesFor: map[types.Hash32]struct{}{
							types.HexToHash32("0x1"): {},
							types.HexToHash32("0x2"): {},
						},
						VotesAgainst: map[types.Hash32]struct{}{
							types.HexToHash32("0x3"): {},
						},
					},
					pk2: votesSetPair{
						VotesFor: map[types.Hash32]struct{}{
							types.HexToHash32("0x1"): {},
							types.HexToHash32("0x4"): {},
							types.HexToHash32("0x5"): {},
						},
						VotesAgainst: map[types.Hash32]struct{}{
							types.HexToHash32("0x6"): {},
						},
					},
				},
				epochRoundPair{EpochID: epoch, Round: 2}: {
					pk1: votesSetPair{
						VotesFor: map[types.Hash32]struct{}{
							types.HexToHash32("0x3"): {},
						},
						VotesAgainst: map[types.Hash32]struct{}{
							types.HexToHash32("0x2"): {},
						},
					},
					pk2: votesSetPair{
						VotesFor:     map[types.Hash32]struct{}{},
						VotesAgainst: map[types.Hash32]struct{}{},
					},
				},
				epochRoundPair{EpochID: epoch, Round: 3}: {
					pk1: votesSetPair{
						VotesFor:     map[types.Hash32]struct{}{},
						VotesAgainst: map[types.Hash32]struct{}{},
					},
					pk2: votesSetPair{
						VotesFor: map[types.Hash32]struct{}{
							types.HexToHash32("0x6"): {},
						},
						VotesAgainst: map[types.Hash32]struct{}{
							types.HexToHash32("0x5"): {},
						},
					},
				},
			},
			forDiff:     hashList{},
			againstDiff: hashList{},
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				Log:             log.NewDefault("TortoiseBeacon"),
				incomingVotes:   tc.incomingVotes,
				votesCache:      map[epochRoundPair]votesPerPK{},
				votesCountCache: map[epochRoundPair]map[types.Hash32]int{},
				ownVotes:        map[epochRoundPair]votesSetPair{},
			}

			forDiff, againstDiff := tb.calcVotesDelta(tc.epoch, tc.round)
			r.EqualValues(tc.forDiff.Sort(), forDiff.Sort())
			r.EqualValues(tc.againstDiff.Sort(), againstDiff.Sort())
		})
	}
}

func TestTortoiseBeacon_firstRoundVotes(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	_, pk1, err := p2pcrypto.GenerateKeyPair()
	r.NoError(err)

	_, pk2, err := p2pcrypto.GenerateKeyPair()
	r.NoError(err)

	const epoch = 5
	const round = 3

	tt := []struct {
		name          string
		epoch         types.EpochID
		upToRound     types.RoundID
		incomingVotes map[epochRoundPair]votesPerPK
		votesCount    votesCountMap
	}{
		{
			name:      "Case 1",
			epoch:     epoch,
			upToRound: round,
			incomingVotes: map[epochRoundPair]votesPerPK{
				epochRoundPair{EpochID: epoch, Round: 1}: {
					pk1: votesSetPair{
						VotesFor: map[types.Hash32]struct{}{
							types.HexToHash32("0x1"): {},
							types.HexToHash32("0x2"): {},
						},
						VotesAgainst: map[types.Hash32]struct{}{
							types.HexToHash32("0x3"): {},
							types.HexToHash32("0x5"): {},
							types.HexToHash32("0x6"): {},
						},
					},
					pk2: votesSetPair{
						VotesFor: map[types.Hash32]struct{}{
							types.HexToHash32("0x1"): {},
							types.HexToHash32("0x4"): {},
							types.HexToHash32("0x5"): {},
						},
						VotesAgainst: map[types.Hash32]struct{}{
							types.HexToHash32("0x6"): {},
						},
					},
				},
			},
			votesCount: map[types.Hash32]int{
				types.HexToHash32("0x1"): 2,
				types.HexToHash32("0x2"): 1,
				types.HexToHash32("0x3"): -1,
				types.HexToHash32("0x4"): 1,
				types.HexToHash32("0x5"): 0,
				types.HexToHash32("0x6"): -2,
			},
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				Log:             log.NewDefault("TortoiseBeacon"),
				incomingVotes:   tc.incomingVotes,
				votesCache:      map[epochRoundPair]votesPerPK{},
				votesCountCache: map[epochRoundPair]map[types.Hash32]int{},
			}

			votesCount := tb.firstRoundVotes(tc.epoch)
			r.EqualValues(tc.votesCount, votesCount)
		})
	}
}

func TestTortoiseBeacon_calcOwnFirstRoundVotes(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	_, pk1, err := p2pcrypto.GenerateKeyPair()
	r.NoError(err)

	_, pk2, err := p2pcrypto.GenerateKeyPair()
	r.NoError(err)

	const epoch = 5
	const round = 3
	const threshold = 2

	tt := []struct {
		name          string
		epoch         types.EpochID
		upToRound     types.RoundID
		incomingVotes map[epochRoundPair]votesPerPK
		weakCoin      weakcoin.WeakCoin
		result        votesSetPair
	}{
		{
			name:      "Weak Coin is false",
			epoch:     epoch,
			upToRound: round,
			incomingVotes: map[epochRoundPair]votesPerPK{
				epochRoundPair{EpochID: epoch, Round: 1}: {
					pk1: votesSetPair{
						VotesFor: map[types.Hash32]struct{}{
							types.HexToHash32("0x1"): {},
							types.HexToHash32("0x2"): {},
						},
						VotesAgainst: map[types.Hash32]struct{}{
							types.HexToHash32("0x3"): {},
							types.HexToHash32("0x6"): {},
						},
					},
					pk2: votesSetPair{
						VotesFor: map[types.Hash32]struct{}{
							types.HexToHash32("0x1"): {},
							types.HexToHash32("0x4"): {},
							types.HexToHash32("0x5"): {},
						},
						VotesAgainst: map[types.Hash32]struct{}{
							types.HexToHash32("0x6"): {},
						},
					},
				},
			},
			weakCoin: weakcoin.ValueMock{Value: false},
			result: votesSetPair{
				VotesFor: hashSet{
					types.HexToHash32("0x1"): {},
				},
				VotesAgainst: hashSet{
					types.HexToHash32("0x2"): {},
					types.HexToHash32("0x3"): {},
					types.HexToHash32("0x4"): {},
					types.HexToHash32("0x5"): {},
					types.HexToHash32("0x6"): {},
				},
			},
		},
		{
			name:      "Weak Coin is true",
			epoch:     epoch,
			upToRound: round,
			incomingVotes: map[epochRoundPair]votesPerPK{
				epochRoundPair{EpochID: epoch, Round: 1}: {
					pk1: votesSetPair{
						VotesFor: map[types.Hash32]struct{}{
							types.HexToHash32("0x1"): {},
							types.HexToHash32("0x2"): {},
						},
						VotesAgainst: map[types.Hash32]struct{}{
							types.HexToHash32("0x3"): {},
							types.HexToHash32("0x6"): {},
						},
					},
					pk2: votesSetPair{
						VotesFor: map[types.Hash32]struct{}{
							types.HexToHash32("0x1"): {},
							types.HexToHash32("0x4"): {},
							types.HexToHash32("0x5"): {},
						},
						VotesAgainst: map[types.Hash32]struct{}{
							types.HexToHash32("0x6"): {},
						},
					},
				},
			},
			weakCoin: weakcoin.ValueMock{Value: true},
			result: votesSetPair{
				VotesFor: hashSet{
					types.HexToHash32("0x1"): {},
					types.HexToHash32("0x2"): {},
					types.HexToHash32("0x3"): {},
					types.HexToHash32("0x4"): {},
					types.HexToHash32("0x5"): {},
				},
				VotesAgainst: hashSet{
					types.HexToHash32("0x6"): {},
				},
			},
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				config: Config{
					Theta: 1,
					TAve:  threshold,
				},
				Log:             log.NewDefault("TortoiseBeacon"),
				weakCoin:        tc.weakCoin,
				incomingVotes:   tc.incomingVotes,
				votesCache:      map[epochRoundPair]votesPerPK{},
				votesCountCache: map[epochRoundPair]map[types.Hash32]int{},
				ownVotes:        map[epochRoundPair]votesSetPair{},
			}

			votesCount := tb.firstRoundVotes(tc.epoch)
			result := tb.calcOwnFirstRoundVotes(tc.epoch, votesCount)
			r.EqualValues(tc.result, result)
		})
	}
}

func TestTortoiseBeacon_calcVotesCount(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	_, pk1, err := p2pcrypto.GenerateKeyPair()
	r.NoError(err)

	_, pk2, err := p2pcrypto.GenerateKeyPair()
	r.NoError(err)

	const epoch = 5
	const round = 3

	tt := []struct {
		name          string
		epoch         types.EpochID
		upToRound     types.RoundID
		incomingVotes map[epochRoundPair]votesPerPK
		result        votesCountMap
	}{
		{
			name:      "Case 1",
			epoch:     epoch,
			upToRound: round,
			incomingVotes: map[epochRoundPair]votesPerPK{
				epochRoundPair{EpochID: epoch, Round: 1}: {
					pk1: votesSetPair{
						VotesFor: map[types.Hash32]struct{}{
							types.HexToHash32("0x1"): {},
							types.HexToHash32("0x2"): {},
						},
						VotesAgainst: map[types.Hash32]struct{}{
							types.HexToHash32("0x3"): {},
						},
					},
					pk2: votesSetPair{
						VotesFor: map[types.Hash32]struct{}{
							types.HexToHash32("0x1"): {},
							types.HexToHash32("0x4"): {},
							types.HexToHash32("0x5"): {},
						},
						VotesAgainst: map[types.Hash32]struct{}{
							types.HexToHash32("0x6"): {},
						},
					},
				},
				epochRoundPair{EpochID: epoch, Round: 2}: {
					pk1: votesSetPair{
						VotesFor: map[types.Hash32]struct{}{
							types.HexToHash32("0x3"): {},
						},
						VotesAgainst: map[types.Hash32]struct{}{
							types.HexToHash32("0x2"): {},
						},
					},
					pk2: votesSetPair{
						VotesFor:     map[types.Hash32]struct{}{},
						VotesAgainst: map[types.Hash32]struct{}{},
					},
				},
				epochRoundPair{EpochID: epoch, Round: 3}: {
					pk1: votesSetPair{
						VotesFor:     map[types.Hash32]struct{}{},
						VotesAgainst: map[types.Hash32]struct{}{},
					},
					pk2: votesSetPair{
						VotesFor: map[types.Hash32]struct{}{
							types.HexToHash32("0x6"): {},
						},
						VotesAgainst: map[types.Hash32]struct{}{
							types.HexToHash32("0x5"): {},
						},
					},
				},
			},
			result: map[types.Hash32]int{
				types.HexToHash32("0x1"): 6,
				types.HexToHash32("0x2"): 1,
				types.HexToHash32("0x3"): -1,
				types.HexToHash32("0x4"): 3,
				types.HexToHash32("0x5"): 1,
				types.HexToHash32("0x6"): -1,
			},
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				Log:             log.NewDefault("TortoiseBeacon"),
				incomingVotes:   tc.incomingVotes,
				votesCache:      map[epochRoundPair]votesPerPK{},
				votesCountCache: map[epochRoundPair]map[types.Hash32]int{},
			}

			votesCount := tb.firstRoundVotes(tc.epoch)
			tb.calcVotesCount(tc.epoch, tc.upToRound, votesCount)
			r.EqualValues(tc.result, votesCount)
		})
	}
}

func TestTortoiseBeacon_calcOneRoundVotes(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	_, pk1, err := p2pcrypto.GenerateKeyPair()
	r.NoError(err)

	_, pk2, err := p2pcrypto.GenerateKeyPair()
	r.NoError(err)

	const epoch = 5
	const round = 3

	tt := []struct {
		name          string
		epoch         types.EpochID
		round         types.RoundID
		incomingVotes map[epochRoundPair]votesPerPK
		result        votesPerPK
	}{
		{
			name:  "Case 1",
			epoch: epoch,
			round: round,
			incomingVotes: map[epochRoundPair]votesPerPK{
				epochRoundPair{EpochID: epoch, Round: 1}: {
					pk1: votesSetPair{
						VotesFor: map[types.Hash32]struct{}{
							types.HexToHash32("0x1"): {},
							types.HexToHash32("0x2"): {},
						},
						VotesAgainst: map[types.Hash32]struct{}{
							types.HexToHash32("0x3"): {},
						},
					},
					pk2: votesSetPair{
						VotesFor: map[types.Hash32]struct{}{
							types.HexToHash32("0x4"): {},
							types.HexToHash32("0x5"): {},
						},
						VotesAgainst: map[types.Hash32]struct{}{
							types.HexToHash32("0x6"): {},
						},
					},
				},
				epochRoundPair{EpochID: epoch, Round: 2}: {
					pk1: votesSetPair{
						VotesFor:     map[types.Hash32]struct{}{},
						VotesAgainst: map[types.Hash32]struct{}{},
					},
					pk2: votesSetPair{
						VotesFor:     map[types.Hash32]struct{}{},
						VotesAgainst: map[types.Hash32]struct{}{},
					},
				},
				epochRoundPair{EpochID: epoch, Round: 3}: {
					pk1: votesSetPair{
						VotesFor:     map[types.Hash32]struct{}{},
						VotesAgainst: map[types.Hash32]struct{}{},
					},
					pk2: votesSetPair{
						VotesFor:     map[types.Hash32]struct{}{},
						VotesAgainst: map[types.Hash32]struct{}{},
					},
				},
			},
			result: votesPerPK{
				pk1: votesSetPair{
					VotesFor: map[types.Hash32]struct{}{
						types.HexToHash32("0x1"): {},
						types.HexToHash32("0x2"): {},
					},
					VotesAgainst: map[types.Hash32]struct{}{
						types.HexToHash32("0x3"): {},
					},
				},
				pk2: votesSetPair{
					VotesFor: map[types.Hash32]struct{}{
						types.HexToHash32("0x4"): {},
						types.HexToHash32("0x5"): {},
					},
					VotesAgainst: map[types.Hash32]struct{}{
						types.HexToHash32("0x6"): {},
					},
				},
			},
		},
		{
			name:  "Case 2",
			epoch: epoch,
			round: round,
			incomingVotes: map[epochRoundPair]votesPerPK{
				epochRoundPair{EpochID: epoch, Round: 1}: {
					pk1: votesSetPair{
						VotesFor: map[types.Hash32]struct{}{
							types.HexToHash32("0x1"): {},
							types.HexToHash32("0x2"): {},
						},
						VotesAgainst: map[types.Hash32]struct{}{
							types.HexToHash32("0x3"): {},
						},
					},
					pk2: votesSetPair{
						VotesFor: map[types.Hash32]struct{}{
							types.HexToHash32("0x4"): {},
							types.HexToHash32("0x5"): {},
						},
						VotesAgainst: map[types.Hash32]struct{}{
							types.HexToHash32("0x6"): {},
						},
					},
				},
				epochRoundPair{EpochID: epoch, Round: 2}: {
					// Should *NOT* affect the result (2 != 3).
					pk1: votesSetPair{
						VotesFor: map[types.Hash32]struct{}{
							types.HexToHash32("0x3"): {},
						},
						VotesAgainst: map[types.Hash32]struct{}{
							types.HexToHash32("0x2"): {},
						},
					},
					pk2: votesSetPair{
						VotesFor:     map[types.Hash32]struct{}{},
						VotesAgainst: map[types.Hash32]struct{}{},
					},
				},
				epochRoundPair{EpochID: epoch, Round: 3}: {
					pk1: votesSetPair{
						VotesFor:     map[types.Hash32]struct{}{},
						VotesAgainst: map[types.Hash32]struct{}{},
					},
					// Should affect the result (3 == 3).
					pk2: votesSetPair{
						VotesFor: map[types.Hash32]struct{}{
							types.HexToHash32("0x6"): {},
						},
						VotesAgainst: map[types.Hash32]struct{}{
							types.HexToHash32("0x5"): {},
						},
					},
				},
			},
			result: votesPerPK{
				pk1: votesSetPair{
					VotesFor: map[types.Hash32]struct{}{
						types.HexToHash32("0x1"): {},
						types.HexToHash32("0x2"): {},
					},
					VotesAgainst: map[types.Hash32]struct{}{
						types.HexToHash32("0x3"): {},
					},
				},
				pk2: votesSetPair{
					VotesFor: map[types.Hash32]struct{}{
						types.HexToHash32("0x4"): {},
						types.HexToHash32("0x6"): {},
					},
					VotesAgainst: map[types.Hash32]struct{}{
						types.HexToHash32("0x5"): {},
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
				incomingVotes: tc.incomingVotes,
				votesCache:    map[epochRoundPair]votesPerPK{},
			}

			result := tb.calcOneRoundVotes(tc.epoch, tc.round)
			r.EqualValues(tc.result, result)
			r.EqualValues(tc.result, tb.votesCache[epochRoundPair{EpochID: epoch, Round: tc.round}])
		})
	}
}

func TestTortoiseBeacon_copyFirstRoundVotes(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	_, pk1, err := p2pcrypto.GenerateKeyPair()
	r.NoError(err)

	_, pk2, err := p2pcrypto.GenerateKeyPair()
	r.NoError(err)

	const epoch = 5

	tt := []struct {
		name          string
		epoch         types.EpochID
		incomingVotes map[epochRoundPair]votesPerPK
		result        votesPerPK
	}{
		{
			name:  "Case 1",
			epoch: epoch,
			incomingVotes: map[epochRoundPair]votesPerPK{
				epochRoundPair{EpochID: epoch, Round: 1}: {
					pk1: votesSetPair{
						VotesFor: map[types.Hash32]struct{}{
							types.HexToHash32("0x1"): {},
							types.HexToHash32("0x2"): {},
						},
						VotesAgainst: map[types.Hash32]struct{}{
							types.HexToHash32("0x3"): {},
						},
					},
					pk2: votesSetPair{
						VotesFor: map[types.Hash32]struct{}{
							types.HexToHash32("0x4"): {},
							types.HexToHash32("0x5"): {},
						},
						VotesAgainst: map[types.Hash32]struct{}{
							types.HexToHash32("0x6"): {},
						},
					},
				},
			},
			result: votesPerPK{
				pk1: votesSetPair{
					VotesFor: map[types.Hash32]struct{}{
						types.HexToHash32("0x1"): {},
						types.HexToHash32("0x2"): {},
					},
					VotesAgainst: map[types.Hash32]struct{}{
						types.HexToHash32("0x3"): {},
					},
				},
				pk2: votesSetPair{
					VotesFor: map[types.Hash32]struct{}{
						types.HexToHash32("0x4"): {},
						types.HexToHash32("0x5"): {},
					},
					VotesAgainst: map[types.Hash32]struct{}{
						types.HexToHash32("0x6"): {},
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
				incomingVotes: tc.incomingVotes,
			}

			result := tb.copyFirstRoundVotes(tc.epoch)
			r.EqualValues(tc.result, result)
		})
	}
}

func TestTortoiseBeacon_calcOwnCurrentRoundVotes(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	const threshold = 3

	tt := []struct {
		name               string
		epoch              types.EpochID
		round              types.RoundID
		ownFirstRoundVotes votesSetPair
		votesCount         votesCountMap
		weakCoin           weakcoin.WeakCoin
		result             votesSetPair
	}{
		{
			name:  "Case 1",
			epoch: 5,
			round: 5,
			ownFirstRoundVotes: votesSetPair{
				VotesFor: map[types.Hash32]struct{}{
					types.HexToHash32("0x1"): {},
					types.HexToHash32("0x2"): {},
				},
				VotesAgainst: map[types.Hash32]struct{}{
					types.HexToHash32("0x3"): {},
				},
			},
			votesCount: votesCountMap{
				types.HexToHash32("0x1"): threshold * 2,
				types.HexToHash32("0x2"): -threshold * 3,
				types.HexToHash32("0x3"): threshold / 2,
			},
			weakCoin: weakcoin.ValueMock{Value: true},
			result: votesSetPair{
				VotesFor: map[types.Hash32]struct{}{
					types.HexToHash32("0x1"): {},
					types.HexToHash32("0x3"): {},
				},
				VotesAgainst: map[types.Hash32]struct{}{
					types.HexToHash32("0x2"): {},
				},
			},
		},
		{
			name:  "Case 2",
			epoch: 5,
			round: 5,
			votesCount: votesCountMap{
				types.HexToHash32("0x1"): threshold * 2,
				types.HexToHash32("0x2"): -threshold * 3,
				types.HexToHash32("0x3"): threshold / 2,
			},
			weakCoin: weakcoin.ValueMock{Value: false},
			result: votesSetPair{
				VotesFor: map[types.Hash32]struct{}{
					types.HexToHash32("0x1"): {},
				},
				VotesAgainst: map[types.Hash32]struct{}{
					types.HexToHash32("0x2"): {},
					types.HexToHash32("0x3"): {},
				},
			},
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				config: Config{
					Theta: 1,
					TAve:  threshold,
				},
				Log:             log.NewDefault("TortoiseBeacon"),
				ownVotes:        map[epochRoundPair]votesSetPair{},
				votesCountCache: map[epochRoundPair]map[types.Hash32]int{},
				weakCoin:        tc.weakCoin,
			}

			result := tb.calcOwnCurrentRoundVotes(tc.epoch, tc.round, tc.votesCount)
			r.EqualValues(tc.result, result)
		})
	}
}
