package tortoisebeacon

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
)

func TestTortoiseBeacon_calcBeacon(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	_, pk1, err := p2pcrypto.GenerateKeyPair()
	r.NoError(err)

	_, pk2, err := p2pcrypto.GenerateKeyPair()
	r.NoError(err)

	const epoch = 5
	const rounds = 3

	tt := []struct {
		name                      string
		epoch                     types.EpochID
		round                     types.RoundID
		validProposals            proposalsMap
		potentiallyValidProposals proposalsMap
		incomingVotes             map[epochRoundPair]votesPerPK
		ownVotes                  ownVotes
		hash                      types.Hash32
	}{
		{
			name:  "With Cache",
			epoch: epoch,
			ownVotes: ownVotes{
				epochRoundPair{EpochID: epoch, Round: rounds}: {
					ValidVotes: hashSet{
						types.HexToHash32("0x1"): {},
						types.HexToHash32("0x2"): {},
						types.HexToHash32("0x4"): {},
						types.HexToHash32("0x5"): {},
					},
					InvalidVotes: hashSet{
						types.HexToHash32("0x3"): {},
						types.HexToHash32("0x6"): {},
					},
				},
			},
			hash: types.HexToHash32("0xd04dd0faf9b5d3baf04dd99152971b5db67b0b3c79e5cc59f8f7b03ab20673f8"),
		},
		{
			name:  "Without Cache",
			epoch: epoch,
			round: rounds,
			validProposals: proposalsMap{
				epoch: map[types.Hash32]struct{}{
					types.HexToHash32("0x1"): {},
					types.HexToHash32("0x2"): {},
					types.HexToHash32("0x3"): {},
				},
			},
			potentiallyValidProposals: proposalsMap{
				epoch: map[types.Hash32]struct{}{
					types.HexToHash32("0x4"): {},
					types.HexToHash32("0x5"): {},
					types.HexToHash32("0x6"): {},
				},
			},
			incomingVotes: map[epochRoundPair]votesPerPK{
				epochRoundPair{EpochID: epoch, Round: 1}: {
					pk1: votesSetPair{
						ValidVotes: map[types.Hash32]struct{}{
							types.HexToHash32("0x1"): {},
							types.HexToHash32("0x2"): {},
						},
						InvalidVotes: map[types.Hash32]struct{}{
							types.HexToHash32("0x3"): {},
						},
					},
					pk2: votesSetPair{
						ValidVotes: map[types.Hash32]struct{}{
							types.HexToHash32("0x1"): {},
							types.HexToHash32("0x4"): {},
							types.HexToHash32("0x5"): {},
						},
						InvalidVotes: map[types.Hash32]struct{}{
							types.HexToHash32("0x6"): {},
						},
					},
				},
				epochRoundPair{EpochID: epoch, Round: 2}: {
					pk1: votesSetPair{
						ValidVotes: map[types.Hash32]struct{}{
							types.HexToHash32("0x3"): {},
						},
						InvalidVotes: map[types.Hash32]struct{}{
							types.HexToHash32("0x2"): {},
						},
					},
					pk2: votesSetPair{
						ValidVotes:   map[types.Hash32]struct{}{},
						InvalidVotes: map[types.Hash32]struct{}{},
					},
				},
				epochRoundPair{EpochID: epoch, Round: 3}: {
					pk1: votesSetPair{
						ValidVotes:   map[types.Hash32]struct{}{},
						InvalidVotes: map[types.Hash32]struct{}{},
					},
					pk2: votesSetPair{
						ValidVotes: map[types.Hash32]struct{}{
							types.HexToHash32("0x6"): {},
						},
						InvalidVotes: map[types.Hash32]struct{}{
							types.HexToHash32("0x5"): {},
						},
					},
				},
			},
			ownVotes: map[epochRoundPair]votesSetPair{},
			hash:     types.HexToHash32("0xd04dd0faf9b5d3baf04dd99152971b5db67b0b3c79e5cc59f8f7b03ab20673f8"),
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				config: Config{
					RoundsNumber: rounds,
				},
				Log:                       log.NewDefault("TortoiseBeacon"),
				validProposals:            tc.validProposals,
				potentiallyValidProposals: tc.potentiallyValidProposals,
				incomingVotes:             tc.incomingVotes,
				ownVotes:                  tc.ownVotes,
				votesCache:                map[epochRoundPair]votesPerPK{},
				votesCountCache:           map[epochRoundPair]map[types.Hash32]int{},
				beacons:                   make(map[types.EpochID]types.Hash32),
				beaconsReady:              make(map[types.EpochID]chan struct{}),
			}

			tb.initGenesisBeacons()
			tb.beaconsReady[3] = make(chan struct{})
			tb.beaconsReady[4] = make(chan struct{})
			tb.beaconsReady[5] = make(chan struct{})
			tb.beaconsReady[6] = make(chan struct{})

			tb.calcBeacon(tc.epoch)
			r.EqualValues(tc.hash, tb.beacons[epoch])
		})
	}
}

func TestTortoiseBeacon_calcTortoiseBeaconHashList(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	_, pk1, err := p2pcrypto.GenerateKeyPair()
	r.NoError(err)

	_, pk2, err := p2pcrypto.GenerateKeyPair()
	r.NoError(err)

	const epoch = 5
	const rounds = 3

	tt := []struct {
		name                      string
		epoch                     types.EpochID
		round                     types.RoundID
		validProposals            proposalsMap
		potentiallyValidProposals proposalsMap
		incomingVotes             map[epochRoundPair]votesPerPK
		ownVotes                  ownVotes
		hashes                    proposalList
	}{
		{
			name:  "With Cache",
			epoch: epoch,
			ownVotes: ownVotes{
				epochRoundPair{EpochID: epoch, Round: rounds}: {
					ValidVotes: hashSet{
						types.HexToHash32("0x1"): {},
						types.HexToHash32("0x2"): {},
						types.HexToHash32("0x4"): {},
						types.HexToHash32("0x5"): {},
					},
					InvalidVotes: hashSet{
						types.HexToHash32("0x3"): {},
						types.HexToHash32("0x6"): {},
					},
				},
			},
			hashes: proposalList{
				types.HexToHash32("0x1"),
				types.HexToHash32("0x2"),
				types.HexToHash32("0x4"),
				types.HexToHash32("0x5"),
			},
		},
		{
			name:  "Without Cache",
			epoch: epoch,
			round: rounds,
			validProposals: proposalsMap{
				epoch: map[types.Hash32]struct{}{
					types.HexToHash32("0x1"): {},
					types.HexToHash32("0x2"): {},
					types.HexToHash32("0x3"): {},
				},
			},
			potentiallyValidProposals: proposalsMap{
				epoch: map[types.Hash32]struct{}{
					types.HexToHash32("0x4"): {},
					types.HexToHash32("0x5"): {},
					types.HexToHash32("0x6"): {},
				},
			},
			incomingVotes: map[epochRoundPair]votesPerPK{
				epochRoundPair{EpochID: epoch, Round: 1}: {
					pk1: votesSetPair{
						ValidVotes: map[types.Hash32]struct{}{
							types.HexToHash32("0x1"): {},
							types.HexToHash32("0x2"): {},
						},
						InvalidVotes: map[types.Hash32]struct{}{
							types.HexToHash32("0x3"): {},
						},
					},
					pk2: votesSetPair{
						ValidVotes: map[types.Hash32]struct{}{
							types.HexToHash32("0x1"): {},
							types.HexToHash32("0x4"): {},
							types.HexToHash32("0x5"): {},
						},
						InvalidVotes: map[types.Hash32]struct{}{
							types.HexToHash32("0x6"): {},
						},
					},
				},
				epochRoundPair{EpochID: epoch, Round: 2}: {
					pk1: votesSetPair{
						ValidVotes: map[types.Hash32]struct{}{
							types.HexToHash32("0x3"): {},
						},
						InvalidVotes: map[types.Hash32]struct{}{
							types.HexToHash32("0x2"): {},
						},
					},
					pk2: votesSetPair{
						ValidVotes:   map[types.Hash32]struct{}{},
						InvalidVotes: map[types.Hash32]struct{}{},
					},
				},
				epochRoundPair{EpochID: epoch, Round: 3}: {
					pk1: votesSetPair{
						ValidVotes:   map[types.Hash32]struct{}{},
						InvalidVotes: map[types.Hash32]struct{}{},
					},
					pk2: votesSetPair{
						ValidVotes: map[types.Hash32]struct{}{
							types.HexToHash32("0x6"): {},
						},
						InvalidVotes: map[types.Hash32]struct{}{
							types.HexToHash32("0x5"): {},
						},
					},
				},
			},
			ownVotes: map[epochRoundPair]votesSetPair{},
			hashes: proposalList{
				types.HexToHash32("0x1"),
				types.HexToHash32("0x2"),
				types.HexToHash32("0x4"),
				types.HexToHash32("0x5"),
			},
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				config: Config{
					RoundsNumber: rounds,
				},
				Log:                       log.NewDefault("TortoiseBeacon"),
				validProposals:            tc.validProposals,
				potentiallyValidProposals: tc.potentiallyValidProposals,
				incomingVotes:             tc.incomingVotes,
				ownVotes:                  tc.ownVotes,
				votesCache:                map[epochRoundPair]votesPerPK{},
				votesCountCache:           map[epochRoundPair]map[types.Hash32]int{},
			}

			hashes := tb.calcTortoiseBeaconHashList(tc.epoch)
			r.EqualValues(tc.hashes.Sort(), hashes.Sort())
		})
	}
}
