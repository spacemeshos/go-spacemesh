package beacon

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/beacon/mocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
)

const (
	epochWeight = uint64(100)
)

func createProtocolDriver(t *testing.T, epoch types.EpochID) *testProtocolDriver {
	t.Helper()
	tpd := setUpProtocolDriver(t)
	createEpochState(t, tpd.ProtocolDriver, epoch)
	tpd.setBeginProtocol(context.TODO())
	return tpd
}

func createProtocolDriverWithFirstRoundVotes(t *testing.T, signer signing.Signer, epoch types.EpochID, round types.RoundID) (*testProtocolDriver, proposalList) {
	tpd := createProtocolDriver(t, epoch)
	plist := proposalList{types.RandomBytes(types.BeaconSize), types.RandomBytes(types.BeaconSize), types.RandomBytes(types.BeaconSize)}
	setFirstRoundVotes(t, tpd.ProtocolDriver, epoch, signer.PublicKey(), plist)
	tpd.setRoundInProgress(round)
	return tpd, plist
}

func createEpochState(t *testing.T, pd *ProtocolDriver, epoch types.EpochID) {
	t.Helper()
	pd.mu.Lock()
	defer pd.mu.Unlock()
	pd.states[epoch] = newState(pd.logger, pd.config, epochWeight, nil)
}

func setFirstRoundVotes(t *testing.T, pd *ProtocolDriver, epoch types.EpochID, minerPK *signing.PublicKey, plist proposalList) {
	t.Helper()
	pd.mu.Lock()
	defer pd.mu.Unlock()
	pd.states[epoch].setMinerFirstRoundVote(minerPK, plist)
}

func setMockAtxDbForProposals(mockDB *mocks.MockactivationDB, epoch types.EpochID) {
	atxID := types.RandomATXID()
	mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), epoch-1).
		Return(atxID, nil).Times(1)
	mockDB.EXPECT().GetAtxHeader(atxID).Return(&types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			StartTick:  1,
			EndTick:    3,
			PubLayerID: epoch.FirstLayer().Sub(1),
		},
		NumUnits: 5,
	}, nil).Times(1)
	mockDB.EXPECT().GetAtxTimestamp(atxID).Return(time.Now().Add(-1*time.Second), nil).Times(1)
}

func setMockAtxDbForVotes(mockDB *mocks.MockactivationDB, epoch types.EpochID) {
	atxID := types.RandomATXID()
	mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), epoch-1).Return(atxID, nil).Times(1)
	mockDB.EXPECT().GetAtxHeader(atxID).Return(&types.ActivationTxHeader{
		NIPostChallenge: types.NIPostChallenge{
			StartTick: 1,
			EndTick:   3,
		},
		NumUnits: 5,
	}, nil).Times(1)
}

func mockAlwaysFalseProposalChecker(t *testing.T, pd *ProtocolDriver, epoch types.EpochID) {
	t.Helper()
	ctrl := gomock.NewController(t)
	mockChecker := mocks.NewMockeligibilityChecker(ctrl)
	mockChecker.EXPECT().IsProposalEligible(gomock.Any()).Return(false).Times(1)
	pd.mu.Lock()
	defer pd.mu.Unlock()
	require.NotNil(t, pd.states[epoch])
	pd.states[epoch].proposalChecker = mockChecker
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

func setEarliestProposalTime(pd *ProtocolDriver, t time.Time) {
	pd.mu.Lock()
	defer pd.mu.Unlock()
	pd.earliestProposalTime = t
}

func checkProposed(t *testing.T, pd *ProtocolDriver, epoch types.EpochID, minerPK *signing.PublicKey, expected bool) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	if _, ok := pd.states[epoch]; ok {
		_, proposed := pd.states[epoch].hasProposed[string(minerPK.Bytes())]
		assert.Equal(t, expected, proposed)
	} else {
		assert.False(t, expected)
	}
}

func checkProposals(t *testing.T, pd *ProtocolDriver, epoch types.EpochID, expected proposals) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	if _, ok := pd.states[epoch]; ok {
		assert.EqualValues(t, expected, pd.states[epoch].incomingProposals)
	} else {
		assert.Equal(t, expected, proposals{})
	}
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

func checkVoted(t *testing.T, pd *ProtocolDriver, epoch types.EpochID, signer signing.Signer, round types.RoundID, voted bool) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	require.NotNil(t, pd.states[epoch])
	_, ok := pd.states[epoch].hasVoted[round][string(signer.PublicKey().Bytes())]
	assert.Equal(t, voted, ok)
}

func checkFirstIncomingVotes(t *testing.T, pd *ProtocolDriver, epoch types.EpochID, expected map[string]proposalList) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	require.NotNil(t, pd.states[epoch])
	assert.EqualValues(t, expected, pd.states[epoch].firstRoundIncomingVotes)
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

func checkVoteMargins(t *testing.T, pd *ProtocolDriver, epoch types.EpochID, expected map[string]*big.Int) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	require.NotNil(t, pd.states[epoch])
	assert.EqualValues(t, expected, pd.states[epoch].votesMargin)
}

func Test_HandleProposal_Success(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := createProtocolDriver(t, epoch)

	signer1 := signing.NewEdSigner()
	vrfSigner1, _, err := signing.NewVRFSigner(signer1.PublicKey().Bytes())
	require.NoError(t, err)

	msg1 := createProposal(t, signer1, vrfSigner1, epoch, false)
	msgBytes1, err := types.InterfaceToBytes(msg1)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	tpd.mClock.EXPECT().LayerToTime(epoch.FirstLayer()).Return(time.Now()).Times(1)
	setMockAtxDbForProposals(tpd.mAtxDB, epoch)
	res := tpd.HandleProposal(context.TODO(), "peerID", msgBytes1)
	assert.Equal(t, pubsub.ValidationAccept, res)

	tpd.markProposalPhaseFinished(epoch, time.Now())

	signer2 := signing.NewEdSigner()
	vrfSigner2, _, err := signing.NewVRFSigner(signer2.PublicKey().Bytes())
	require.NoError(t, err)

	msg2 := createProposal(t, signer2, vrfSigner2, epoch, false)
	msgBytes2, err := types.InterfaceToBytes(msg2)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	tpd.mClock.EXPECT().LayerToTime(epoch.FirstLayer()).Return(time.Now()).Times(1)
	setMockAtxDbForProposals(tpd.mAtxDB, epoch)
	res = tpd.HandleProposal(context.TODO(), "peerID", msgBytes2)
	assert.Equal(t, pubsub.ValidationAccept, res)

	p1 := msg1.VRFSignature[:types.BeaconSize]
	p2 := msg2.VRFSignature[:types.BeaconSize]
	checkProposed(t, tpd.ProtocolDriver, epoch, vrfSigner1.PublicKey(), true)
	expectedProposals := proposals{
		valid:            proposalSet{string(p1): struct{}{}},
		potentiallyValid: proposalSet{string(p2): struct{}{}},
	}
	checkProposals(t, tpd.ProtocolDriver, epoch, expectedProposals)
}

func Test_HandleProposal_Shutdown(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := createProtocolDriver(t, epoch)
	tpd.mClock.EXPECT().Unsubscribe(gomock.Any()).Times(1)
	tpd.Close()

	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)

	msg := createProposal(t, signer, vrfSigner, epoch, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	res := tpd.HandleProposal(context.TODO(), "peerID", msgBytes)
	assert.Equal(t, pubsub.ValidationIgnore, res)

	checkProposed(t, tpd.ProtocolDriver, epoch, vrfSigner.PublicKey(), false)
	checkProposals(t, tpd.ProtocolDriver, epoch, proposals{})
}

func Test_HandleProposal_NotInProtocolStillWorks(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := createProtocolDriver(t, epoch)

	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)

	msg := createProposal(t, signer, vrfSigner, epoch, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	tpd.mClock.EXPECT().LayerToTime(epoch.FirstLayer()).Return(time.Now()).Times(1)
	setMockAtxDbForProposals(tpd.mAtxDB, epoch)

	tpd.setEndProtocol(context.TODO())
	require.False(t, tpd.isInProtocol())
	res := tpd.HandleProposal(context.TODO(), "peerID", msgBytes)
	assert.Equal(t, pubsub.ValidationAccept, res)

	p := msg.VRFSignature[:types.BeaconSize]
	checkProposed(t, tpd.ProtocolDriver, epoch, vrfSigner.PublicKey(), true)
	expectedProposals := proposals{
		valid: proposalSet{string(p): struct{}{}},
	}
	checkProposals(t, tpd.ProtocolDriver, epoch, expectedProposals)
}

func Test_handleProposal_Corrupted(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := createProtocolDriver(t, epoch)

	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)

	msg := createProposal(t, signer, vrfSigner, epoch, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	got := tpd.handleProposal(context.TODO(), "peerID", msgBytes[1:], time.Now())
	assert.ErrorIs(t, got, errMalformedMessage)

	checkProposed(t, tpd.ProtocolDriver, epoch, vrfSigner.PublicKey(), false)
	checkProposals(t, tpd.ProtocolDriver, epoch, proposals{})
}

func Test_handleProposal_EpochTooOld(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := createProtocolDriver(t, epoch)

	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)

	msg := createProposal(t, signer, vrfSigner, epoch-1, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	got := tpd.handleProposal(context.TODO(), "peerID", msgBytes, time.Now())
	assert.ErrorIs(t, got, errUntimelyMessage)

	checkProposed(t, tpd.ProtocolDriver, epoch, vrfSigner.PublicKey(), false)
	checkProposals(t, tpd.ProtocolDriver, epoch, proposals{})
}

func Test_handleProposal_NextEpoch(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const nextEpoch = epoch + 1
	tpd := createProtocolDriver(t, epoch)
	tpd.mAtxDB.EXPECT().GetEpochWeight(nextEpoch).Return(epochWeight, nil, nil).Times(1)

	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)

	msg := createProposal(t, signer, vrfSigner, nextEpoch, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	setEarliestProposalTime(tpd.ProtocolDriver, time.Now().Add(-1*time.Second))
	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).AnyTimes()
	tpd.mClock.EXPECT().LayerToTime((nextEpoch).FirstLayer()).Return(time.Now()).Times(1)
	setMockAtxDbForProposals(tpd.mAtxDB, nextEpoch)
	got := tpd.handleProposal(context.TODO(), "peerID", msgBytes, time.Now())
	assert.NoError(t, got)

	// nothing added to the current epoch
	checkProposed(t, tpd.ProtocolDriver, epoch, vrfSigner.PublicKey(), false)
	checkProposals(t, tpd.ProtocolDriver, epoch, proposals{})

	// proposal added to the next epoch
	p := msg.VRFSignature[:types.BeaconSize]
	checkProposed(t, tpd.ProtocolDriver, nextEpoch, vrfSigner.PublicKey(), true)
	expectedProposals := proposals{
		valid: proposalSet{string(p): struct{}{}},
	}
	checkProposals(t, tpd.ProtocolDriver, nextEpoch, expectedProposals)
}

func Test_handleProposal_NextEpochTooEarly(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const nextEpoch = epoch + 1
	tpd := createProtocolDriver(t, epoch)

	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)

	msg := createProposal(t, signer, vrfSigner, nextEpoch, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	now := time.Now()
	setEarliestProposalTime(tpd.ProtocolDriver, now.Add(1*time.Second))
	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).AnyTimes()
	got := tpd.handleProposal(context.TODO(), "peerID", msgBytes, now)
	assert.ErrorIs(t, got, errUntimelyMessage)

	// nothing added to the current epoch
	checkProposed(t, tpd.ProtocolDriver, epoch, vrfSigner.PublicKey(), false)
	checkProposals(t, tpd.ProtocolDriver, epoch, proposals{})

	// proposal added to the next epoch
	checkProposed(t, tpd.ProtocolDriver, nextEpoch, vrfSigner.PublicKey(), false)
	checkProposals(t, tpd.ProtocolDriver, nextEpoch, proposals{})
}

func Test_handleProposal_EpochTooFarAhead(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := createProtocolDriver(t, epoch)

	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)

	msg := createProposal(t, signer, vrfSigner, epoch+2, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	got := tpd.handleProposal(context.TODO(), "peerID", msgBytes, time.Now())
	assert.ErrorIs(t, got, errUntimelyMessage)

	checkProposed(t, tpd.ProtocolDriver, epoch, vrfSigner.PublicKey(), false)
	checkProposals(t, tpd.ProtocolDriver, epoch, proposals{})
}

func Test_handleProposal_BadSignature(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := createProtocolDriver(t, epoch)

	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)

	msg := createProposal(t, signer, vrfSigner, epoch, true)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	tpd.mAtxDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), epoch-1).Return(types.ATXID{}, nil).Times(1)
	got := tpd.handleProposal(context.TODO(), "peerID", msgBytes, time.Now())
	assert.ErrorIs(t, got, errVRFNotVerified)

	checkProposed(t, tpd.ProtocolDriver, epoch, vrfSigner.PublicKey(), false)
	checkProposals(t, tpd.ProtocolDriver, epoch, proposals{})
}

func Test_handleProposal_AlreadyProposed(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := createProtocolDriver(t, epoch)

	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)

	msg1 := createProposal(t, signer, vrfSigner, epoch, false)
	msgBytes1, err := types.InterfaceToBytes(msg1)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	tpd.mClock.EXPECT().LayerToTime(epoch.FirstLayer()).Return(time.Now()).Times(1)
	setMockAtxDbForProposals(tpd.mAtxDB, epoch)
	got := tpd.handleProposal(context.TODO(), "peerID", msgBytes1, time.Now())
	require.NoError(t, got)

	checkProposed(t, tpd.ProtocolDriver, epoch, vrfSigner.PublicKey(), true)
	p := msg1.VRFSignature[:types.BeaconSize]
	expectedProposals := proposals{
		valid: proposalSet{string(p): struct{}{}},
	}
	checkProposals(t, tpd.ProtocolDriver, epoch, expectedProposals)

	// the same vrf key will not cause double-proposal
	msg2 := createProposal(t, signing.NewEdSigner(), vrfSigner, epoch, false)
	msgBytes2, err := types.InterfaceToBytes(msg2)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	tpd.mAtxDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), epoch-1).Return(types.ATXID{}, nil).Times(1)
	got = tpd.handleProposal(context.TODO(), "peerID", msgBytes2, time.Now())
	assert.ErrorIs(t, got, errAlreadyProposed)

	checkProposed(t, tpd.ProtocolDriver, epoch, vrfSigner.PublicKey(), true)
	checkProposals(t, tpd.ProtocolDriver, epoch, expectedProposals)
}

func Test_handleProposal_ProposalNotEligible(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := createProtocolDriver(t, epoch)
	mockAlwaysFalseProposalChecker(t, tpd.ProtocolDriver, epoch)

	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)

	msg := createProposal(t, signer, vrfSigner, epoch, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	tpd.mAtxDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), epoch-1).Return(types.ATXID{}, nil).Times(1)
	got := tpd.handleProposal(context.TODO(), "peerID", msgBytes, time.Now())
	assert.ErrorIs(t, got, errProposalDoesntPassThreshold)

	checkProposed(t, tpd.ProtocolDriver, epoch, vrfSigner.PublicKey(), true)
	checkProposals(t, tpd.ProtocolDriver, epoch, proposals{})
}

func Test_handleProposal_MinerMissingATX(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := createProtocolDriver(t, epoch)

	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)

	msg := createProposal(t, signer, vrfSigner, epoch, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	tpd.mAtxDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), epoch-1).Return(types.ATXID{}, sql.ErrNotFound).Times(1)
	got := tpd.handleProposal(context.TODO(), "peerID", msgBytes, time.Now())
	assert.ErrorIs(t, got, errMinerATXNotFound)

	checkProposed(t, tpd.ProtocolDriver, epoch, vrfSigner.PublicKey(), false)
	checkProposals(t, tpd.ProtocolDriver, epoch, proposals{})
}

func Test_handleProposal_ATXLookupError(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := createProtocolDriver(t, epoch)

	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)

	msg := createProposal(t, signer, vrfSigner, epoch, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	errUnknown := errors.New("unknown")
	tpd.mAtxDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), epoch-1).Return(types.ATXID{}, errUnknown).Times(1)
	got := tpd.handleProposal(context.TODO(), "peerID", msgBytes, time.Now())
	assert.ErrorIs(t, got, errUnknown)

	checkProposed(t, tpd.ProtocolDriver, epoch, vrfSigner.PublicKey(), false)
	checkProposals(t, tpd.ProtocolDriver, epoch, proposals{})
}

func Test_handleProposal_ATXHeaderLookupError(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := createProtocolDriver(t, epoch)

	signer := signing.NewEdSigner()
	vrfSigner, _, err := signing.NewVRFSigner(signer.Sign(signer.PublicKey().Bytes()))
	require.NoError(t, err)

	msg := createProposal(t, signer, vrfSigner, epoch, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	errUnknown := errors.New("unknown")
	atxID := types.RandomATXID()
	tpd.mAtxDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), epoch-1).Return(atxID, nil).Times(1)
	tpd.mAtxDB.EXPECT().GetAtxHeader(atxID).Return(nil, errUnknown).Times(1)
	got := tpd.handleProposal(context.TODO(), "peerID", msgBytes, time.Now())
	assert.ErrorIs(t, got, errUnknown)

	checkProposed(t, tpd.ProtocolDriver, epoch, vrfSigner.PublicKey(), true)
	checkProposals(t, tpd.ProtocolDriver, epoch, proposals{})
}

func Test_HandleFirstVotes_Success(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := createProtocolDriver(t, epoch)

	validVotes := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValidVotes := [][]byte{types.HexToHash32("0x23456789").Bytes()}
	signer := signing.NewEdSigner()
	msg := createFirstVote(t, signer, epoch, validVotes, pValidVotes, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	setMockAtxDbForVotes(tpd.mAtxDB, epoch)
	res := tpd.HandleFirstVotes(context.TODO(), "peerID", msgBytes)
	assert.Equal(t, pubsub.ValidationAccept, res)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, types.FirstRound, true)
	expected := map[string]proposalList{
		string(signer.PublicKey().Bytes()): append(validVotes, pValidVotes...),
	}
	checkFirstIncomingVotes(t, tpd.ProtocolDriver, epoch, expected)
}

func Test_HandleFirstVotes_Shutdown(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := createProtocolDriver(t, epoch)
	tpd.mClock.EXPECT().Unsubscribe(gomock.Any()).Times(1)
	tpd.Close()

	validVotes := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValidVotes := [][]byte{types.HexToHash32("0x23456789").Bytes()}
	signer := signing.NewEdSigner()
	msg := createFirstVote(t, signer, epoch, validVotes, pValidVotes, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	res := tpd.HandleFirstVotes(context.TODO(), "peerID", msgBytes)
	assert.Equal(t, pubsub.ValidationIgnore, res)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, types.FirstRound, false)
	checkFirstIncomingVotes(t, tpd.ProtocolDriver, epoch, map[string]proposalList{})
}

func Test_HandleFirstVotes_NotInProtocol(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := createProtocolDriver(t, epoch)

	validVotes := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValidVotes := [][]byte{types.HexToHash32("0x23456789").Bytes()}
	signer := signing.NewEdSigner()
	msg := createFirstVote(t, signer, epoch, validVotes, pValidVotes, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	tpd.setEndProtocol(context.TODO())
	res := tpd.HandleFirstVotes(context.TODO(), "peerID", msgBytes)
	assert.Equal(t, pubsub.ValidationIgnore, res)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, types.FirstRound, false)
	checkFirstIncomingVotes(t, tpd.ProtocolDriver, epoch, map[string]proposalList{})
}

func Test_handleFirstVotes_CorruptMsg(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := createProtocolDriver(t, epoch)

	validVotes := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValidVotes := [][]byte{types.HexToHash32("0x23456789").Bytes()}
	signer := signing.NewEdSigner()
	msg := createFirstVote(t, signer, epoch, validVotes, pValidVotes, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	got := tpd.handleFirstVotes(context.TODO(), "peerID", msgBytes[1:])
	assert.ErrorIs(t, got, errMalformedMessage)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, types.FirstRound, false)
	checkFirstIncomingVotes(t, tpd.ProtocolDriver, epoch, map[string]proposalList{})
}

func Test_handleFirstVotes_WrongEpoch(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := createProtocolDriver(t, epoch)
	createEpochState(t, tpd.ProtocolDriver, epoch-1)
	createEpochState(t, tpd.ProtocolDriver, epoch+1)

	validVotes := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValidVotes := [][]byte{types.HexToHash32("0x23456789").Bytes()}
	signer := signing.NewEdSigner()
	msg := createFirstVote(t, signer, epoch+1, validVotes, pValidVotes, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	got := tpd.handleFirstVotes(context.TODO(), "peerID", msgBytes)
	assert.ErrorIs(t, got, errEpochNotActive)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, types.FirstRound, false)
	checkFirstIncomingVotes(t, tpd.ProtocolDriver, epoch, map[string]proposalList{})
	checkVoted(t, tpd.ProtocolDriver, epoch+1, signer, types.FirstRound, false)
	checkFirstIncomingVotes(t, tpd.ProtocolDriver, epoch+1, map[string]proposalList{})

	msg = createFirstVote(t, signer, epoch-1, validVotes, pValidVotes, false)
	msgBytes, err = types.InterfaceToBytes(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	got = tpd.handleFirstVotes(context.TODO(), "peerID", msgBytes)
	assert.ErrorIs(t, got, errEpochNotActive)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, types.FirstRound, false)
	checkFirstIncomingVotes(t, tpd.ProtocolDriver, epoch, map[string]proposalList{})
	checkVoted(t, tpd.ProtocolDriver, epoch-1, signer, types.FirstRound, false)
	checkFirstIncomingVotes(t, tpd.ProtocolDriver, epoch-1, map[string]proposalList{})
}

func Test_handleFirstVotes_TooLate(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := createProtocolDriver(t, epoch)

	validVotes := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValidVotes := [][]byte{types.HexToHash32("0x23456789").Bytes()}
	signer := signing.NewEdSigner()
	msg := createFirstVote(t, signer, epoch, validVotes, pValidVotes, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	tpd.setRoundInProgress(types.RoundID(1))
	got := tpd.handleFirstVotes(context.TODO(), "peerID", msgBytes)
	assert.ErrorIs(t, got, errUntimelyMessage)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, types.FirstRound, false)
	checkFirstIncomingVotes(t, tpd.ProtocolDriver, epoch, map[string]proposalList{})
}

func Test_HandleFirstVotes_FailedToExtractPK(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := createProtocolDriver(t, epoch)

	validVotes := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValidVotes := [][]byte{types.HexToHash32("0x23456789").Bytes()}
	signer := signing.NewEdSigner()
	msg := createFirstVote(t, signer, epoch, validVotes, pValidVotes, true)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	got := tpd.handleFirstVotes(context.TODO(), "peerID", msgBytes)
	assert.Contains(t, got.Error(), "bad signature format")
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, types.FirstRound, false)
	checkFirstIncomingVotes(t, tpd.ProtocolDriver, epoch, map[string]proposalList{})
}

func Test_HandleFirstVotes_AlreadyVoted(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := createProtocolDriver(t, epoch)

	validVotes := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValidVotes := [][]byte{types.HexToHash32("0x23456789").Bytes()}
	signer := signing.NewEdSigner()
	msg := createFirstVote(t, signer, epoch, validVotes, pValidVotes, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	setMockAtxDbForVotes(tpd.mAtxDB, epoch)
	got := tpd.handleFirstVotes(context.TODO(), "peerID", msgBytes)
	assert.NoError(t, got)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, types.FirstRound, true)
	expected := map[string]proposalList{
		string(signer.PublicKey().Bytes()): append(validVotes, pValidVotes...),
	}
	checkFirstIncomingVotes(t, tpd.ProtocolDriver, epoch, expected)

	// the same ed key will not cause double-vote
	msg2 := createFirstVote(t, signer, epoch, validVotes, proposalList{}, false)
	msgBytes2, err := types.InterfaceToBytes(msg2)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	got = tpd.handleFirstVotes(context.TODO(), "peerID", msgBytes2)
	assert.ErrorIs(t, got, errAlreadyVoted)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, types.FirstRound, true)
	checkFirstIncomingVotes(t, tpd.ProtocolDriver, epoch, expected)
}

func Test_HandleFirstVotes_MinerMissingATX(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := createProtocolDriver(t, epoch)

	validVotes := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValidVotes := [][]byte{types.HexToHash32("0x23456789").Bytes()}
	signer := signing.NewEdSigner()
	msg := createFirstVote(t, signer, epoch, validVotes, pValidVotes, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	tpd.mAtxDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), epoch-1).Return(types.ATXID{}, sql.ErrNotFound).Times(1)
	got := tpd.handleFirstVotes(context.TODO(), "peerID", msgBytes)
	assert.ErrorIs(t, got, errMinerATXNotFound)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, types.FirstRound, true)
	checkFirstIncomingVotes(t, tpd.ProtocolDriver, epoch, map[string]proposalList{})
}

func Test_HandleFirstVotes_ATXLookupError(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := createProtocolDriver(t, epoch)

	validVotes := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValidVotes := [][]byte{types.HexToHash32("0x23456789").Bytes()}
	signer := signing.NewEdSigner()
	msg := createFirstVote(t, signer, epoch, validVotes, pValidVotes, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	errUnknown := errors.New("unknown")
	tpd.mAtxDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), epoch-1).Return(types.ATXID{}, errUnknown).Times(1)
	got := tpd.handleFirstVotes(context.TODO(), "peerID", msgBytes)
	assert.ErrorIs(t, got, errUnknown)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, types.FirstRound, true)
	checkFirstIncomingVotes(t, tpd.ProtocolDriver, epoch, map[string]proposalList{})
}

func Test_HandleFirstVotes_ATXHeaderLookupError(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := createProtocolDriver(t, epoch)

	validVotes := [][]byte{types.HexToHash32("0x12345678").Bytes(), types.HexToHash32("0x87654321").Bytes()}
	pValidVotes := [][]byte{types.HexToHash32("0x23456789").Bytes()}
	signer := signing.NewEdSigner()
	msg := createFirstVote(t, signer, epoch, validVotes, pValidVotes, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	atxID := types.RandomATXID()
	tpd.mAtxDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), epoch-1).Return(atxID, nil).Times(1)
	errUnknown := errors.New("unknown")
	tpd.mAtxDB.EXPECT().GetAtxHeader(atxID).Return(nil, errUnknown).Times(1)
	got := tpd.handleFirstVotes(context.TODO(), "peerID", msgBytes)
	assert.ErrorIs(t, got, errUnknown)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, types.FirstRound, true)
	checkFirstIncomingVotes(t, tpd.ProtocolDriver, epoch, map[string]proposalList{})
}

func Test_HandleFollowingVotes_Success(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer := signing.NewEdSigner()
	tpd, plist := createProtocolDriverWithFirstRoundVotes(t, signer, epoch, round)

	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []byte{0b101}, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	setMockAtxDbForVotes(tpd.mAtxDB, epoch)
	res := tpd.HandleFollowingVotes(context.TODO(), "peerID", msgBytes)
	assert.Equal(t, pubsub.ValidationAccept, res)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, round, true)
	expected := make(map[string]*big.Int, len(plist))
	for i, p := range plist {
		if i == 0 || i == 2 {
			expected[string(p)] = big.NewInt(10)
		} else {
			expected[string(p)] = big.NewInt(-10)
		}
	}
	checkVoteMargins(t, tpd.ProtocolDriver, epoch, expected)
}

func Test_HandleFollowingVotes_Shutdown(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer := signing.NewEdSigner()
	tpd, _ := createProtocolDriverWithFirstRoundVotes(t, signer, epoch, round)
	tpd.mClock.EXPECT().Unsubscribe(gomock.Any())
	tpd.Close()

	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []byte{0b101}, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	res := tpd.HandleFollowingVotes(context.TODO(), "peerID", msgBytes)
	assert.Equal(t, pubsub.ValidationIgnore, res)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, round, false)
	checkVoteMargins(t, tpd.ProtocolDriver, epoch, map[string]*big.Int{})
}

func Test_HandleFollowingVotes_NotInProtocol(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer := signing.NewEdSigner()
	tpd, _ := createProtocolDriverWithFirstRoundVotes(t, signer, epoch, round)

	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []byte{0b101}, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	tpd.setEndProtocol(context.TODO())
	res := tpd.HandleFollowingVotes(context.TODO(), "peerID", msgBytes)
	assert.Equal(t, pubsub.ValidationIgnore, res)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, round, false)
	checkVoteMargins(t, tpd.ProtocolDriver, epoch, map[string]*big.Int{})
}

func Test_handleFollowingVotes_CorruptMsg(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer := signing.NewEdSigner()
	tpd, _ := createProtocolDriverWithFirstRoundVotes(t, signer, epoch, round)

	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []byte{0b101}, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	got := tpd.handleFollowingVotes(context.TODO(), "peerID", msgBytes[1:], time.Now())
	assert.ErrorIs(t, got, errMalformedMessage)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, round, false)
	checkVoteMargins(t, tpd.ProtocolDriver, epoch, map[string]*big.Int{})
}

func Test_handleFollowingVotes_WrongEpoch(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer := signing.NewEdSigner()
	tpd, _ := createProtocolDriverWithFirstRoundVotes(t, signer, epoch, round)
	createEpochState(t, tpd.ProtocolDriver, epoch+1)
	createEpochState(t, tpd.ProtocolDriver, epoch-1)

	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch+1, round, []byte{0b101}, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	got := tpd.handleFollowingVotes(context.TODO(), "peerID", msgBytes, time.Now())
	assert.ErrorIs(t, got, errEpochNotActive)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, round, false)
	checkVoteMargins(t, tpd.ProtocolDriver, epoch, map[string]*big.Int{})
	checkVoted(t, tpd.ProtocolDriver, epoch+1, signer, round, false)
	checkVoteMargins(t, tpd.ProtocolDriver, epoch+1, map[string]*big.Int{})

	msg = createFollowingVote(t, signer, epoch-1, round, []byte{0b101}, false)
	msgBytes, err = types.InterfaceToBytes(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	got = tpd.handleFollowingVotes(context.TODO(), "peerID", msgBytes, time.Now())
	assert.ErrorIs(t, got, errEpochNotActive)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, round, false)
	checkVoteMargins(t, tpd.ProtocolDriver, epoch, map[string]*big.Int{})
	checkVoted(t, tpd.ProtocolDriver, epoch-1, signer, round, false)
	checkVoteMargins(t, tpd.ProtocolDriver, epoch-1, map[string]*big.Int{})
}

func Test_handleFollowingVotes_TooEarly(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer := signing.NewEdSigner()
	tpd, _ := createProtocolDriverWithFirstRoundVotes(t, signer, epoch, round)

	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []byte{0b101}, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	tpd.setRoundInProgress(round - 1)
	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	got := tpd.handleFollowingVotes(context.TODO(), "peerID", msgBytes, time.Now())
	assert.ErrorIs(t, got, errUntimelyMessage)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, round, false)
	checkVoteMargins(t, tpd.ProtocolDriver, epoch, map[string]*big.Int{})
}

func Test_handleFollowingVotes_FailedToExtractPK(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer := signing.NewEdSigner()
	tpd, _ := createProtocolDriverWithFirstRoundVotes(t, signer, epoch, round)

	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []byte{0b101}, true)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	got := tpd.handleFollowingVotes(context.TODO(), "peerID", msgBytes, time.Now())
	assert.Contains(t, got.Error(), "bad signature format")
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, round, false)
	checkVoteMargins(t, tpd.ProtocolDriver, epoch, map[string]*big.Int{})
}

func Test_handleFollowingVotes_AlreadyVoted(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer := signing.NewEdSigner()
	tpd, plist := createProtocolDriverWithFirstRoundVotes(t, signer, epoch, round)

	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []byte{0b101}, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	setMockAtxDbForVotes(tpd.mAtxDB, epoch)
	got := tpd.handleFollowingVotes(context.TODO(), "peerID", msgBytes, time.Now())
	assert.NoError(t, got)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, round, true)
	expected := make(map[string]*big.Int, len(plist))
	for i, p := range plist {
		if i == 0 || i == 2 {
			expected[string(p)] = big.NewInt(10)
		} else {
			expected[string(p)] = big.NewInt(-10)
		}
	}
	checkVoteMargins(t, tpd.ProtocolDriver, epoch, expected)

	// now vote again
	msg = createFollowingVote(t, signer, epoch, round, []byte{0b111}, false)
	msgBytes, err = types.InterfaceToBytes(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	got = tpd.handleFollowingVotes(context.TODO(), "peerID", msgBytes, time.Now())
	assert.ErrorIs(t, got, errAlreadyVoted)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, round, true)
	checkVoteMargins(t, tpd.ProtocolDriver, epoch, expected)
}

func Test_handleFollowingVotes_MinerMissingATX(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer := signing.NewEdSigner()
	tpd, _ := createProtocolDriverWithFirstRoundVotes(t, signer, epoch, round)

	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []byte{0b101}, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	tpd.mAtxDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), epoch-1).
		Return(types.ATXID{}, sql.ErrNotFound).Times(1)
	got := tpd.handleFollowingVotes(context.TODO(), "peerID", msgBytes, time.Now())
	assert.ErrorIs(t, got, errMinerATXNotFound)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, round, true)
	checkVoteMargins(t, tpd.ProtocolDriver, epoch, map[string]*big.Int{})
}

func Test_handleFollowingVotes_ATXLookupError(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer := signing.NewEdSigner()
	tpd, _ := createProtocolDriverWithFirstRoundVotes(t, signer, epoch, round)

	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []byte{0b101}, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	errUnknown := errors.New("unknown")
	tpd.mAtxDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), epoch-1).
		Return(types.ATXID{}, errUnknown).Times(1)
	got := tpd.handleFollowingVotes(context.TODO(), "peerID", msgBytes, time.Now())
	assert.ErrorIs(t, got, errUnknown)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, round, true)
	checkVoteMargins(t, tpd.ProtocolDriver, epoch, map[string]*big.Int{})
}

func Test_handleFollowingVotes_ATXHeaderLookupError(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer := signing.NewEdSigner()
	tpd, _ := createProtocolDriverWithFirstRoundVotes(t, signer, epoch, round)

	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []byte{0b101}, false)
	msgBytes, err := types.InterfaceToBytes(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().GetCurrentLayer().Return(epoch.FirstLayer()).Times(1)
	atxID := types.RandomATXID()
	tpd.mAtxDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), epoch-1).Return(atxID, nil).Times(1)
	errUnknown := errors.New("unknown")
	tpd.mAtxDB.EXPECT().GetAtxHeader(atxID).Return(nil, errUnknown).Times(1)
	got := tpd.handleFollowingVotes(context.TODO(), "peerID", msgBytes, time.Now())
	assert.ErrorIs(t, got, errUnknown)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, round, true)
	checkVoteMargins(t, tpd.ProtocolDriver, epoch, map[string]*big.Int{})
}

func Test_UniqueFollowingVotingMessages(t *testing.T) {
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
