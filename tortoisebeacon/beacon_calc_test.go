package tortoisebeacon

import (
	"testing"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/tortoisebeacon/weakcoin"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestTortoiseBeacon_calcBeacon(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	_, pk1, err := p2pcrypto.GenerateKeyPair()
	r.NoError(err)

	_, pk2, err := p2pcrypto.GenerateKeyPair()
	r.NoError(err)

	mockDB := &mockActivationDB{}
	mockDB.On("GetEpochWeight", mock.AnythingOfType("types.EpochID")).Return(uint64(1), nil, nil)

	mwc := &weakcoin.MockWeakCoin{}
	mwc.On("OnRoundStarted",
		mock.AnythingOfType("types.EpochID"),
		mock.AnythingOfType("types.RoundID"))
	mwc.On("OnRoundFinished",
		mock.AnythingOfType("types.EpochID"),
		mock.AnythingOfType("types.RoundID"))
	mwc.On("PublishProposal",
		mock.Anything,
		mock.AnythingOfType("types.EpochID"),
		mock.AnythingOfType("types.RoundID")).
		Return(nil)
	mwc.On("Get",
		mock.AnythingOfType("types.EpochID"),
		mock.AnythingOfType("types.RoundID")).
		Return(false)

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
						"0x1": {},
						"0x2": {},
						"0x4": {},
						"0x5": {},
					},
					InvalidVotes: hashSet{
						"0x3": {},
						"0x6": {},
					},
				},
			},
			hash: types.HexToHash32("0x6d148de54cc5ac334cdf4537018209b0e9f5ea94c049417103065eac777ddb5c"),
		},
		{
			name:  "Without Cache",
			epoch: epoch,
			round: rounds,
			validProposals: proposalsMap{
				epoch: hashSet{
					"0x1": {},
					"0x2": {},
					"0x3": {},
				},
			},
			potentiallyValidProposals: proposalsMap{
				epoch: hashSet{
					"0x4": {},
					"0x5": {},
					"0x6": {},
				},
			},
			incomingVotes: map[epochRoundPair]votesPerPK{
				epochRoundPair{EpochID: epoch, Round: 1}: {
					pk1.String(): votesSetPair{
						ValidVotes: hashSet{
							"0x1": {},
							"0x2": {},
						},
						InvalidVotes: hashSet{
							"0x3": {},
						},
					},
					pk2.String(): votesSetPair{
						ValidVotes: hashSet{
							"0x1": {},
							"0x4": {},
							"0x5": {},
						},
						InvalidVotes: hashSet{
							"0x6": {},
						},
					},
				},
				epochRoundPair{EpochID: epoch, Round: 2}: {
					pk1.String(): votesSetPair{
						ValidVotes: hashSet{
							"0x3": {},
						},
						InvalidVotes: hashSet{},
					},
					pk2.String(): votesSetPair{
						ValidVotes:   hashSet{},
						InvalidVotes: hashSet{},
					},
				},
				epochRoundPair{EpochID: epoch, Round: 3}: {
					pk1.String(): votesSetPair{
						ValidVotes:   hashSet{},
						InvalidVotes: hashSet{},
					},
					pk2.String(): votesSetPair{
						ValidVotes: hashSet{
							"0x6": {},
						},
						InvalidVotes: hashSet{},
					},
				},
			},
			ownVotes: map[epochRoundPair]votesSetPair{},
			hash:     types.HexToHash32("0x6d148de54cc5ac334cdf4537018209b0e9f5ea94c049417103065eac777ddb5c"),
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				config: Config{
					RoundsNumber: rounds,
					Theta:        1,
				},
				Log:                       log.NewDefault("TortoiseBeacon"),
				validProposals:            tc.validProposals,
				potentiallyValidProposals: tc.potentiallyValidProposals,
				incomingVotes:             tc.incomingVotes,
				ownVotes:                  tc.ownVotes,
				beacons:                   make(map[types.EpochID]types.Hash32),
				atxDB:                     mockDB,
				weakCoin:                  mwc,
			}

			tb.initGenesisBeacons()

			err := tb.calcBeacon(tc.epoch)
			r.NoError(err)
			r.EqualValues(tc.hash.String(), tb.beacons[epoch].String())
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

	mockDB := &mockActivationDB{}
	mockDB.On("GetEpochWeight", mock.AnythingOfType("types.EpochID")).Return(uint64(1), nil, nil)

	mwc := &weakcoin.MockWeakCoin{}
	mwc.On("OnRoundStarted",
		mock.AnythingOfType("types.EpochID"),
		mock.AnythingOfType("types.RoundID"))
	mwc.On("OnRoundFinished",
		mock.AnythingOfType("types.EpochID"),
		mock.AnythingOfType("types.RoundID"))
	mwc.On("PublishProposal",
		mock.Anything,
		mock.AnythingOfType("types.EpochID"),
		mock.AnythingOfType("types.RoundID")).
		Return(nil)
	mwc.On("Get",
		mock.AnythingOfType("types.EpochID"),
		mock.AnythingOfType("types.RoundID")).
		Return(false)

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
						"0x1": {},
						"0x2": {},
						"0x4": {},
						"0x5": {},
					},
					InvalidVotes: hashSet{
						"0x3": {},
						"0x6": {},
					},
				},
			},
			hashes: proposalList{
				"0x1",
				"0x2",
				"0x4",
				"0x5",
			},
		},
		{
			name:  "Without Cache",
			epoch: epoch,
			round: rounds,
			validProposals: proposalsMap{
				epoch: hashSet{
					"0x1": {},
					"0x2": {},
					"0x3": {},
				},
			},
			potentiallyValidProposals: proposalsMap{
				epoch: hashSet{
					"0x4": {},
					"0x5": {},
					"0x6": {},
				},
			},
			incomingVotes: map[epochRoundPair]votesPerPK{
				epochRoundPair{EpochID: epoch, Round: 1}: {
					pk1.String(): votesSetPair{
						ValidVotes: hashSet{
							"0x1": {},
							"0x2": {},
						},
						InvalidVotes: hashSet{
							"0x3": {},
						},
					},
					pk2.String(): votesSetPair{
						ValidVotes: hashSet{
							"0x1": {},
							"0x4": {},
							"0x5": {},
						},
						InvalidVotes: hashSet{
							"0x6": {},
						},
					},
				},
				epochRoundPair{EpochID: epoch, Round: 2}: {
					pk1.String(): votesSetPair{
						ValidVotes: hashSet{
							"0x3": {},
						},
						InvalidVotes: hashSet{},
					},
					pk2.String(): votesSetPair{
						ValidVotes:   hashSet{},
						InvalidVotes: hashSet{},
					},
				},
				epochRoundPair{EpochID: epoch, Round: 3}: {
					pk1.String(): votesSetPair{
						ValidVotes:   hashSet{},
						InvalidVotes: hashSet{},
					},
					pk2.String(): votesSetPair{
						ValidVotes: hashSet{
							"0x6": {},
						},
						InvalidVotes: hashSet{},
					},
				},
			},
			ownVotes: map[epochRoundPair]votesSetPair{},
			hashes: proposalList{
				"0x1",
				"0x2",
				"0x4",
				"0x5",
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
					Theta:        1,
				},
				Log:                       log.NewDefault("TortoiseBeacon"),
				validProposals:            tc.validProposals,
				potentiallyValidProposals: tc.potentiallyValidProposals,
				incomingVotes:             tc.incomingVotes,
				ownVotes:                  tc.ownVotes,
				atxDB:                     mockDB,
				weakCoin:                  mwc,
			}

			hashes, err := tb.calcTortoiseBeaconHashList(tc.epoch)
			r.NoError(err)
			r.EqualValues(tc.hashes.Sort(), hashes.Sort())
		})
	}
}
