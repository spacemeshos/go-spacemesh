package tortoisebeacon

import (
	"context"
	"errors"
	"math/big"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	p2pMocks "github.com/spacemeshos/go-spacemesh/p2p/service/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spacemeshos/go-spacemesh/tortoisebeacon/mocks"
)

var errUnknown = errors.New("unknown")

func clockNeverNotify(t *testing.T) layerClock {
	return timesync.NewClock(timesync.RealClock{}, time.Hour, time.Now(), logtest.New(t).WithName("clock"))
}

func createTortoiseBeacon(t *testing.T, epoch types.EpochID) (*TortoiseBeacon, *mocks.MockactivationDB) {
	types.SetLayersPerEpoch(3)
	ctrl := gomock.NewController(t)
	mockDB := mocks.NewMockactivationDB(ctrl)
	edSgn := signing.NewEdSigner()
	tb := &TortoiseBeacon{
		logger:                  logtest.New(t).WithName("TortoiseBeacon"),
		clock:                   clockNeverNotify(t),
		config:                  UnitTestConfig(),
		atxDB:                   mockDB,
		edSigner:                edSgn,
		edVerifier:              signing.NewEDVerifier(),
		vrfVerifier:             signing.VRFVerifier{},
		proposalChans:           make(map[types.EpochID]chan *proposalMessageWithReceiptData),
		firstRoundIncomingVotes: map[string]proposals{},
		votesMargin:             map[string]*big.Int{},
		hasProposed:             make(map[string]struct{}),
		hasVoted:                make([]map[string]struct{}, 10),
		epochInProgress:         epoch,
		running:                 1,
		inProtocol:              1,
	}
	return tb, mockDB
}

func createTortoiseBeaconWithFirstRoundVotes(t *testing.T, epoch types.EpochID, signer signing.Signer) (*TortoiseBeacon, *mocks.MockactivationDB, []types.Hash32) {
	tb, mockDB := createTortoiseBeacon(t, epoch)
	hash1 := types.HexToHash32("0x12345678")
	hash2 := types.HexToHash32("0x23456789")
	hash3 := types.HexToHash32("0x34567890")
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.firstRoundIncomingVotes = map[string]proposals{
		string(signer.PublicKey().Bytes()): {
			valid:            [][]byte{hash1.Bytes(), hash2.Bytes()},
			potentiallyValid: [][]byte{hash3.Bytes()},
		}}
	return tb, mockDB, []types.Hash32{hash1, hash2, hash3}
}

func setMockAtxDbExpectations(mockDB *mocks.MockactivationDB) {
	mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), gomock.Any()).
		Return(types.ATXID(types.HexToHash32("0x22345678")), nil).Times(1)
	mockDB.EXPECT().GetAtxHeader(gomock.Any()).Return(&types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			StartTick: 1,
			EndTick:   3,
		},
		NumUnits: 5,
	}, nil).Times(1)
	mockDB.EXPECT().GetAtxTimestamp(gomock.Any()).Return(time.Now().Add(-1*time.Second), nil).Times(1)
}

func createProposalGossip(t *testing.T, signer signing.Signer, epoch types.EpochID, corruptData bool) *p2pMocks.MockGossipMessage {
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)
	message := createProposal(t, signer, vrfSigner, epoch, false)
	msgBytes, err := types.InterfaceToBytes(message)
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	mockGossip := p2pMocks.NewMockGossipMessage(ctrl)
	pubKey := p2pcrypto.NewRandomPubkey()
	mockGossip.EXPECT().Sender().Return(pubKey).AnyTimes()
	if corruptData {
		mockGossip.EXPECT().Bytes().Return(msgBytes[1:]).Times(2)
	} else {
		mockGossip.EXPECT().Bytes().Return(msgBytes).Times(2)
	}
	return mockGossip
}

func createProposal(t *testing.T, signer, vrfSigner signing.Signer, epoch types.EpochID, corruptSignature bool) *ProposalMessage {
	nodeID := types.NodeID{
		Key:          signer.PublicKey().String(),
		VRFPublicKey: vrfSigner.PublicKey().Bytes(),
	}
	sig := buildSignedProposal(context.TODO(), vrfSigner, epoch, logtest.New(t))
	msg := &ProposalMessage{
		NodeID:       nodeID,
		EpochID:      epoch,
		VRFSignature: sig,
	}
	if corruptSignature {
		msg.VRFSignature = sig[1:]
	}
	return msg
}

func checkProposalQueueSize(t *testing.T, tb *TortoiseBeacon, epoch types.EpochID, numProposals int) {
	tb.mu.RLock()
	defer tb.mu.RUnlock()
	ch := tb.getOrCreateProposalChannel(epoch)
	assert.Equal(t, numProposals, len(ch))
}

func checkProposed(t *testing.T, tb *TortoiseBeacon, signer signing.Signer, proposed bool) {
	tb.mu.RLock()
	defer tb.mu.RUnlock()
	_, ok := tb.hasProposed[string(signer.PublicKey().Bytes())]
	assert.Equal(t, proposed, ok)
}

func checkProposals(t *testing.T, tb *TortoiseBeacon, expected proposals) {
	tb.mu.RLock()
	defer tb.mu.RUnlock()
	assert.EqualValues(t, expected, tb.incomingProposals)
}

func createFirstVoteGossip(t *testing.T, signer signing.Signer, epoch types.EpochID, valid, pValid [][]byte, corruptData bool) *p2pMocks.MockGossipMessage {
	msg := createFirstVote(t, signer, epoch, valid, pValid, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	mockGossip := p2pMocks.NewMockGossipMessage(ctrl)
	pubKey := p2pcrypto.NewRandomPubkey()
	mockGossip.EXPECT().Sender().Return(pubKey).AnyTimes()
	if corruptData {
		mockGossip.EXPECT().Bytes().Return(msgBytes[1:]).Times(2)
	} else {
		mockGossip.EXPECT().Bytes().Return(msgBytes).Times(2)
	}
	return mockGossip
}

func createFirstVote(t *testing.T, signer signing.Signer, epoch types.EpochID, valid, pValid [][]byte, corruptSignature bool) *FirstVotingMessage {
	msg := &FirstVotingMessage{
		FirstVotingMessageBody: FirstVotingMessageBody{
			EpochID:                   epoch,
			ValidProposals:            valid,
			PotentiallyValidProposals: pValid,
		},
	}
	sig := signMessage(signer, msg.FirstVotingMessageBody, logtest.New(t))
	if corruptSignature {
		msg.Signature = sig[1:]
	} else {
		msg.Signature = sig
	}
	return msg
}

func checkVoted(t *testing.T, tb *TortoiseBeacon, signer signing.Signer, round types.RoundID, voted bool) {
	tb.mu.RLock()
	defer tb.mu.RUnlock()
	_, ok := tb.hasVoted[round][string(signer.PublicKey().Bytes())]
	assert.Equal(t, voted, ok)
}

func checkFirstIncomingVotes(t *testing.T, tb *TortoiseBeacon, expected map[string]proposals) {
	tb.mu.RLock()
	defer tb.mu.RUnlock()
	assert.EqualValues(t, expected, tb.firstRoundIncomingVotes)
}

func createFollowingVoteGossip(t *testing.T, signer signing.Signer, epoch types.EpochID, round types.RoundID, bitVector []uint64, corruptData bool) *p2pMocks.MockGossipMessage {
	msg := createFollowingVote(t, signer, epoch, round, bitVector, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	ctrl := gomock.NewController(t)
	mockGossip := p2pMocks.NewMockGossipMessage(ctrl)
	pubKey := p2pcrypto.NewRandomPubkey()
	mockGossip.EXPECT().Sender().Return(pubKey).AnyTimes()
	if corruptData {
		mockGossip.EXPECT().Bytes().Return(msgBytes[1:]).Times(2)
	} else {
		mockGossip.EXPECT().Bytes().Return(msgBytes).Times(2)
	}
	return mockGossip
}

func createFollowingVote(t *testing.T, signer signing.Signer, epoch types.EpochID, round types.RoundID, bitVector []uint64, corruptSignature bool) *FollowingVotingMessage {
	msg := &FollowingVotingMessage{
		FollowingVotingMessageBody: FollowingVotingMessageBody{
			EpochID:        epoch,
			RoundID:        round,
			VotesBitVector: bitVector,
		},
	}
	sig := signMessage(signer, msg.FollowingVotingMessageBody, logtest.New(t))
	if corruptSignature {
		msg.Signature = sig[1:]
	} else {
		msg.Signature = sig
	}
	return msg
}

func checkVoteMargins(t *testing.T, tb *TortoiseBeacon, expected map[string]*big.Int) {
	tb.mu.RLock()
	defer tb.mu.RUnlock()
	assert.EqualValues(t, expected, tb.votesMargin)
}

func TestTB_HandleSerializedProposalMessage_Success(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)

	tb, _ := createTortoiseBeacon(t, epoch)
	signer := signing.NewEdSigner()
	mockGossip := createProposalGossip(t, signer, epoch, false)

	tb.HandleSerializedProposalMessage(context.TODO(), mockGossip, nil)
	checkProposalQueueSize(t, tb, epoch, 1)
}

func TestTB_HandleSerializedProposalMessage_Shutdown(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tb, _ := createTortoiseBeacon(t, epoch)
	signer := signing.NewEdSigner()
	mockGossip := createProposalGossip(t, signer, epoch, false)

	atomic.StoreUint64(&tb.running, 0)
	tb.HandleSerializedProposalMessage(context.TODO(), mockGossip, nil)
	checkProposalQueueSize(t, tb, epoch, 0)
}

func TestTB_HandleSerializedProposalMessage_NotInProtocol(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tb, _ := createTortoiseBeacon(t, epoch)
	signer := signing.NewEdSigner()
	mockGossip := createProposalGossip(t, signer, epoch, false)

	atomic.StoreUint64(&tb.inProtocol, 0)
	tb.HandleSerializedProposalMessage(context.TODO(), mockGossip, nil)
	checkProposalQueueSize(t, tb, epoch, 0)
}

func TestTB_HandleSerializedProposalMessage_Corrupted(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tb, _ := createTortoiseBeacon(t, epoch)
	signer := signing.NewEdSigner()
	mockGossip := createProposalGossip(t, signer, epoch, true)

	tb.HandleSerializedProposalMessage(context.TODO(), mockGossip, nil)
	checkProposalQueueSize(t, tb, epoch, 0)
}

func TestTB_HandleSerializedProposalMessage_EpochTooOld(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tb, _ := createTortoiseBeacon(t, epoch)
	signer := signing.NewEdSigner()
	mockGossip := createProposalGossip(t, signer, epoch-1, false)

	tb.HandleSerializedProposalMessage(context.TODO(), mockGossip, nil)
	checkProposalQueueSize(t, tb, epoch-1, 0)
	checkProposalQueueSize(t, tb, epoch, 0)
}

func TestTB_HandleSerializedProposalMessage_NextEpoch(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tb, _ := createTortoiseBeacon(t, epoch)
	signer := signing.NewEdSigner()
	mockGossip := createProposalGossip(t, signer, epoch+1, false)

	tb.HandleSerializedProposalMessage(context.TODO(), mockGossip, nil)
	checkProposalQueueSize(t, tb, epoch, 0)
	checkProposalQueueSize(t, tb, epoch+1, 1)
}

func TestTB_HandleSerializedProposalMessage_NextEpochFull(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tb, _ := createTortoiseBeacon(t, epoch)
	signer := signing.NewEdSigner()
	for i := 0; i < proposalChanCapacity; i++ {
		mockGossip := createProposalGossip(t, signer, epoch+1, false)
		tb.HandleSerializedProposalMessage(context.TODO(), mockGossip, nil)
	}
	checkProposalQueueSize(t, tb, epoch, 0)
	checkProposalQueueSize(t, tb, epoch+1, proposalChanCapacity)

	// now try to overflow channel for the next epoch
	mockGossip := createProposalGossip(t, signer, epoch+1, false)
	tb.HandleSerializedProposalMessage(context.TODO(), mockGossip, nil)
	checkProposalQueueSize(t, tb, epoch, 0)
	checkProposalQueueSize(t, tb, epoch+1, proposalChanCapacity)
}

func TestTB_HandleSerializedProposalMessage_EpochTooFarAhead(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tb, _ := createTortoiseBeacon(t, epoch)
	signer := signing.NewEdSigner()
	mockGossip := createProposalGossip(t, signer, epoch+2, false)

	tb.HandleSerializedProposalMessage(context.TODO(), mockGossip, nil)
	checkProposalQueueSize(t, tb, epoch, 0)
	checkProposalQueueSize(t, tb, epoch+2, 0)
}

func TestTB_handleProposalMessage_Success(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tb, mockDB := createTortoiseBeacon(t, epoch)
	setMockAtxDbExpectations(mockDB)
	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)
	msg := createProposal(t, signer, vrfSigner, epoch, false)

	ctrl := gomock.NewController(t)
	mockChecker := mocks.NewMockeligibilityChecker(ctrl)
	mockChecker.EXPECT().IsProposalEligible(gomock.Any()).Return(true).Times(1)
	tb.proposalChecker = mockChecker

	err = tb.handleProposalMessage(context.TODO(), *msg, time.Now())
	assert.NoError(t, err)

	checkProposed(t, tb, vrfSigner, true)
	var expectedProposals proposals
	expectedProposals.valid = [][]byte{msg.VRFSignature}
	checkProposals(t, tb, expectedProposals)
}

func TestTB_handleProposalMessage_BadSignature(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tb, mockDB := createTortoiseBeacon(t, epoch)
	setMockAtxDbExpectations(mockDB)
	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)
	msg := createProposal(t, signer, vrfSigner, epoch, true)

	ctrl := gomock.NewController(t)
	mockChecker := mocks.NewMockeligibilityChecker(ctrl)
	mockChecker.EXPECT().IsProposalEligible(gomock.Any()).Return(true).Times(1)
	tb.proposalChecker = mockChecker

	err = tb.handleProposalMessage(context.TODO(), *msg, time.Now())
	assert.ErrorIs(t, err, errVRFNotVerified)

	checkProposed(t, tb, vrfSigner, false)
	checkProposals(t, tb, proposals{})
}

func TestTB_handleProposalMessage_AlreadyProposed(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tb, mockDB := createTortoiseBeacon(t, epoch)
	setMockAtxDbExpectations(mockDB)
	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)
	msg := createProposal(t, signer, vrfSigner, epoch, false)

	ctrl := gomock.NewController(t)
	mockChecker := mocks.NewMockeligibilityChecker(ctrl)
	mockChecker.EXPECT().IsProposalEligible(gomock.Any()).Return(true).Times(1)
	tb.proposalChecker = mockChecker

	err = tb.handleProposalMessage(context.TODO(), *msg, time.Now())
	assert.NoError(t, err)

	checkProposed(t, tb, vrfSigner, true)
	var expectedProposals proposals
	expectedProposals.valid = [][]byte{msg.VRFSignature}
	checkProposals(t, tb, expectedProposals)

	// now make the same proposal again
	setMockAtxDbExpectations(mockDB)
	err = tb.handleProposalMessage(context.TODO(), *msg, time.Now())
	assert.ErrorIs(t, err, errAlreadyProposed)
	checkProposed(t, tb, vrfSigner, true)
	checkProposals(t, tb, expectedProposals)
}

func TestTB_handleProposalMessage_ProposalNotEligible(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tb, mockDB := createTortoiseBeacon(t, epoch)
	setMockAtxDbExpectations(mockDB)
	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)
	msg := createProposal(t, signer, vrfSigner, epoch, false)

	ctrl := gomock.NewController(t)
	mockChecker := mocks.NewMockeligibilityChecker(ctrl)
	mockChecker.EXPECT().IsProposalEligible(gomock.Any()).Return(false).Times(1)
	tb.proposalChecker = mockChecker

	err = tb.handleProposalMessage(context.TODO(), *msg, time.Now())
	assert.ErrorIs(t, err, errProposalDoesntPassThreshold)

	checkProposed(t, tb, vrfSigner, true)
	checkProposals(t, tb, proposals{})
}

func TestTB_handleProposalMessage_MinerMissingATX(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tb, mockDB := createTortoiseBeacon(t, epoch)
	mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), gomock.Any()).
		Return(types.ATXID{}, database.ErrNotFound).Times(1)

	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)
	msg := createProposal(t, signer, vrfSigner, epoch, false)

	err = tb.handleProposalMessage(context.TODO(), *msg, time.Now())
	assert.ErrorIs(t, err, errMinerATXNotFound)

	checkProposed(t, tb, vrfSigner, false)
	checkProposals(t, tb, proposals{})
}

func TestTB_handleProposalMessage_ATXLookupError(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tb, mockDB := createTortoiseBeacon(t, epoch)
	mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), gomock.Any()).
		Return(types.ATXID{}, errUnknown).Times(1)

	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)
	msg := createProposal(t, signer, vrfSigner, epoch, false)

	err = tb.handleProposalMessage(context.TODO(), *msg, time.Now())
	assert.ErrorIs(t, err, errUnknown)

	checkProposed(t, tb, vrfSigner, false)
	checkProposals(t, tb, proposals{})
}

func TestTB_handleProposalMessage_ATXHeaderLookupError(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tb, mockDB := createTortoiseBeacon(t, epoch)
	mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), gomock.Any()).
		Return(types.ATXID(types.HexToHash32("0x22345678")), nil).Times(1)
	mockDB.EXPECT().GetAtxHeader(gomock.Any()).Return(nil, errUnknown).Times(1)

	ctrl := gomock.NewController(t)
	mockChecker := mocks.NewMockeligibilityChecker(ctrl)
	mockChecker.EXPECT().IsProposalEligible(gomock.Any()).Return(true).Times(1)
	tb.proposalChecker = mockChecker

	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)
	msg := createProposal(t, signer, vrfSigner, epoch, false)

	err = tb.handleProposalMessage(context.TODO(), *msg, time.Now())
	assert.ErrorIs(t, err, errUnknown)

	checkProposed(t, tb, vrfSigner, true)
	checkProposals(t, tb, proposals{})
}

func TestTB_HandleSerializedFirstVotingMessage_Success(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	valid := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValid := [][]byte{types.HexToHash32("0x23456789").Bytes()}

	tb, mockDB := createTortoiseBeacon(t, epoch)
	setMockAtxDbExpectations(mockDB)

	signer := signing.NewEdSigner()
	mockGossip := createFirstVoteGossip(t, signer, epoch, valid, pValid, false)
	mockGossip.EXPECT().ReportValidation(gomock.Any(), TBFirstVotingProtocol).Times(1)

	tb.HandleSerializedFirstVotingMessage(context.TODO(), mockGossip, nil)
	checkVoted(t, tb, signer, types.FirstRound, true)
	expected := map[string]proposals{
		string(signer.PublicKey().Bytes()): {valid: valid, potentiallyValid: pValid},
	}
	checkFirstIncomingVotes(t, tb, expected)
}

func TestTB_HandleSerializedFirstVotingMessage_Shutdown(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	valid := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValid := [][]byte{types.HexToHash32("0x23456789").Bytes()}

	tb, _ := createTortoiseBeacon(t, epoch)
	atomic.StoreUint64(&tb.running, 0)

	signer := signing.NewEdSigner()
	mockGossip := createFirstVoteGossip(t, signer, epoch, valid, pValid, false)

	tb.HandleSerializedFirstVotingMessage(context.TODO(), mockGossip, nil)
	checkVoted(t, tb, signer, types.FirstRound, false)
	checkFirstIncomingVotes(t, tb, map[string]proposals{})
}

func TestTB_HandleSerializedFirstVotingMessage_NotInProtocol(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	valid := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValid := [][]byte{types.HexToHash32("0x23456789").Bytes()}

	tb, _ := createTortoiseBeacon(t, epoch)
	atomic.StoreUint64(&tb.inProtocol, 0)

	signer := signing.NewEdSigner()
	mockGossip := createFirstVoteGossip(t, signer, epoch, valid, pValid, false)

	tb.HandleSerializedFirstVotingMessage(context.TODO(), mockGossip, nil)
	checkVoted(t, tb, signer, types.FirstRound, false)
	checkFirstIncomingVotes(t, tb, map[string]proposals{})
}

func TestTB_HandleSerializedFirstVotingMessage_CorruptedGossipMsg(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	valid := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValid := [][]byte{types.HexToHash32("0x23456789").Bytes()}

	tb, _ := createTortoiseBeacon(t, epoch)
	signer := signing.NewEdSigner()
	mockGossip := createFirstVoteGossip(t, signer, epoch, valid, pValid, true)

	tb.HandleSerializedFirstVotingMessage(context.TODO(), mockGossip, nil)
	checkVoted(t, tb, signer, types.FirstRound, false)
	checkFirstIncomingVotes(t, tb, map[string]proposals{})
}

func TestTB_HandleSerializedFirstVotingMessage_WrongEpoch(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	valid := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValid := [][]byte{types.HexToHash32("0x23456789").Bytes()}

	tb, _ := createTortoiseBeacon(t, epoch+1)
	signer := signing.NewEdSigner()
	mockGossip := createFirstVoteGossip(t, signer, epoch, valid, pValid, false)

	tb.HandleSerializedFirstVotingMessage(context.TODO(), mockGossip, nil)
	checkVoted(t, tb, signer, types.FirstRound, false)
	checkFirstIncomingVotes(t, tb, map[string]proposals{})
}

func TestTB_handleFirstVotingMessage_Success(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	valid := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValid := [][]byte{types.HexToHash32("0x23456789").Bytes()}

	tb, mockDB := createTortoiseBeacon(t, epoch)
	setMockAtxDbExpectations(mockDB)
	signer := signing.NewEdSigner()
	msg := createFirstVote(t, signer, epoch, valid, pValid, false)

	err := tb.handleFirstVotingMessage(context.TODO(), *msg)
	assert.NoError(t, err)

	checkVoted(t, tb, signer, types.FirstRound, true)
	expected := map[string]proposals{
		string(signer.PublicKey().Bytes()): {valid: valid, potentiallyValid: pValid}}
	checkFirstIncomingVotes(t, tb, expected)
}

func TestTB_handleFirstVotingMessage_FailedToExtractPK(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	valid := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValid := [][]byte{types.HexToHash32("0x23456789").Bytes()}

	tb, _ := createTortoiseBeacon(t, epoch)
	signer := signing.NewEdSigner()
	msg := createFirstVote(t, signer, epoch, valid, pValid, true)

	err := tb.handleFirstVotingMessage(context.TODO(), *msg)
	assert.Contains(t, err.Error(), "bad signature format")

	checkVoted(t, tb, signer, types.FirstRound, false)
	checkFirstIncomingVotes(t, tb, map[string]proposals{})
}

func TestTB_handleFirstVotingMessage_AlreadyVoted(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	valid := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValid := [][]byte{types.HexToHash32("0x23456789").Bytes()}

	tb, mockDB := createTortoiseBeacon(t, epoch)
	setMockAtxDbExpectations(mockDB)
	signer := signing.NewEdSigner()
	msg := createFirstVote(t, signer, epoch, valid, pValid, false)

	err := tb.handleFirstVotingMessage(context.TODO(), *msg)
	assert.NoError(t, err)

	checkVoted(t, tb, signer, types.FirstRound, true)
	expected := map[string]proposals{
		string(signer.PublicKey().Bytes()): {valid: valid, potentiallyValid: pValid}}
	checkFirstIncomingVotes(t, tb, expected)

	// now vote again
	err = tb.handleFirstVotingMessage(context.TODO(), *msg)
	assert.ErrorIs(t, err, errAlreadyVoted)

	checkVoted(t, tb, signer, types.FirstRound, true)
	checkFirstIncomingVotes(t, tb, expected)
}

func TestTB_handleFirstVotingMessage_MinerMissingATX(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	valid := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValid := [][]byte{types.HexToHash32("0x23456789").Bytes()}

	tb, mockDB := createTortoiseBeacon(t, epoch)
	mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), gomock.Any()).
		Return(types.ATXID{}, database.ErrNotFound).Times(1)

	signer := signing.NewEdSigner()
	msg := createFirstVote(t, signer, epoch, valid, pValid, false)

	err := tb.handleFirstVotingMessage(context.TODO(), *msg)
	assert.ErrorIs(t, err, errMinerATXNotFound)

	checkVoted(t, tb, signer, types.FirstRound, true)
	checkFirstIncomingVotes(t, tb, map[string]proposals{})
}

func TestTB_handleFirstVotingMessage_ATXLookupError(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	valid := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValid := [][]byte{types.HexToHash32("0x23456789").Bytes()}

	tb, mockDB := createTortoiseBeacon(t, epoch)
	mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), gomock.Any()).
		Return(types.ATXID{}, errUnknown).Times(1)

	signer := signing.NewEdSigner()
	msg := createFirstVote(t, signer, epoch, valid, pValid, false)

	err := tb.handleFirstVotingMessage(context.TODO(), *msg)
	assert.ErrorIs(t, err, errUnknown)

	checkVoted(t, tb, signer, types.FirstRound, true)
	checkFirstIncomingVotes(t, tb, map[string]proposals{})
}

func TestTB_handleFirstVotingMessage_ATXHeaderLookupError(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	valid := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValid := [][]byte{types.HexToHash32("0x23456789").Bytes()}

	tb, mockDB := createTortoiseBeacon(t, epoch)
	mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), gomock.Any()).
		Return(types.ATXID(types.HexToHash32("0x22345678")), nil).Times(1)
	mockDB.EXPECT().GetAtxHeader(gomock.Any()).Return(nil, errUnknown).Times(1)

	signer := signing.NewEdSigner()
	msg := createFirstVote(t, signer, epoch, valid, pValid, false)

	err := tb.handleFirstVotingMessage(context.TODO(), *msg)
	assert.ErrorIs(t, err, errUnknown)

	checkVoted(t, tb, signer, types.FirstRound, true)
	checkFirstIncomingVotes(t, tb, map[string]proposals{})
}

func TestTB_HandleSerializedFollowingVotingMessage_Success(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)

	signer := signing.NewEdSigner()
	tb, mockDB, hashes := createTortoiseBeaconWithFirstRoundVotes(t, epoch, signer)
	setMockAtxDbExpectations(mockDB)
	// this msg will contain a bit vector that set bit 0 and 2
	mockGossip := createFollowingVoteGossip(t, signer, epoch, round, []uint64{0b101}, false)
	mockGossip.EXPECT().ReportValidation(gomock.Any(), TBFollowingVotingProtocol).Times(1)

	tb.HandleSerializedFollowingVotingMessage(context.TODO(), mockGossip, nil)
	checkVoted(t, tb, signer, round, true)
	expected := make(map[string]*big.Int, len(hashes))
	for i, hash := range hashes {
		if i == 0 || i == 2 {
			expected[string(hash.Bytes())] = big.NewInt(10)
		} else {
			expected[string(hash.Bytes())] = big.NewInt(-10)
		}
	}
	checkVoteMargins(t, tb, expected)
}

func TestTB_HandleSerializedFollowingVotingMessage_Shutdown(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)

	signer := signing.NewEdSigner()
	tb, _, _ := createTortoiseBeaconWithFirstRoundVotes(t, epoch, signer)
	atomic.StoreUint64(&tb.running, 0)
	// this msg will contain a bit vector that set bit 0 and 2
	mockGossip := createFollowingVoteGossip(t, signer, epoch, round, []uint64{0b101}, false)

	tb.HandleSerializedFollowingVotingMessage(context.TODO(), mockGossip, nil)
	checkVoted(t, tb, signer, round, false)
	checkVoteMargins(t, tb, map[string]*big.Int{})
}

func TestTB_HandleSerializedFollowingVotingMessage_NotInProtocol(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)

	signer := signing.NewEdSigner()
	tb, _, _ := createTortoiseBeaconWithFirstRoundVotes(t, epoch, signer)
	atomic.StoreUint64(&tb.inProtocol, 0)
	// this msg will contain a bit vector that set bit 0 and 2
	mockGossip := createFollowingVoteGossip(t, signer, epoch, round, []uint64{0b101}, false)

	tb.HandleSerializedFollowingVotingMessage(context.TODO(), mockGossip, nil)
	checkVoted(t, tb, signer, round, false)
	checkVoteMargins(t, tb, map[string]*big.Int{})
}

func TestTB_HandleSerializedFollowingVotingMessage_CorruptedGossipMsg(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer := signing.NewEdSigner()
	tb, _, _ := createTortoiseBeaconWithFirstRoundVotes(t, epoch, signer)
	// this msg will contain a bit vector that set bit 0 and 2
	mockGossip := createFollowingVoteGossip(t, signer, epoch, round, []uint64{0b101}, true)

	tb.HandleSerializedFollowingVotingMessage(context.TODO(), mockGossip, nil)
	checkVoted(t, tb, signer, round, false)
	checkVoteMargins(t, tb, map[string]*big.Int{})
}

func TestTB_HandleSerializedFollowingVotingMessage_WrongEpoch(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer := signing.NewEdSigner()
	tb, _, _ := createTortoiseBeaconWithFirstRoundVotes(t, epoch+1, signer)
	// this msg will contain a bit vector that set bit 0 and 2
	mockGossip := createFollowingVoteGossip(t, signer, epoch, round, []uint64{0b101}, false)

	tb.HandleSerializedFollowingVotingMessage(context.TODO(), mockGossip, nil)
	checkVoted(t, tb, signer, round, false)
	checkVoteMargins(t, tb, map[string]*big.Int{})
}

func TestTB_handleFollowingVotingMessage_Success(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer := signing.NewEdSigner()
	tb, mockDB, hashes := createTortoiseBeaconWithFirstRoundVotes(t, epoch, signer)
	setMockAtxDbExpectations(mockDB)
	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []uint64{0b101}, false)

	err := tb.handleFollowingVotingMessage(context.TODO(), *msg)
	assert.NoError(t, err)

	checkVoted(t, tb, signer, round, true)
	expected := make(map[string]*big.Int, len(hashes))
	for i, hash := range hashes {
		if i == 0 || i == 2 {
			expected[string(hash.Bytes())] = big.NewInt(10)
		} else {
			expected[string(hash.Bytes())] = big.NewInt(-10)
		}
	}
	checkVoteMargins(t, tb, expected)
}

func TestTB_handleFollowingVotingMessage_FailedToExtractPK(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer := signing.NewEdSigner()
	tb, _, _ := createTortoiseBeaconWithFirstRoundVotes(t, epoch, signer)
	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []uint64{0b101}, true)

	err := tb.handleFollowingVotingMessage(context.TODO(), *msg)
	assert.Contains(t, err.Error(), "bad signature format")

	checkVoted(t, tb, signer, round, false)
	checkVoteMargins(t, tb, map[string]*big.Int{})
}

func TestTB_handleFollowingVotingMessage_AlreadyVoted(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer := signing.NewEdSigner()
	tb, mockDB, hashes := createTortoiseBeaconWithFirstRoundVotes(t, epoch, signer)
	setMockAtxDbExpectations(mockDB)
	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []uint64{0b101}, false)

	err := tb.handleFollowingVotingMessage(context.TODO(), *msg)
	assert.NoError(t, err)

	checkVoted(t, tb, signer, round, true)
	expected := make(map[string]*big.Int, len(hashes))
	for i, hash := range hashes {
		if i == 0 || i == 2 {
			expected[string(hash.Bytes())] = big.NewInt(10)
		} else {
			expected[string(hash.Bytes())] = big.NewInt(-10)
		}
	}
	checkVoteMargins(t, tb, expected)

	// now vote again
	err = tb.handleFollowingVotingMessage(context.TODO(), *msg)
	assert.ErrorIs(t, err, errAlreadyVoted)

	checkVoted(t, tb, signer, round, true)
	checkVoteMargins(t, tb, expected)
}

func TestTB_handleFollowingVotingMessage_MinerMissingATX(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer := signing.NewEdSigner()
	tb, mockDB, _ := createTortoiseBeaconWithFirstRoundVotes(t, epoch, signer)
	mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), gomock.Any()).
		Return(types.ATXID{}, database.ErrNotFound).Times(1)
	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []uint64{0b101}, false)

	err := tb.handleFollowingVotingMessage(context.TODO(), *msg)
	assert.ErrorIs(t, err, errMinerATXNotFound)

	checkVoted(t, tb, signer, round, true)
	checkVoteMargins(t, tb, map[string]*big.Int{})
}

func TestTB_handleFollowingVotingMessage_ATXLookupError(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer := signing.NewEdSigner()
	tb, mockDB, _ := createTortoiseBeaconWithFirstRoundVotes(t, epoch, signer)
	mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), gomock.Any()).
		Return(types.ATXID{}, errUnknown).Times(1)
	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []uint64{0b101}, false)

	err := tb.handleFollowingVotingMessage(context.TODO(), *msg)
	assert.ErrorIs(t, err, errUnknown)

	checkVoted(t, tb, signer, round, true)
	checkVoteMargins(t, tb, map[string]*big.Int{})
}

func TestTB_handleFollowingVotingMessage_ATXHeaderLookupError(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer := signing.NewEdSigner()
	tb, mockDB, _ := createTortoiseBeaconWithFirstRoundVotes(t, epoch, signer)
	mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), gomock.Any()).
		Return(types.ATXID(types.HexToHash32("0x22345678")), nil).Times(1)
	mockDB.EXPECT().GetAtxHeader(gomock.Any()).Return(nil, errUnknown).Times(1)
	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []uint64{0b101}, false)

	err := tb.handleFollowingVotingMessage(context.TODO(), *msg)
	assert.ErrorIs(t, err, errUnknown)

	checkVoted(t, tb, signer, round, true)
	checkVoteMargins(t, tb, map[string]*big.Int{})
}

func TestTB_uniqueFollowingVotingMessages(t *testing.T) {
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
