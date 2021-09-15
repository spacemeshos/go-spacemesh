package tortoisebeacon

import (
	"context"
	"fmt"
	"math/big"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	p2pMocks "github.com/spacemeshos/go-spacemesh/p2p/service/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spacemeshos/go-spacemesh/tortoisebeacon/mocks"
)

func clockNeverNotify(t *testing.T) layerClock {
	return timesync.NewClock(timesync.RealClock{}, time.Hour, time.Now(), logtest.New(t).WithName("clock"))
}

func TestTortoiseBeacon_HandleSerializedProposalMessage(t *testing.T) {
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
	sig := buildSignedProposal(context.TODO(), vrfSigner, epoch, logtest.New(t))
	message := ProposalMessage{
		NodeID:       nodeID,
		EpochID:      epoch,
		VRFSignature: sig,
	}
	msgBytes, err := types.InterfaceToBytes(message)
	r.NoError(err)
	tt := []struct {
		name        string
		shutdown    bool
		reject      bool
		corruptData bool
		expected    int
	}{
		{
			name:     "shutdown",
			shutdown: true,
		},
		{
			name:        "mal formed proposal",
			corruptData: true,
		},
		{
			name:   "proposal rejected",
			reject: true,
		},
		{
			name:     "proposal accepted",
			expected: 1,
		},
	}
	for i, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// don't run these tests in parallel. somehow all cases end up using the same TortoiseBeacon instance
			// t.Parallel()

			tb := TortoiseBeacon{
				logger:          logtest.New(t).WithName(fmt.Sprintf("TortoiseBeacon-%v", i)),
				config:          UnitTestConfig(),
				proposalChans:   make(map[types.EpochID]chan *proposalMessageWithReceiptData),
				epochInProgress: epoch,
				running:         1,
			}
			if tc.shutdown {
				atomic.StoreUint64(&tb.running, 0)
			}
			if tc.reject {
				tb.epochInProgress = epoch + 2
			}
			ctrl := gomock.NewController(t)
			mockGossip := p2pMocks.NewMockGossipMessage(ctrl)
			mockGossip.EXPECT().Sender().Return(p2pcrypto.NewRandomPubkey()).AnyTimes()
			if tc.corruptData {
				mockGossip.EXPECT().Bytes().Return(msgBytes[1:]).Times(1)
			} else {
				mockGossip.EXPECT().Bytes().Return(msgBytes).Times(1)
			}

			tb.HandleSerializedProposalMessage(context.TODO(), mockGossip, nil)
			tb.mu.RLock()
			ch := tb.getProposalChannel(context.TODO(), epoch)
			assert.Equal(t, tc.expected, len(ch))
			tb.mu.RUnlock()
		})
	}
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

func TestTortoiseBeacon_HandleSerializedFirstVotingMessage(t *testing.T) {
	t.Parallel()

	r := require.New(t)
	const epoch = types.EpochID(10)
	types.SetLayersPerEpoch(1)
	hash := types.HexToHash32("0x12345678")

	edSgn := signing.NewEdSigner()
	edPubkey := edSgn.PublicKey()

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

	msg := FirstVotingMessage{
		FirstVotingMessageBody: FirstVotingMessageBody{
			EpochID:                   epoch,
			ValidProposals:            [][]byte{hash.Bytes()},
			PotentiallyValidProposals: nil,
		},
	}
	msg.Signature = signMessage(edSgn, msg.FirstVotingMessageBody, logtest.New(t))
	msgBytes, err := types.InterfaceToBytes(msg)
	r.NoError(err)

	tt := []struct {
		name            string
		shutdown        bool
		epochInProgress types.EpochID
		corruptData     bool
		expectedVoted   bool
	}{
		{
			name:          "shutdown",
			shutdown:      true,
			expectedVoted: false,
		},
		{
			name:          "mal formed first votes",
			corruptData:   true,
			expectedVoted: false,
		},
		{
			name:            "different epoch",
			epochInProgress: epoch + 1,
			expectedVoted:   false,
		},
		{
			name:            "vote accepted",
			epochInProgress: epoch,
			expectedVoted:   true,
		},
	}
	for i, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// don't run these tests in parallel. somehow all cases end up using the same TortoiseBeacon instance
			// t.Parallel()

			config := UnitTestConfig()
			tb := TortoiseBeacon{
				logger:                  logtest.New(t).WithName(fmt.Sprintf("TortoiseBeacon-%v", i)),
				config:                  config,
				atxDB:                   mockDB,
				edSigner:                edSgn,
				edVerifier:              signing.NewEDVerifier(),
				firstRoundIncomingVotes: map[string]proposals{},
				votesMargin:             map[string]*big.Int{},
				hasVoted:                make([]map[string]struct{}, 1),
				epochInProgress:         tc.epochInProgress,
				running:                 1,
			}
			if tc.shutdown {
				atomic.StoreUint64(&tb.running, 0)
			}
			ctrl := gomock.NewController(t)
			mockGossip := p2pMocks.NewMockGossipMessage(ctrl)
			pubKey := p2pcrypto.NewRandomPubkey()
			mockGossip.EXPECT().Sender().Return(pubKey).AnyTimes()

			if tc.corruptData {
				mockGossip.EXPECT().Bytes().Return(msgBytes[1:]).Times(2)
			} else {
				mockGossip.EXPECT().Bytes().Return(msgBytes).Times(2)
			}

			if tc.expectedVoted {
				mockGossip.EXPECT().ReportValidation(gomock.Any(), TBFirstVotingProtocol).Times(1)
			}
			tb.HandleSerializedFirstVotingMessage(context.TODO(), mockGossip, nil)
			tb.mu.RLock()
			_, ok := tb.hasVoted[0][string(edPubkey.Bytes())]
			assert.Equal(t, tc.expectedVoted, ok)
			tb.mu.RUnlock()
		})
	}
}

func TestTortoiseBeacon_handleFirstVotingMessage(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	hash := types.HexToHash32("0x12345678")

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
	msg := FirstVotingMessage{
		FirstVotingMessageBody: FirstVotingMessageBody{
			EpochID:                   3,
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
				edSigner:                edSgn,
				edVerifier:              signing.NewEDVerifier(),
				firstRoundIncomingVotes: map[string]proposals{},
				votesMargin:             map[string]*big.Int{},
				hasVoted:                make([]map[string]struct{}, round+1),
			}

			err := tb.handleFirstVotingMessage(context.TODO(), tc.message)
			r.NoError(err)

			tb.mu.RLock()
			r.EqualValues(tc.expected, tb.firstRoundIncomingVotes)
			tb.mu.RUnlock()
		})
	}
}

func TestTortoiseBeacon_HandleSerializedFollowingVotingMessage(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	const epoch = types.EpochID(10)
	const round = types.RoundID(3)
	types.SetLayersPerEpoch(1)

	hash := types.HexToHash32("0x12345678")

	edSgn := signing.NewEdSigner()
	edPubkey := edSgn.PublicKey()

	ctrl := gomock.NewController(t)
	mockDB := mocks.NewMockactivationDB(ctrl)
	mockDB.EXPECT().GetEpochWeight(gomock.Any()).Return(uint64(1), nil, nil).AnyTimes()
	mockDB.EXPECT().GetNodeAtxIDForEpoch(
		gomock.Any(),
		gomock.Any()).
		Return(types.ATXID(hash), nil).AnyTimes()
	mockDB.EXPECT().GetAtxHeader(gomock.Any()).Return(&types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			StartTick: 0,
			EndTick:   1,
		},
		NumUnits: 1,
	}, nil).AnyTimes()
	mockDB.EXPECT().GetAtxTimestamp(gomock.Any()).Return(time.Now(), nil)

	msg := FollowingVotingMessage{
		FollowingVotingMessageBody: FollowingVotingMessageBody{
			EpochID:        epoch,
			RoundID:        round,
			VotesBitVector: []uint64{0b101},
		},
	}
	msg.Signature = signMessage(edSgn, msg.FollowingVotingMessageBody, logtest.New(t))
	msgBytes, err := types.InterfaceToBytes(msg)
	r.NoError(err)

	tt := []struct {
		name            string
		shutdown        bool
		epochInProgress types.EpochID
		corruptData     bool
		expectedVoted   bool
	}{
		{
			name:          "shutdown",
			shutdown:      true,
			expectedVoted: false,
		},
		{
			name:          "mal formed following votes",
			corruptData:   true,
			expectedVoted: false,
		},
		{
			name:            "different epoch",
			epochInProgress: epoch + 1,
			expectedVoted:   false,
		},
		{
			name:            "vote accepted",
			epochInProgress: epoch,
			expectedVoted:   true,
		},
	}
	for i, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// don't run these tests in parallel. somehow all cases end up using the same TortoiseBeacon instance
			// t.Parallel()

			config := UnitTestConfig()
			tb := TortoiseBeacon{
				logger:          logtest.New(t).WithName(fmt.Sprintf("TortoiseBeacon-%v", i)),
				config:          config,
				atxDB:           mockDB,
				edSigner:        edSgn,
				edVerifier:      signing.NewEDVerifier(),
				votesMargin:     map[string]*big.Int{},
				hasVoted:        make([]map[string]struct{}, round+1),
				epochInProgress: tc.epochInProgress,
				running:         1,
			}
			if tc.shutdown {
				atomic.StoreUint64(&tb.running, 0)
			}
			ctrl := gomock.NewController(t)
			mockGossip := p2pMocks.NewMockGossipMessage(ctrl)
			pubKey := p2pcrypto.NewRandomPubkey()
			mockGossip.EXPECT().Sender().Return(pubKey).AnyTimes()

			if tc.corruptData {
				mockGossip.EXPECT().Bytes().Return(msgBytes[1:]).Times(2)
			} else {
				mockGossip.EXPECT().Bytes().Return(msgBytes).Times(2)
			}

			if tc.expectedVoted {
				mockGossip.EXPECT().ReportValidation(gomock.Any(), TBFollowingVotingProtocol).Times(1)
			}
			tb.HandleSerializedFollowingVotingMessage(context.TODO(), mockGossip, nil)
			tb.mu.RLock()
			_, ok := tb.hasVoted[round][string(edPubkey.Bytes())]
			assert.Equal(t, tc.expectedVoted, ok)
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
	msg := FollowingVotingMessage{
		FollowingVotingMessageBody: FollowingVotingMessageBody{
			EpochID:        epoch,
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
				logger:     logtest.New(t).WithName("TortoiseBeacon"),
				atxDB:      mockDB,
				edSigner:   edSgn,
				edVerifier: signing.NewEDVerifier(),
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

			err := tb.handleFollowingVotingMessage(context.TODO(), tc.message)
			r.NoError(err)

			r.EqualValues(tc.expected, tb.votesMargin)
		})
	}
}

func TestTortoiseBeacon_uniqueFollowingVotingMessages(t *testing.T) {
	round := types.RoundID(3)
	votesBitVector := []uint64{0b101}
	edSgn := signing.NewEdSigner()
	msg1 := FollowingVotingMessage{
		FollowingVotingMessageBody: FollowingVotingMessageBody{
			RoundID:        round,
			VotesBitVector: votesBitVector,
		},
	}
	msg1.Signature = signMessage(edSgn, msg1.FollowingVotingMessageBody, logtest.New(t))
	data1, err := types.InterfaceToBytes(msg1)
	require.NoError(t, err)

	msg2 := FollowingVotingMessage{
		FollowingVotingMessageBody: FollowingVotingMessageBody{
			RoundID:        round,
			VotesBitVector: votesBitVector,
		},
	}
	msg2.Signature = signMessage(edSgn, msg2.FollowingVotingMessageBody, logtest.New(t))
	data2, err := types.InterfaceToBytes(msg2)
	require.NoError(t, err)

	// without EpochID, we cannot tell the following messages apart
	assert.Equal(t, data1, data2)

	msg1.EpochID = types.EpochID(5)
	msg1.Signature = signMessage(edSgn, msg1.FollowingVotingMessageBody, logtest.New(t))
	data1, err = types.InterfaceToBytes(msg1)
	require.NoError(t, err)

	msg2.EpochID = msg1.EpochID + 1
	msg2.Signature = signMessage(edSgn, msg2.FollowingVotingMessageBody, logtest.New(t))
	data2, err = types.InterfaceToBytes(msg2)
	require.NoError(t, err)

	// with EpochID, voting messages from the same miner with the same bit vector will
	// not be considered duplicate gossip messages.
	assert.NotEqual(t, data1, data2)
}
