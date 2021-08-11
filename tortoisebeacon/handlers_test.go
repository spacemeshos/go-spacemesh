package tortoisebeacon

import (
	"math/big"
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
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
	clock := timesync.NewClock(timesync.RealClock{}, ld, genesisTime, logtest.New(t).WithName("clock"))
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
			NIPostChallenge: types.NIPostChallenge{
				StartTick: 1,
				EndTick:   3,
			},
			NumUnits: 5,
		}, nil)
	mockDB.On("GetAtxTimestamp",
		mock.AnythingOfType("types.ATXID")).
		Return(time.Now(), nil)

	const (
		epoch = 3
		round = 5
	)

	types.SetLayersPerEpoch(1)

	edSgn := signing.NewEdSigner()
	edPubkey := edSgn.PublicKey()

	vrfSigner, vrfPub, err := signing.NewVRFSigner(edSgn.Sign(edPubkey.Bytes()))
	r.NoError(err)

	minerID := types.NodeID{Key: edPubkey.String(), VRFPublicKey: vrfPub}

	tt := []struct {
		name          string
		epoch         types.EpochID
		currentRounds map[types.EpochID]types.RoundID
		message       ProposalMessage
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
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				config:      UnitTestConfig(),
				Log:         logtest.New(t).WithName("TortoiseBeacon"),
				atxDB:       mockDB,
				vrfVerifier: signing.VRFVerifier{},
				vrfSigner:   vrfSigner,
				clock:       clock,
				lastLayer:   types.NewLayerID(epoch),
			}

			sig, err := tb.getSignedProposal(epoch)
			r.NoError(err)
			tc.message.VRFSignature = sig

			err = tb.handleProposalMessage(tc.message, time.Now())
			r.NoError(err)

			expected := proposals{
				valid: [][]byte{sig},
			}

			r.EqualValues(expected, tb.incomingProposals)
		})
	}
}

func TestTortoiseBeacon_handleFirstVotingMessage(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	hash := types.HexToHash32("0x12345678")

	genesisTime := time.Now().Add(time.Second * 10)
	ld := time.Duration(10) * time.Second
	clock := timesync.NewClock(timesync.RealClock{}, ld, genesisTime, logtest.New(t).WithName("clock"))
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
			NIPostChallenge: types.NIPostChallenge{
				StartTick: 1,
				EndTick:   3,
			},
			NumUnits: 5,
		}, nil)
	mockDB.On("GetAtxTimestamp",
		mock.AnythingOfType("types.ATXID")).
		Return(time.Now(), nil)

	const (
		epoch = 0
		round = 0
	)

	types.SetLayersPerEpoch(1)

	edSgn := signing.NewEdSigner()
	edPubkey := edSgn.PublicKey()

	vrfSigner, vrfPub, err := signing.NewVRFSigner(edSgn.Sign(edPubkey.Bytes()))
	r.NoError(err)

	minerID := types.NodeID{Key: edPubkey.String(), VRFPublicKey: vrfPub}

	tt := []struct {
		name          string
		epoch         types.EpochID
		currentRounds map[types.EpochID]types.RoundID
		from          string
		message       FirstVotingMessage
		expected      map[nodeID]proposals
	}{
		{
			name:  "Current round and message round equal",
			epoch: epoch,
			currentRounds: map[types.EpochID]types.RoundID{
				epoch: round,
			},
			from: minerID.Key,
			message: FirstVotingMessage{
				FirstVotingMessageBody: FirstVotingMessageBody{
					ValidProposals:            [][]byte{hash.Bytes()},
					PotentiallyValidProposals: nil,
				},
			},
			expected: map[nodeID]proposals{
				minerID.Key: {
					valid: [][]byte{hash.Bytes()},
				},
			},
		},
		{
			name:  "Current round and message round differ",
			epoch: epoch,
			currentRounds: map[types.EpochID]types.RoundID{
				epoch: round + 1,
			},
			from: minerID.Key,
			message: FirstVotingMessage{
				FirstVotingMessageBody: FirstVotingMessageBody{
					ValidProposals:            [][]byte{hash.Bytes()},
					PotentiallyValidProposals: nil,
				},
			},
			expected: map[nodeID]proposals{
				minerID.Key: {
					valid: [][]byte{hash.Bytes()},
				},
			},
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				Log:                     logtest.New(t).WithName("TortoiseBeacon"),
				atxDB:                   mockDB,
				vrfVerifier:             signing.VRFVerifier{},
				vrfSigner:               vrfSigner,
				edSigner:                edSgn,
				clock:                   clock,
				firstRoundIncomingVotes: map[nodeID]proposals{},
				votesMargin:             map[proposal]*big.Int{},
				hasVoted:                make([]map[nodeID]struct{}, round+1),
			}

			sig, err := tb.signMessage(tc.message.FirstVotingMessageBody)
			r.NoError(err)
			tc.message.Signature = sig

			err = tb.handleFirstVotingMessage(tc.message)
			r.NoError(err)

			r.EqualValues(tc.expected, tb.firstRoundIncomingVotes)
		})
	}
}

func TestTortoiseBeacon_handleFollowingVotingMessage(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	hash1 := types.HexToHash32("0x12345678")
	hash2 := types.HexToHash32("0x23456789")
	hash3 := types.HexToHash32("0x34567890")

	genesisTime := time.Now().Add(time.Second * 10)
	ld := time.Duration(10) * time.Second
	clock := timesync.NewClock(timesync.RealClock{}, ld, genesisTime, logtest.New(t).WithName("clock"))
	clock.StartNotifying()

	mockDB := &mockActivationDB{}
	mockDB.On("GetEpochWeight",
		mock.AnythingOfType("types.EpochID")).
		Return(uint64(1), nil, nil)
	mockDB.On("GetNodeAtxIDForEpoch",
		mock.Anything,
		mock.Anything).
		Return(types.ATXID(hash1), nil)
	mockDB.On("GetAtxHeader",
		mock.AnythingOfType("types.ATXID")).
		Return(&types.ActivationTxHeader{
			NIPostChallenge: types.NIPostChallenge{
				StartTick: 0,
				EndTick:   1,
			},
			NumUnits: 1,
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

	tt := []struct {
		name          string
		epoch         types.EpochID
		currentRounds map[types.EpochID]types.RoundID
		message       FollowingVotingMessage
		expected      map[proposal]*big.Int
	}{
		{
			name:  "Current round and message round equal",
			epoch: epoch,
			currentRounds: map[types.EpochID]types.RoundID{
				epoch: round,
			},
			message: FollowingVotingMessage{
				FollowingVotingMessageBody: FollowingVotingMessageBody{
					RoundID:        round,
					VotesBitVector: []uint64{0b101},
				},
			},
			expected: map[proposal]*big.Int{
				string(hash1.Bytes()): big.NewInt(1),
				string(hash2.Bytes()): big.NewInt(-1),
				string(hash3.Bytes()): big.NewInt(1),
			},
		},
		{
			name:  "Current round and message round differ",
			epoch: epoch,
			currentRounds: map[types.EpochID]types.RoundID{
				epoch: round + 1,
			},
			message: FollowingVotingMessage{
				FollowingVotingMessageBody: FollowingVotingMessageBody{
					EpochID:        epoch,
					RoundID:        round,
					VotesBitVector: []uint64{0b101},
				},
			},
			expected: map[proposal]*big.Int{
				string(hash1.Bytes()): big.NewInt(1),
				string(hash2.Bytes()): big.NewInt(-1),
				string(hash3.Bytes()): big.NewInt(1),
			},
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				config: Config{
					RoundsNumber: round + 1,
				},
				Log:         logtest.New(t).WithName("TortoiseBeacon"),
				atxDB:       mockDB,
				vrfVerifier: signing.VRFVerifier{},
				vrfSigner:   vrfSigner,
				edSigner:    edSgn,
				clock:       clock,
				firstRoundIncomingVotes: map[nodeID]proposals{
					minerID.Key: {
						valid:            [][]byte{hash1.Bytes(), hash2.Bytes()},
						potentiallyValid: [][]byte{hash3.Bytes()},
					},
				},
				lastLayer:   types.NewLayerID(epoch),
				hasVoted:    make([]map[nodeID]struct{}, round+1),
				votesMargin: map[proposal]*big.Int{},
			}

			sig, err := tb.signMessage(tc.message.FollowingVotingMessageBody)
			r.NoError(err)
			tc.message.Signature = sig

			err = tb.handleFollowingVotingMessage(tc.message)
			r.NoError(err)

			r.EqualValues(tc.expected, tb.votesMargin)
		})
	}
}
