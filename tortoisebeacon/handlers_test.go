package tortoisebeacon

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spacemeshos/go-spacemesh/tortoisebeacon/mocks"
	"github.com/stretchr/testify/require"
)

func clockNeverNotify(t *testing.T) layerClock {
	return timesync.NewClock(timesync.RealClock{}, time.Hour, time.Now(), logtest.New(t).WithName("clock"))
}

func TestTortoiseBeacon_handleProposalMessage(t *testing.T) {
	t.Parallel()

	r := require.New(t)
	const epoch = types.EpochID(3)
	types.SetLayersPerEpoch(1)

	edSgn := signing.NewEdSigner()
	edPubkey := edSgn.PublicKey()
	vrfSigner, _, err := signing.NewVRFSigner(edSgn.Sign(edPubkey.Bytes()))
	r.NoError(err)

	nodeID := types.NodeID{
		Key:          edPubkey.String(),
		VRFPublicKey: vrfSigner.PublicKey().Bytes(),
	}

	ctrl := gomock.NewController(t)
	mockChecker := mocks.NewMockeligibilityChecker(ctrl)
	mockChecker.EXPECT().IsProposalEligible(gomock.Any()).Return(true).Times(1)
	mockChecker.EXPECT().IsProposalEligible(gomock.Any()).Return(false).Times(1)

	mockDB := mocks.NewMockactivationDB(ctrl)
	mockDB.EXPECT().GetEpochWeight(epoch).Return(uint64(10), nil, nil).Times(1)
	mockDB.EXPECT().GetNodeAtxIDForEpoch(
		nodeID,
		epoch-1).
		Return(types.ATXID(types.HexToHash32("0x12345678")), nil).Times(3)
	mockDB.EXPECT().GetAtxHeader(gomock.Any()).Return(&types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			StartTick: 1,
			EndTick:   3,
		},
		NumUnits: 5,
	}, nil).Times(2)
	mockDB.EXPECT().GetAtxTimestamp(gomock.Any()).Return(time.Now(), nil).Times(2)

	sig := buildSignedProposal(context.TODO(), vrfSigner, epoch, logtest.New(t))
	tt := []struct {
		name        string
		message     ProposalMessage
		eligible    bool
		expectedErr error
	}{
		{
			name: "bad signature",
			message: ProposalMessage{
				NodeID:       nodeID,
				EpochID:      epoch,
				VRFSignature: sig[1:], // missing the first byte
			},
			eligible:    false,
			expectedErr: ErrMalformedProposal,
		},
		{
			name: "proposal eligible",
			message: ProposalMessage{
				NodeID:       nodeID,
				EpochID:      epoch,
				VRFSignature: sig,
			},
			eligible:    true,
			expectedErr: nil,
		},
		{
			name: "proposal not eligible",
			message: ProposalMessage{
				NodeID:       nodeID,
				EpochID:      epoch,
				VRFSignature: sig,
			},
			eligible:    false,
			expectedErr: nil,
		},
	}
	for i, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// don't run these tests in parallel. somehow all cases end up using the same TortoiseBeacon instance
			// t.Parallel()

			tb := TortoiseBeacon{
				config:          UnitTestConfig(),
				logger:          logtest.New(t).WithName(fmt.Sprintf("TortoiseBeacon-%v", i)),
				nodeID:          nodeID,
				atxDB:           mockDB,
				vrfVerifier:     signing.VRFVerifier{},
				vrfSigner:       vrfSigner,
				clock:           clockNeverNotify(t),
				epochInProgress: epoch,
				proposalChecker: mockChecker,
			}

			err = tb.handleProposalMessage(context.TODO(), tc.message, time.Now())
			r.Equal(tc.expectedErr, err)

			var expectedProposals proposals
			if tc.eligible {
				expectedProposals.valid = [][]byte{sig}
			}

			tb.mu.RLock()
			r.EqualValues(expectedProposals, tb.incomingProposals, t.Name())
			tb.mu.RUnlock()
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

	ctrl := gomock.NewController(t)
	mockDB := mocks.NewMockactivationDB(ctrl)
	mockDB.EXPECT().GetEpochWeight(gomock.Any()).Return(uint64(10), nil, nil).AnyTimes()
	mockDB.EXPECT().GetNodeAtxIDForEpoch(
		gomock.Any(),
		gomock.Any()).
		Return(types.ATXID(hash), nil).AnyTimes()
	mockDB.EXPECT().GetAtxHeader(gomock.Any()).Return(&types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			StartTick: 1,
			EndTick:   3,
		},
		NumUnits: 5,
	}, nil).AnyTimes()
	mockDB.EXPECT().GetAtxTimestamp(gomock.Any()).Return(time.Now(), nil)

	const (
		round = 0
	)

	types.SetLayersPerEpoch(1)

	edSgn := signing.NewEdSigner()
	edPubkey := edSgn.PublicKey()

	vrfSigner, _, err := signing.NewVRFSigner(edSgn.Sign(edPubkey.Bytes()))
	r.NoError(err)

	msg := FirstVotingMessage{
		FirstVotingMessageBody: FirstVotingMessageBody{
			ValidProposals:            [][]byte{hash.Bytes()},
			PotentiallyValidProposals: nil,
		},
	}
	msg.Signature = signMessage(edSgn, msg.FirstVotingMessageBody, logtest.New(t))

	tt := []struct {
		name     string
		message  FirstVotingMessage
		expected map[string]proposals
	}{
		{
			name:    "Current round and message round equal",
			message: msg,
			expected: map[string]proposals{
				string(edSgn.PublicKey().Bytes()): {
					valid: [][]byte{hash.Bytes()},
				},
			},
		},
		{
			name:    "Current round and message round differ",
			message: msg,
			expected: map[string]proposals{
				string(edSgn.PublicKey().Bytes()): {
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
				logger:                  logtest.New(t).WithName("TortoiseBeacon"),
				atxDB:                   mockDB,
				vrfVerifier:             signing.VRFVerifier{},
				vrfSigner:               vrfSigner,
				edSigner:                edSgn,
				edVerifier:              signing.NewEDVerifier(),
				clock:                   clock,
				firstRoundIncomingVotes: map[string]proposals{},
				votesMargin:             map[string]*big.Int{},
				hasVoted:                make([]map[string]struct{}, round+1),
			}

			err = tb.handleFirstVotingMessage(context.TODO(), tc.message)
			r.NoError(err)

			tb.mu.RLock()
			r.EqualValues(tc.expected, tb.firstRoundIncomingVotes)
			tb.mu.RUnlock()
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
	ld := 10 * time.Second
	clock := timesync.NewClock(timesync.RealClock{}, ld, genesisTime, logtest.New(t).WithName("clock"))
	clock.StartNotifying()

	ctrl := gomock.NewController(t)
	mockDB := mocks.NewMockactivationDB(ctrl)
	mockDB.EXPECT().GetEpochWeight(gomock.Any()).Return(uint64(1), nil, nil).AnyTimes()
	mockDB.EXPECT().GetNodeAtxIDForEpoch(
		gomock.Any(),
		gomock.Any()).
		Return(types.ATXID(hash1), nil).AnyTimes()
	mockDB.EXPECT().GetAtxHeader(gomock.Any()).Return(&types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			StartTick: 0,
			EndTick:   1,
		},
		NumUnits: 1,
	}, nil).AnyTimes()
	mockDB.EXPECT().GetAtxTimestamp(gomock.Any()).Return(time.Now(), nil)

	const epoch = 3
	const round = 5

	types.SetLayersPerEpoch(1)

	edSgn := signing.NewEdSigner()
	edPubkey := edSgn.PublicKey()

	vrfSigner, _, err := signing.NewVRFSigner(edSgn.Sign(edPubkey.Bytes()))
	r.NoError(err)

	msg := FollowingVotingMessage{
		FollowingVotingMessageBody: FollowingVotingMessageBody{
			RoundID:        round,
			VotesBitVector: []uint64{0b101},
		},
	}
	msg.Signature = signMessage(edSgn, msg.FollowingVotingMessageBody, logtest.New(t))

	tt := []struct {
		name     string
		message  FollowingVotingMessage
		expected map[string]*big.Int
	}{
		{
			name:    "Current round and message round equal",
			message: msg,
			expected: map[string]*big.Int{
				string(hash1.Bytes()): big.NewInt(1),
				string(hash2.Bytes()): big.NewInt(-1),
				string(hash3.Bytes()): big.NewInt(1),
			},
		},
		{
			name:    "Current round and message round differ",
			message: msg,
			expected: map[string]*big.Int{
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
				logger:      logtest.New(t).WithName("TortoiseBeacon"),
				atxDB:       mockDB,
				vrfVerifier: signing.VRFVerifier{},
				vrfSigner:   vrfSigner,
				edSigner:    edSgn,
				edVerifier:  signing.NewEDVerifier(),
				clock:       clock,
				firstRoundIncomingVotes: map[string]proposals{
					string(edSgn.PublicKey().Bytes()): {
						valid:            [][]byte{hash1.Bytes(), hash2.Bytes()},
						potentiallyValid: [][]byte{hash3.Bytes()},
					},
				},
				epochInProgress: epoch,
				hasVoted:        make([]map[string]struct{}, round+1),
				votesMargin:     map[string]*big.Int{},
			}

			err = tb.handleFollowingVotingMessage(context.TODO(), tc.message)
			r.NoError(err)

			r.EqualValues(tc.expected, tb.votesMargin)
		})
	}
}
