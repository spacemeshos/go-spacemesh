package tortoisebeacon

import (
	"context"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestTortoiseBeacon_handleProposalMessage(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	hash := types.HexToHash32("0x12345678")

	genesisTime := time.Now().Add(time.Second * 10)
	ld := time.Duration(10) * time.Second
	clock := timesync.NewClock(timesync.RealClock{}, ld, genesisTime, log.NewDefault("clock"))
	clock.StartNotifying()

	mockDB := &mockActivationDB{}
	mockDB.On("GetEpochWeight",
		mock.AnythingOfType("types.EpochID")).
		Return(uint64(10), nil, nil)
	mockDB.On("GetNodeAtxIDForEpoch",
		mock.Anything,
		mock.Anything).
		Return(types.ATXID(hash), nil)
	mockDB.On("GetAtxHeader",
		mock.AnythingOfType("types.ATXID")).
		Return(&types.ActivationTxHeader{
			NIPSTChallenge: types.NIPSTChallenge{
				StartTick: 1,
				EndTick:   3,
			},
			Space: 5,
		}, nil)
	mockDB.On("GetAtxTimestamp",
		mock.AnythingOfType("types.ATXID")).
		Return(time.Now(), nil)

	const epoch = 3
	const round = 5

	types.SetLayersPerEpoch(1)

	edSgn := signing.NewEdSigner()
	edPubkey := edSgn.PublicKey()

	vrfSigner, vrfPub, err := signing.NewVRFSigner(edSgn.Sign(edPubkey.Bytes()))
	r.NoError(err)

	minerID := types.NodeID{Key: edPubkey.String(), VRFPublicKey: vrfPub}
	r.NoError(err)

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
				MinerID: minerID,
			},
			expected: proposalsMap{
				epoch: hashSet{
					hash.String(): {},
				},
			},
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				config:         TestConfig(),
				Log:            log.NewDefault("TortoiseBeacon"),
				validProposals: proposalsMap{},
				atxDB:          mockDB,
				vrfVerifier:    signing.VRFVerify,
				vrfSigner:      vrfSigner,
				clock:          clock,
			}

			sig, err := tb.calcProposalSignature(epoch)
			r.NoError(err)
			tc.message.VRFSignature = sig

			err = tb.handleProposalMessage(context.TODO(), tc.message)
			r.NoError(err)

			r.EqualValues(tc.expected, tb.validProposals)
		})
	}
}

func TestTortoiseBeacon_handleFirstVotingMessage(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	hash := types.HexToHash32("0x12345678")

	_, pk1, err := p2pcrypto.GenerateKeyPair()
	r.NoError(err)

	genesisTime := time.Now().Add(time.Second * 10)
	ld := time.Duration(10) * time.Second
	clock := timesync.NewClock(timesync.RealClock{}, ld, genesisTime, log.NewDefault("clock"))
	clock.StartNotifying()

	mockDB := &mockActivationDB{}
	mockDB.On("GetEpochWeight",
		mock.AnythingOfType("types.EpochID")).
		Return(uint64(10), nil, nil)
	mockDB.On("GetNodeAtxIDForEpoch",
		mock.Anything,
		mock.Anything).
		Return(types.ATXID(hash), nil)
	mockDB.On("GetAtxHeader",
		mock.AnythingOfType("types.ATXID")).
		Return(&types.ActivationTxHeader{
			NIPSTChallenge: types.NIPSTChallenge{
				StartTick: 1,
				EndTick:   3,
			},
			Space: 5,
		}, nil)
	mockDB.On("GetAtxTimestamp",
		mock.AnythingOfType("types.ATXID")).
		Return(time.Now(), nil)

	const epoch = 3
	const round = 5

	types.SetLayersPerEpoch(1)

	edSgn := signing.NewEdSigner()
	edPubkey := edSgn.PublicKey()

	vrfSigner, vrfPub, err := signing.NewVRFSigner(edSgn.Sign(edPubkey.Bytes()))
	r.NoError(err)

	minerID := types.NodeID{Key: edPubkey.String(), VRFPublicKey: vrfPub}
	r.NoError(err)

	tt := []struct {
		name          string
		epoch         types.EpochID
		currentRounds map[types.EpochID]types.RoundID
		from          p2pcrypto.PublicKey
		message       FirstVotingMessage
		expected      map[epochRoundPair]votesPerPK
	}{
		{
			name:  "Current round and message round equal",
			epoch: epoch,
			currentRounds: map[types.EpochID]types.RoundID{
				epoch: round,
			},
			from: pk1,
			message: FirstVotingMessage{
				FirstVotingMessageBody: FirstVotingMessageBody{
					MinerID:                   minerID,
					ValidProposals:            [][]byte{hash.Bytes()},
					PotentiallyValidProposals: nil,
				},
			},
			expected: map[epochRoundPair]votesPerPK{
				epochRoundPair{EpochID: epoch, Round: round}: {
					pk1.String(): votesSetPair{
						ValidVotes:   hashSet{hash.String(): {}},
						InvalidVotes: hashSet{},
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
			message: FirstVotingMessage{
				FirstVotingMessageBody: FirstVotingMessageBody{
					MinerID:                   minerID,
					ValidProposals:            [][]byte{hash.Bytes()},
					PotentiallyValidProposals: nil,
				},
			},
			expected: map[epochRoundPair]votesPerPK{
				epochRoundPair{EpochID: epoch, Round: round}: {
					pk1.String(): votesSetPair{
						ValidVotes:   hashSet{hash.String(): {}},
						InvalidVotes: hashSet{},
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
				Log:                     log.NewDefault("TortoiseBeacon"),
				incomingVotes:           map[epochRoundPair]votesPerPK{},
				atxDB:                   mockDB,
				vrfVerifier:             signing.VRFVerify,
				vrfSigner:               vrfSigner,
				edSigner:                edSgn,
				clock:                   clock,
				firstRoundIncomingVotes: map[types.EpochID]firstRoundVotesPerPK{},
			}

			sig, err := tb.calcEligibilityProof(tc.message.FirstVotingMessageBody)
			r.NoError(err)
			tc.message.Signature = sig

			err = tb.handleFirstVotingMessage(context.TODO(), tc.message)
			r.NoError(err)

			r.EqualValues(tc.expected, tb.incomingVotes)
		})
	}
}

func TestTortoiseBeacon_handleFollowingVotingMessage(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	hash1 := types.HexToHash32("0x12345678")
	hash2 := types.HexToHash32("0x23456789")
	hash3 := types.HexToHash32("0x34567890")

	_, pk1, err := p2pcrypto.GenerateKeyPair()
	r.NoError(err)

	genesisTime := time.Now().Add(time.Second * 10)
	ld := time.Duration(10) * time.Second
	clock := timesync.NewClock(timesync.RealClock{}, ld, genesisTime, log.NewDefault("clock"))
	clock.StartNotifying()

	mockDB := &mockActivationDB{}
	mockDB.On("GetEpochWeight",
		mock.AnythingOfType("types.EpochID")).
		Return(uint64(10), nil, nil)
	mockDB.On("GetNodeAtxIDForEpoch",
		mock.Anything,
		mock.Anything).
		Return(types.ATXID(hash1), nil)
	mockDB.On("GetAtxHeader",
		mock.AnythingOfType("types.ATXID")).
		Return(&types.ActivationTxHeader{
			NIPSTChallenge: types.NIPSTChallenge{
				StartTick: 1,
				EndTick:   3,
			},
			Space: 5,
		}, nil)
	mockDB.On("GetAtxTimestamp",
		mock.AnythingOfType("types.ATXID")).
		Return(time.Now(), nil)

	const epoch = 3
	const round = 5

	types.SetLayersPerEpoch(1)

	edSgn := signing.NewEdSigner()
	edPubkey := edSgn.PublicKey()

	vrfSigner, vrfPub, err := signing.NewVRFSigner(edSgn.Sign(edPubkey.Bytes()))
	r.NoError(err)

	minerID := types.NodeID{Key: edPubkey.String(), VRFPublicKey: vrfPub}
	r.NoError(err)

	tt := []struct {
		name          string
		epoch         types.EpochID
		currentRounds map[types.EpochID]types.RoundID
		from          p2pcrypto.PublicKey
		message       FollowingVotingMessage
		expected      map[epochRoundPair]votesPerPK
	}{
		{
			name:  "Current round and message round equal",
			epoch: epoch,
			currentRounds: map[types.EpochID]types.RoundID{
				epoch: round,
			},
			from: pk1,
			message: FollowingVotingMessage{
				FollowingVotingMessageBody: FollowingVotingMessageBody{
					MinerID:        minerID,
					RoundID:        round,
					VotesBitVector: []uint64{0b101},
				},
			},
			expected: map[epochRoundPair]votesPerPK{
				epochRoundPair{EpochID: epoch, Round: round}: {
					pk1.String(): votesSetPair{
						ValidVotes:   hashSet{hash3.String(): {}},
						InvalidVotes: hashSet{},
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
			message: FollowingVotingMessage{
				FollowingVotingMessageBody: FollowingVotingMessageBody{
					MinerID:        minerID,
					RoundID:        round,
					VotesBitVector: []uint64{0b101},
				},
			},
			expected: map[epochRoundPair]votesPerPK{
				epochRoundPair{EpochID: epoch, Round: round}: {
					pk1.String(): votesSetPair{
						ValidVotes:   hashSet{hash3.String(): {}},
						InvalidVotes: hashSet{},
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
				atxDB:         mockDB,
				vrfVerifier:   signing.VRFVerify,
				vrfSigner:     vrfSigner,
				edSigner:      edSgn,
				clock:         clock,
				firstRoundIncomingVotes: map[types.EpochID]firstRoundVotesPerPK{
					epoch: {
						pk1.String(): {
							ValidVotes:            []proposal{hash1.String(), hash2.String()},
							PotentiallyValidVotes: []proposal{hash3.String()},
						},
					},
				},
			}

			sig, err := tb.calcEligibilityProof(tc.message.FollowingVotingMessageBody)
			r.NoError(err)
			tc.message.Signature = sig

			err = tb.handleFollowingVotingMessage(context.TODO(), tc.message)
			r.NoError(err)

			r.EqualValues(tc.expected, tb.incomingVotes)
		})
	}
}
