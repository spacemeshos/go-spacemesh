package beacon

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

	"github.com/spacemeshos/go-spacemesh/beacon/mocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

const peerID = "peer1"

var errUnknown = errors.New("unknown")

func clockNeverNotify(t *testing.T) layerClock {
	return timesync.NewClock(timesync.RealClock{}, time.Hour, time.Now(), logtest.New(t).WithName("clock"))
}

func createProtocolDriver(t *testing.T, epoch types.EpochID) (*ProtocolDriver, *mocks.MockactivationDB) {
	types.SetLayersPerEpoch(3)
	ctrl := gomock.NewController(t)
	mockDB := mocks.NewMockactivationDB(ctrl)
	edSgn := signing.NewEdSigner()
	pd := &ProtocolDriver{
		logger:                  logtest.New(t).WithName("Beacon"),
		clock:                   clockNeverNotify(t),
		config:                  UnitTestConfig(),
		atxDB:                   mockDB,
		edSigner:                edSgn,
		edVerifier:              signing.NewEDVerifier(),
		vrfVerifier:             signing.VRFVerifier{},
		proposalChans:           make(map[types.EpochID]chan *proposalMessageWithReceiptData),
		firstRoundIncomingVotes: map[string][][]byte{},
		votesMargin:             map[string]*big.Int{},
		hasProposed:             make(map[string]struct{}),
		hasVoted:                make([]map[string]struct{}, 10),
		epochInProgress:         epoch,
		running:                 1,
		inProtocol:              1,
	}
	return pd, mockDB
}

func createProtocolDriverWithFirstRoundVotes(t *testing.T, epoch types.EpochID, signer signing.Signer) (*ProtocolDriver, *mocks.MockactivationDB, []types.Hash32) {
	pd, mockDB := createProtocolDriver(t, epoch)
	hash1 := types.HexToHash32("0x12345678")
	hash2 := types.HexToHash32("0x23456789")
	hash3 := types.HexToHash32("0x34567890")
	pd.mu.Lock()
	defer pd.mu.Unlock()
	pd.firstRoundIncomingVotes = map[string][][]byte{
		string(signer.PublicKey().Bytes()): {hash1.Bytes(), hash2.Bytes(), hash3.Bytes()},
	}
	return pd, mockDB, []types.Hash32{hash1, hash2, hash3}
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

func checkProposalQueueSize(t *testing.T, pd *ProtocolDriver, epoch types.EpochID, numProposals int) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	ch := pd.getOrCreateProposalChannel(epoch)
	assert.Equal(t, numProposals, len(ch))
}

func checkProposed(t *testing.T, pd *ProtocolDriver, signer signing.Signer, proposed bool) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	_, ok := pd.hasProposed[string(signer.PublicKey().Bytes())]
	assert.Equal(t, proposed, ok)
}

func checkProposals(t *testing.T, pd *ProtocolDriver, expected proposals) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	assert.EqualValues(t, expected, pd.incomingProposals)
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

func checkVoted(t *testing.T, pd *ProtocolDriver, signer signing.Signer, round types.RoundID, voted bool) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	_, ok := pd.hasVoted[round][string(signer.PublicKey().Bytes())]
	assert.Equal(t, voted, ok)
}

func checkFirstIncomingVotes(t *testing.T, pd *ProtocolDriver, expected map[string][][]byte) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	assert.EqualValues(t, expected, pd.firstRoundIncomingVotes)
}

func createFollowingVote(t *testing.T, signer signing.Signer, epoch types.EpochID, round types.RoundID, bitVector []byte, corruptSignature bool) *FollowingVotingMessage {
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

func checkVoteMargins(t *testing.T, pd *ProtocolDriver, expected map[string]*big.Int) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	assert.EqualValues(t, expected, pd.votesMargin)
}

func Test_HandleSerializedProposalMessage_Success(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)

	pd, _ := createProtocolDriver(t, epoch)
	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)

	msg := createProposal(t, signer, vrfSigner, epoch, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	pd.HandleSerializedProposalMessage(context.TODO(), peerID, msgBytes)
	checkProposalQueueSize(t, pd, epoch, 1)
}

func Test_HandleSerializedProposalMessage_Shutdown(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	pd, _ := createProtocolDriver(t, epoch)
	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)

	atomic.StoreUint64(&pd.running, 0)
	msg := createProposal(t, signer, vrfSigner, epoch, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	pd.HandleSerializedProposalMessage(context.TODO(), peerID, msgBytes)
	checkProposalQueueSize(t, pd, epoch, 0)
}

func Test_HandleSerializedProposalMessage_NotInProtocol(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	pd, _ := createProtocolDriver(t, epoch)
	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)

	atomic.StoreUint64(&pd.inProtocol, 0)
	msg := createProposal(t, signer, vrfSigner, epoch, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	pd.HandleSerializedProposalMessage(context.TODO(), peerID, msgBytes)
	checkProposalQueueSize(t, pd, epoch, 0)
}

func Test_HandleSerializedProposalMessage_Corrupted(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	pd, _ := createProtocolDriver(t, epoch)
	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)

	msg := createProposal(t, signer, vrfSigner, epoch, true)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	pd.HandleSerializedProposalMessage(context.TODO(), peerID, msgBytes[1:])
	checkProposalQueueSize(t, pd, epoch, 0)
}

func Test_HandleSerializedProposalMessage_EpochTooOld(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	pd, _ := createProtocolDriver(t, epoch)
	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)

	msg := createProposal(t, signer, vrfSigner, epoch-1, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	pd.HandleSerializedProposalMessage(context.TODO(), peerID, msgBytes)
	checkProposalQueueSize(t, pd, epoch-1, 0)
	checkProposalQueueSize(t, pd, epoch, 0)
}

func Test_HandleSerializedProposalMessage_NextEpoch(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	pd, _ := createProtocolDriver(t, epoch)
	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)

	msg := createProposal(t, signer, vrfSigner, epoch+1, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	pd.HandleSerializedProposalMessage(context.TODO(), peerID, msgBytes)
	checkProposalQueueSize(t, pd, epoch, 0)
	checkProposalQueueSize(t, pd, epoch+1, 1)
}

func Test_HandleSerializedProposalMessage_NextEpochFull(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	pd, _ := createProtocolDriver(t, epoch)
	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)

	for i := 0; i < proposalChanCapacity; i++ {
		msg := createProposal(t, signer, vrfSigner, epoch+1, false)
		msgBytes, err := types.InterfaceToBytes(msg)
		require.NoError(t, err)

		pd.HandleSerializedProposalMessage(context.TODO(), peerID, msgBytes)
	}
	checkProposalQueueSize(t, pd, epoch, 0)
	checkProposalQueueSize(t, pd, epoch+1, proposalChanCapacity)

	// now try to overflow channel for the next epoch
	msg := createProposal(t, signer, vrfSigner, epoch+1, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	pd.HandleSerializedProposalMessage(context.TODO(), peerID, msgBytes)
	checkProposalQueueSize(t, pd, epoch, 0)
	checkProposalQueueSize(t, pd, epoch+1, proposalChanCapacity)
}

func Test_HandleSerializedProposalMessage_EpochTooFarAhead(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	pd, _ := createProtocolDriver(t, epoch)
	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)

	msg := createProposal(t, signer, vrfSigner, epoch+2, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	pd.HandleSerializedProposalMessage(context.TODO(), peerID, msgBytes)
	checkProposalQueueSize(t, pd, epoch, 0)
	checkProposalQueueSize(t, pd, epoch+2, 0)
}

func Test_handleProposalMessage_Success(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	pd, mockDB := createProtocolDriver(t, epoch)
	setMockAtxDbExpectations(mockDB)
	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)
	msg := createProposal(t, signer, vrfSigner, epoch, false)

	ctrl := gomock.NewController(t)
	mockChecker := mocks.NewMockeligibilityChecker(ctrl)
	mockChecker.EXPECT().IsProposalEligible(gomock.Any()).Return(true).Times(1)
	pd.mu.Lock()
	pd.proposalChecker = mockChecker
	pd.mu.Unlock()

	err = pd.handleProposalMessage(context.TODO(), *msg, time.Now())
	assert.NoError(t, err)

	checkProposed(t, pd, vrfSigner, true)
	var expectedProposals proposals
	expectedProposals.valid = [][]byte{msg.VRFSignature}
	checkProposals(t, pd, expectedProposals)
}

func Test_handleProposalMessage_BadSignature(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	pd, mockDB := createProtocolDriver(t, epoch)
	mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), gomock.Any()).
		Return(types.ATXID(types.HexToHash32("0x22345678")), nil).Times(1)

	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)
	msg := createProposal(t, signer, vrfSigner, epoch, true)

	ctrl := gomock.NewController(t)
	mockChecker := mocks.NewMockeligibilityChecker(ctrl)
	mockChecker.EXPECT().IsProposalEligible(gomock.Any()).Return(true).Times(0)

	pd.mu.Lock()
	pd.proposalChecker = mockChecker
	pd.mu.Unlock()

	err = pd.handleProposalMessage(context.TODO(), *msg, time.Now())
	assert.ErrorIs(t, err, errVRFNotVerified)

	checkProposed(t, pd, vrfSigner, false)
	checkProposals(t, pd, proposals{})
}

func Test_handleProposalMessage_AlreadyProposed(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	pd, mockDB := createProtocolDriver(t, epoch)
	setMockAtxDbExpectations(mockDB)
	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)
	msg := createProposal(t, signer, vrfSigner, epoch, false)

	ctrl := gomock.NewController(t)
	mockChecker := mocks.NewMockeligibilityChecker(ctrl)
	mockChecker.EXPECT().IsProposalEligible(gomock.Any()).Return(true).Times(1)

	pd.mu.Lock()
	pd.proposalChecker = mockChecker
	pd.mu.Unlock()

	err = pd.handleProposalMessage(context.TODO(), *msg, time.Now())
	assert.NoError(t, err)

	checkProposed(t, pd, vrfSigner, true)
	var expectedProposals proposals
	expectedProposals.valid = [][]byte{msg.VRFSignature}
	checkProposals(t, pd, expectedProposals)

	// now make the same proposal again
	mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), gomock.Any()).
		Return(types.ATXID(types.HexToHash32("0x22345678")), nil).Times(1)

	err = pd.handleProposalMessage(context.TODO(), *msg, time.Now())
	assert.ErrorIs(t, err, errAlreadyProposed)
	checkProposed(t, pd, vrfSigner, true)
	checkProposals(t, pd, expectedProposals)
}

func Test_handleProposalMessage_ProposalNotEligible(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	pd, mockDB := createProtocolDriver(t, epoch)
	mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), gomock.Any()).
		Return(types.ATXID(types.HexToHash32("0x22345678")), nil).Times(1)

	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)
	msg := createProposal(t, signer, vrfSigner, epoch, false)

	ctrl := gomock.NewController(t)
	mockChecker := mocks.NewMockeligibilityChecker(ctrl)
	mockChecker.EXPECT().IsProposalEligible(gomock.Any()).Return(false).Times(1)

	pd.mu.Lock()
	pd.proposalChecker = mockChecker
	pd.mu.Unlock()

	err = pd.handleProposalMessage(context.TODO(), *msg, time.Now())
	assert.ErrorIs(t, err, errProposalDoesntPassThreshold)

	checkProposed(t, pd, vrfSigner, true)
	checkProposals(t, pd, proposals{})
}

func Test_handleProposalMessage_MinerMissingATX(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	pd, mockDB := createProtocolDriver(t, epoch)
	mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), gomock.Any()).
		Return(types.ATXID{}, database.ErrNotFound).Times(1)

	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)
	msg := createProposal(t, signer, vrfSigner, epoch, false)

	err = pd.handleProposalMessage(context.TODO(), *msg, time.Now())
	assert.ErrorIs(t, err, errMinerATXNotFound)

	checkProposed(t, pd, vrfSigner, false)
	checkProposals(t, pd, proposals{})
}

func Test_handleProposalMessage_ATXLookupError(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	pd, mockDB := createProtocolDriver(t, epoch)
	mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), gomock.Any()).
		Return(types.ATXID{}, errUnknown).Times(1)

	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)
	msg := createProposal(t, signer, vrfSigner, epoch, false)

	err = pd.handleProposalMessage(context.TODO(), *msg, time.Now())
	assert.ErrorIs(t, err, errUnknown)

	checkProposed(t, pd, vrfSigner, false)
	checkProposals(t, pd, proposals{})
}

func Test_handleProposalMessage_ATXHeaderLookupError(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	pd, mockDB := createProtocolDriver(t, epoch)
	mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), gomock.Any()).
		Return(types.ATXID(types.HexToHash32("0x22345678")), nil).Times(1)
	mockDB.EXPECT().GetAtxHeader(gomock.Any()).Return(nil, errUnknown).Times(1)

	ctrl := gomock.NewController(t)
	mockChecker := mocks.NewMockeligibilityChecker(ctrl)
	mockChecker.EXPECT().IsProposalEligible(gomock.Any()).Return(true).Times(1)

	pd.mu.Lock()
	pd.proposalChecker = mockChecker
	pd.mu.Unlock()

	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)

	msg := createProposal(t, signer, vrfSigner, epoch, false)

	err = pd.handleProposalMessage(context.TODO(), *msg, time.Now())
	assert.ErrorIs(t, err, errUnknown)

	checkProposed(t, pd, vrfSigner, true)
	checkProposals(t, pd, proposals{})
}

func Test_HandleSerializedFirstVotingMessage_Success(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	valid := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValid := [][]byte{types.HexToHash32("0x23456789").Bytes()}

	pd, mockDB := createProtocolDriver(t, epoch)
	mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), gomock.Any()).
		Return(types.ATXID(types.HexToHash32("0x22345678")), nil).Times(1)
	mockDB.EXPECT().GetAtxHeader(gomock.Any()).Return(&types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			StartTick: 1,
			EndTick:   3,
		},
		NumUnits: 5,
	}, nil).Times(1)

	signer := signing.NewEdSigner()

	msg := createFirstVote(t, signer, epoch, valid, pValid, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	pd.HandleSerializedFirstVotingMessage(context.TODO(), peerID, msgBytes)
	checkVoted(t, pd, signer, types.FirstRound, true)
	expected := map[string][][]byte{
		string(signer.PublicKey().Bytes()): append(valid, pValid...),
	}
	checkFirstIncomingVotes(t, pd, expected)
}

func Test_HandleSerializedFirstVotingMessage_Shutdown(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	valid := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValid := [][]byte{types.HexToHash32("0x23456789").Bytes()}

	pd, _ := createProtocolDriver(t, epoch)
	atomic.StoreUint64(&pd.running, 0)

	signer := signing.NewEdSigner()
	msg := createFirstVote(t, signer, epoch, valid, pValid, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	pd.HandleSerializedFirstVotingMessage(context.TODO(), peerID, msgBytes)
	checkVoted(t, pd, signer, types.FirstRound, false)
	checkFirstIncomingVotes(t, pd, map[string][][]byte{})
}

func Test_HandleSerializedFirstVotingMessage_NotInProtocol(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	valid := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValid := [][]byte{types.HexToHash32("0x23456789").Bytes()}

	pd, _ := createProtocolDriver(t, epoch)
	atomic.StoreUint64(&pd.inProtocol, 0)

	signer := signing.NewEdSigner()
	msg := createFirstVote(t, signer, epoch, valid, pValid, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	pd.HandleSerializedFirstVotingMessage(context.TODO(), peerID, msgBytes)
	checkVoted(t, pd, signer, types.FirstRound, false)
	checkFirstIncomingVotes(t, pd, map[string][][]byte{})
}

func Test_HandleSerializedFirstVotingMessage_CorruptedGossipMsg(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	valid := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValid := [][]byte{types.HexToHash32("0x23456789").Bytes()}

	pd, _ := createProtocolDriver(t, epoch)
	signer := signing.NewEdSigner()
	msg := createFirstVote(t, signer, epoch, valid, pValid, true)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	pd.HandleSerializedFirstVotingMessage(context.TODO(), peerID, msgBytes)
	checkVoted(t, pd, signer, types.FirstRound, false)
	checkFirstIncomingVotes(t, pd, map[string][][]byte{})
}

func Test_HandleSerializedFirstVotingMessage_WrongEpoch(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	valid := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValid := [][]byte{types.HexToHash32("0x23456789").Bytes()}

	pd, _ := createProtocolDriver(t, epoch+1)
	signer := signing.NewEdSigner()
	msg := createFirstVote(t, signer, epoch, valid, pValid, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	pd.HandleSerializedFirstVotingMessage(context.TODO(), peerID, msgBytes)
	checkVoted(t, pd, signer, types.FirstRound, false)
	checkFirstIncomingVotes(t, pd, map[string][][]byte{})
}

func Test_handleFirstVotingMessage_Success(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	valid := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValid := [][]byte{types.HexToHash32("0x23456789").Bytes()}

	pd, mockDB := createProtocolDriver(t, epoch)
	mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), gomock.Any()).
		Return(types.ATXID(types.HexToHash32("0x22345678")), nil).Times(1)
	mockDB.EXPECT().GetAtxHeader(gomock.Any()).Return(&types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			StartTick: 1,
			EndTick:   3,
		},
		NumUnits: 5,
	}, nil).Times(1)

	signer := signing.NewEdSigner()
	msg := createFirstVote(t, signer, epoch, valid, pValid, false)

	err := pd.handleFirstVotingMessage(context.TODO(), *msg)
	assert.NoError(t, err)

	checkVoted(t, pd, signer, types.FirstRound, true)
	expected := map[string][][]byte{
		string(signer.PublicKey().Bytes()): append(valid, pValid...),
	}
	checkFirstIncomingVotes(t, pd, expected)
}

func Test_handleFirstVotingMessage_FailedToExtractPK(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	valid := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValid := [][]byte{types.HexToHash32("0x23456789").Bytes()}

	pd, _ := createProtocolDriver(t, epoch)
	signer := signing.NewEdSigner()
	msg := createFirstVote(t, signer, epoch, valid, pValid, true)

	err := pd.handleFirstVotingMessage(context.TODO(), *msg)
	assert.Contains(t, err.Error(), "bad signature format")

	checkVoted(t, pd, signer, types.FirstRound, false)
	checkFirstIncomingVotes(t, pd, map[string][][]byte{})
}

func Test_handleFirstVotingMessage_AlreadyVoted(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	valid := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValid := [][]byte{types.HexToHash32("0x23456789").Bytes()}

	pd, mockDB := createProtocolDriver(t, epoch)
	mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), gomock.Any()).
		Return(types.ATXID(types.HexToHash32("0x22345678")), nil).Times(1)
	mockDB.EXPECT().GetAtxHeader(gomock.Any()).Return(&types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			StartTick: 1,
			EndTick:   3,
		},
		NumUnits: 5,
	}, nil).Times(1)

	signer := signing.NewEdSigner()
	msg := createFirstVote(t, signer, epoch, valid, pValid, false)

	err := pd.handleFirstVotingMessage(context.TODO(), *msg)
	assert.NoError(t, err)

	checkVoted(t, pd, signer, types.FirstRound, true)
	expected := map[string][][]byte{
		string(signer.PublicKey().Bytes()): append(valid, pValid...),
	}
	checkFirstIncomingVotes(t, pd, expected)

	// now vote again
	err = pd.handleFirstVotingMessage(context.TODO(), *msg)
	assert.ErrorIs(t, err, errAlreadyVoted)

	checkVoted(t, pd, signer, types.FirstRound, true)
	checkFirstIncomingVotes(t, pd, expected)
}

func Test_handleFirstVotingMessage_MinerMissingATX(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	valid := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValid := [][]byte{types.HexToHash32("0x23456789").Bytes()}

	pd, mockDB := createProtocolDriver(t, epoch)
	mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), gomock.Any()).
		Return(types.ATXID{}, database.ErrNotFound).Times(1)

	signer := signing.NewEdSigner()
	msg := createFirstVote(t, signer, epoch, valid, pValid, false)

	err := pd.handleFirstVotingMessage(context.TODO(), *msg)
	assert.ErrorIs(t, err, errMinerATXNotFound)

	checkVoted(t, pd, signer, types.FirstRound, true)
	checkFirstIncomingVotes(t, pd, map[string][][]byte{})
}

func Test_handleFirstVotingMessage_ATXLookupError(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	valid := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValid := [][]byte{types.HexToHash32("0x23456789").Bytes()}

	pd, mockDB := createProtocolDriver(t, epoch)
	mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), gomock.Any()).
		Return(types.ATXID{}, errUnknown).Times(1)

	signer := signing.NewEdSigner()
	msg := createFirstVote(t, signer, epoch, valid, pValid, false)

	err := pd.handleFirstVotingMessage(context.TODO(), *msg)
	assert.ErrorIs(t, err, errUnknown)

	checkVoted(t, pd, signer, types.FirstRound, true)
	checkFirstIncomingVotes(t, pd, map[string][][]byte{})
}

func Test_handleFirstVotingMessage_ATXHeaderLookupError(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	valid := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValid := [][]byte{types.HexToHash32("0x23456789").Bytes()}

	pd, mockDB := createProtocolDriver(t, epoch)
	mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), gomock.Any()).
		Return(types.ATXID(types.HexToHash32("0x22345678")), nil).Times(1)
	mockDB.EXPECT().GetAtxHeader(gomock.Any()).Return(nil, errUnknown).Times(1)

	signer := signing.NewEdSigner()
	msg := createFirstVote(t, signer, epoch, valid, pValid, false)

	err := pd.handleFirstVotingMessage(context.TODO(), *msg)
	assert.ErrorIs(t, err, errUnknown)

	checkVoted(t, pd, signer, types.FirstRound, true)
	checkFirstIncomingVotes(t, pd, map[string][][]byte{})
}

func Test_HandleSerializedFollowingVotingMessage_Success(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)

	signer := signing.NewEdSigner()
	pd, mockDB, hashes := createProtocolDriverWithFirstRoundVotes(t, epoch, signer)
	mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), gomock.Any()).
		Return(types.ATXID(types.HexToHash32("0x22345678")), nil).Times(1)
	mockDB.EXPECT().GetAtxHeader(gomock.Any()).Return(&types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			StartTick: 1,
			EndTick:   3,
		},
		NumUnits: 5,
	}, nil).Times(1)

	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []byte{0b101}, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	pd.HandleSerializedFollowingVotingMessage(context.TODO(), peerID, msgBytes)
	checkVoted(t, pd, signer, round, true)
	expected := make(map[string]*big.Int, len(hashes))
	for i, hash := range hashes {
		if i == 0 || i == 2 {
			expected[string(hash.Bytes())] = big.NewInt(10)
		} else {
			expected[string(hash.Bytes())] = big.NewInt(-10)
		}
	}
	checkVoteMargins(t, pd, expected)
}

func Test_HandleSerializedFollowingVotingMessage_Shutdown(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)

	signer := signing.NewEdSigner()
	pd, _, _ := createProtocolDriverWithFirstRoundVotes(t, epoch, signer)
	atomic.StoreUint64(&pd.running, 0)

	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []byte{0b101}, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	pd.HandleSerializedFollowingVotingMessage(context.TODO(), peerID, msgBytes)
	checkVoted(t, pd, signer, round, false)
	checkVoteMargins(t, pd, map[string]*big.Int{})
}

func Test_HandleSerializedFollowingVotingMessage_NotInProtocol(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)

	signer := signing.NewEdSigner()
	pd, _, _ := createProtocolDriverWithFirstRoundVotes(t, epoch, signer)
	atomic.StoreUint64(&pd.inProtocol, 0)
	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []byte{0b101}, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	pd.HandleSerializedFollowingVotingMessage(context.TODO(), peerID, msgBytes)
	checkVoted(t, pd, signer, round, false)
	checkVoteMargins(t, pd, map[string]*big.Int{})
}

func Test_HandleSerializedFollowingVotingMessage_CorruptedGossipMsg(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer := signing.NewEdSigner()
	pd, _, _ := createProtocolDriverWithFirstRoundVotes(t, epoch, signer)
	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []byte{0b101}, true)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	pd.HandleSerializedFollowingVotingMessage(context.TODO(), peerID, msgBytes)
	checkVoted(t, pd, signer, round, false)
	checkVoteMargins(t, pd, map[string]*big.Int{})
}

func Test_HandleSerializedFollowingVotingMessage_WrongEpoch(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer := signing.NewEdSigner()
	pd, _, _ := createProtocolDriverWithFirstRoundVotes(t, epoch+1, signer)
	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []byte{0b101}, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	pd.HandleSerializedFollowingVotingMessage(context.TODO(), peerID, msgBytes)
	checkVoted(t, pd, signer, round, false)
	checkVoteMargins(t, pd, map[string]*big.Int{})
}

func Test_handleFollowingVotingMessage_Success(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer := signing.NewEdSigner()
	pd, mockDB, hashes := createProtocolDriverWithFirstRoundVotes(t, epoch, signer)
	mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), gomock.Any()).
		Return(types.ATXID(types.HexToHash32("0x22345678")), nil).Times(1)
	mockDB.EXPECT().GetAtxHeader(gomock.Any()).Return(&types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			StartTick: 1,
			EndTick:   3,
		},
		NumUnits: 5,
	}, nil).Times(1)

	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []byte{0b101}, false)

	err := pd.handleFollowingVotingMessage(context.TODO(), *msg)
	assert.NoError(t, err)

	checkVoted(t, pd, signer, round, true)
	expected := make(map[string]*big.Int, len(hashes))
	for i, hash := range hashes {
		if i == 0 || i == 2 {
			expected[string(hash.Bytes())] = big.NewInt(10)
		} else {
			expected[string(hash.Bytes())] = big.NewInt(-10)
		}
	}
	checkVoteMargins(t, pd, expected)
}

func Test_handleFollowingVotingMessage_FailedToExtractPK(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer := signing.NewEdSigner()
	pd, _, _ := createProtocolDriverWithFirstRoundVotes(t, epoch, signer)
	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []byte{0b101}, true)

	err := pd.handleFollowingVotingMessage(context.TODO(), *msg)
	assert.Contains(t, err.Error(), "bad signature format")

	checkVoted(t, pd, signer, round, false)
	checkVoteMargins(t, pd, map[string]*big.Int{})
}

func Test_handleFollowingVotingMessage_AlreadyVoted(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer := signing.NewEdSigner()
	pd, mockDB, hashes := createProtocolDriverWithFirstRoundVotes(t, epoch, signer)
	mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), gomock.Any()).
		Return(types.ATXID(types.HexToHash32("0x22345678")), nil).Times(1)
	mockDB.EXPECT().GetAtxHeader(gomock.Any()).Return(&types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			StartTick: 1,
			EndTick:   3,
		},
		NumUnits: 5,
	}, nil).Times(1)

	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []byte{0b101}, false)

	err := pd.handleFollowingVotingMessage(context.TODO(), *msg)
	assert.NoError(t, err)

	checkVoted(t, pd, signer, round, true)
	expected := make(map[string]*big.Int, len(hashes))
	for i, hash := range hashes {
		if i == 0 || i == 2 {
			expected[string(hash.Bytes())] = big.NewInt(10)
		} else {
			expected[string(hash.Bytes())] = big.NewInt(-10)
		}
	}
	checkVoteMargins(t, pd, expected)

	// now vote again
	err = pd.handleFollowingVotingMessage(context.TODO(), *msg)
	assert.ErrorIs(t, err, errAlreadyVoted)

	checkVoted(t, pd, signer, round, true)
	checkVoteMargins(t, pd, expected)
}

func Test_handleFollowingVotingMessage_MinerMissingATX(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer := signing.NewEdSigner()
	pd, mockDB, _ := createProtocolDriverWithFirstRoundVotes(t, epoch, signer)
	mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), gomock.Any()).
		Return(types.ATXID{}, database.ErrNotFound).Times(1)
	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []byte{0b101}, false)

	err := pd.handleFollowingVotingMessage(context.TODO(), *msg)
	assert.ErrorIs(t, err, errMinerATXNotFound)

	checkVoted(t, pd, signer, round, true)
	checkVoteMargins(t, pd, map[string]*big.Int{})
}

func Test_handleFollowingVotingMessage_ATXLookupError(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer := signing.NewEdSigner()
	pd, mockDB, _ := createProtocolDriverWithFirstRoundVotes(t, epoch, signer)
	mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), gomock.Any()).
		Return(types.ATXID{}, errUnknown).Times(1)
	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []byte{0b101}, false)

	err := pd.handleFollowingVotingMessage(context.TODO(), *msg)
	assert.ErrorIs(t, err, errUnknown)

	checkVoted(t, pd, signer, round, true)
	checkVoteMargins(t, pd, map[string]*big.Int{})
}

func Test_handleFollowingVotingMessage_ATXHeaderLookupError(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer := signing.NewEdSigner()
	pd, mockDB, _ := createProtocolDriverWithFirstRoundVotes(t, epoch, signer)
	mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), gomock.Any()).
		Return(types.ATXID(types.HexToHash32("0x22345678")), nil).Times(1)
	mockDB.EXPECT().GetAtxHeader(gomock.Any()).Return(nil, errUnknown).Times(1)
	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []byte{0b101}, false)

	err := pd.handleFollowingVotingMessage(context.TODO(), *msg)
	assert.ErrorIs(t, err, errUnknown)

	checkVoted(t, pd, signer, round, true)
	checkVoteMargins(t, pd, map[string]*big.Int{})
}

func Test_uniqueFollowingVotingMessages(t *testing.T) {
	round := types.RoundID(3)
	votesBitVector := []byte{0b101}
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
