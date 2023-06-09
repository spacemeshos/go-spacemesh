package beacon

import (
	"context"
	"fmt"
	"math/big"
	"math/rand"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
)

const epochWeight = uint64(100)

func createProtocolDriverWithFirstRoundVotes(
	t *testing.T,
	signer *signing.EdSigner,
	epoch types.EpochID,
	round types.RoundID,
) (*testProtocolDriver, proposalList) {
	tpd := setUpProtocolDriver(t)
	tpd.setBeginProtocol(context.Background())
	id := createATX(t, tpd.cdb, epoch.FirstLayer().Sub(1), signer, 10, time.Now())
	minerAtxs := map[types.NodeID]types.ATXID{signer.NodeID(): id}
	createEpochState(t, tpd.ProtocolDriver, epoch, minerAtxs, nil)
	plist := make(proposalList, 3)
	for i := range plist {
		copy(plist[i][:], types.RandomBytes(types.BeaconSize))
	}
	setOwnFirstRoundVotes(t, tpd.ProtocolDriver, epoch, plist)
	setMinerFirstRoundVotes(t, tpd.ProtocolDriver, epoch, signer.NodeID(), plist)
	tpd.setRoundInProgress(round)
	return tpd, plist
}

func createEpochState(tb testing.TB, pd *ProtocolDriver, epoch types.EpochID, minerAtxs map[types.NodeID]types.ATXID, checker eligibilityChecker) {
	tb.Helper()
	pd.mu.Lock()
	defer pd.mu.Unlock()
	pd.states[epoch] = newState(pd.logger, pd.config, nil, epochWeight, minerAtxs, checker)
}

func setOwnFirstRoundVotes(t *testing.T, pd *ProtocolDriver, epoch types.EpochID, ownFirstRound proposalList) {
	t.Helper()
	pd.mu.Lock()
	defer pd.mu.Unlock()
	for _, p := range ownFirstRound {
		pd.states[epoch].addValidProposal(p)
	}
}

func setMinerFirstRoundVotes(t *testing.T, pd *ProtocolDriver, epoch types.EpochID, minerID types.NodeID, minerFirstRound proposalList) {
	t.Helper()
	pd.mu.Lock()
	defer pd.mu.Unlock()
	pd.states[epoch].setMinerFirstRoundVote(minerID, minerFirstRound)
}

func createProposal(t *testing.T, vrfSigner *signing.VRFSigner, epoch types.EpochID, corruptSignature bool) *ProposalMessage {
	sig := buildSignedProposal(context.Background(), logtest.New(t), vrfSigner, epoch, types.VRFPostIndex(rand.Uint64()))
	msg := &ProposalMessage{
		NodeID:       vrfSigner.NodeID(),
		EpochID:      epoch,
		VRFSignature: sig,
	}
	if corruptSignature {
		msg.VRFSignature = vrfSigner.Sign(types.RandomBytes(32))
	}
	return msg
}

func setEarliestProposalTime(pd *ProtocolDriver, t time.Time) {
	pd.mu.Lock()
	defer pd.mu.Unlock()
	pd.earliestProposalTime = t
}

func checkProposed(t *testing.T, pd *ProtocolDriver, epoch types.EpochID, nodeID types.NodeID, expected bool) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	if _, ok := pd.states[epoch]; ok {
		_, proposed := pd.states[epoch].hasProposed[nodeID]
		require.Equal(t, expected, proposed)
	} else {
		require.False(t, expected)
	}
}

func checkProposals(t *testing.T, pd *ProtocolDriver, epoch types.EpochID, expected proposals) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	if _, ok := pd.states[epoch]; ok {
		require.EqualValues(t, expected, pd.states[epoch].incomingProposals)
	} else {
		require.Equal(t, expected, proposals{})
	}
}

func createFirstVote(t *testing.T, signer *signing.EdSigner, epoch types.EpochID, valid proposalList, pValid proposalList, corruptSignature bool) *FirstVotingMessage {
	logger := logtest.New(t)
	msg := &FirstVotingMessage{
		FirstVotingMessageBody: FirstVotingMessageBody{
			EpochID:                   epoch,
			ValidProposals:            valid,
			PotentiallyValidProposals: pValid,
		},
	}
	encoded, err := codec.Encode(&msg.FirstVotingMessageBody)
	if err != nil {
		logger.With().Panic("failed to serialize message for signing", log.Err(err))
	}
	msg.Signature = signer.Sign(signing.BEACON_FIRST_MSG, encoded)
	if corruptSignature {
		msg.Signature = signer.Sign(signing.BEACON_FIRST_MSG, encoded[1:])
	}
	msg.SmesherID = signer.NodeID()
	return msg
}

func checkVoted(t *testing.T, pd *ProtocolDriver, epoch types.EpochID, signer *signing.EdSigner, round types.RoundID, voted bool) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	require.NotNil(t, pd.states[epoch])
	tracker, exists := pd.states[epoch].hasVoted[signer.NodeID()]
	if !exists {
		require.Equal(t, voted, exists)
	} else {
		require.Equal(t, voted, tracker.voted(round))
	}
}

func checkFirstIncomingVotes(t *testing.T, pd *ProtocolDriver, epoch types.EpochID, expected map[types.NodeID]proposalList) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	require.NotNil(t, pd.states[epoch])
	require.EqualValues(t, expected, pd.states[epoch].firstRoundIncomingVotes)
}

func createFollowingVote(t *testing.T, signer *signing.EdSigner, epoch types.EpochID, round types.RoundID, bitVector []byte, corruptSignature bool) *FollowingVotingMessage {
	msg := &FollowingVotingMessage{
		FollowingVotingMessageBody: FollowingVotingMessageBody{
			EpochID:        epoch,
			RoundID:        round,
			VotesBitVector: bitVector,
		},
	}
	logger := logtest.New(t)
	encoded, err := codec.Encode(&msg.FollowingVotingMessageBody)
	if err != nil {
		logger.With().Panic("failed to serialize message for signing", log.Err(err))
	}
	msg.Signature = signer.Sign(signing.BEACON_FOLLOWUP_MSG, encoded)
	if corruptSignature {
		msg.Signature = signer.Sign(signing.BEACON_FOLLOWUP_MSG, encoded[1:])
	}
	msg.SmesherID = signer.NodeID()
	return msg
}

func checkVoteMargins(t *testing.T, pd *ProtocolDriver, epoch types.EpochID, expected map[Proposal]*big.Int) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	require.NotNil(t, pd.states[epoch])
	require.EqualValues(t, expected, pd.states[epoch].votesMargin)
}

func emptyVoteMargins(plist proposalList) map[Proposal]*big.Int {
	vm := make(map[Proposal]*big.Int, len(plist))
	for _, p := range plist {
		vm[p] = new(big.Int)
	}
	return vm
}

func Test_HandleProposal_InitEpoch(t *testing.T) {
	const epoch = types.EpochID(10)
	tpd := setUpProtocolDriver(t)
	tpd.setBeginProtocol(context.Background())

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	vrfSigner, err := signer.VRFSigner()
	require.NoError(t, err)
	epochStart := time.Now()
	createATX(t, tpd.cdb, epoch.FirstLayer().Sub(1), signer, 10, epochStart.Add(-1*time.Minute))

	msg := createProposal(t, vrfSigner, epoch, false)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	tpd.mClock.EXPECT().LayerToTime(epoch.FirstLayer()).Return(epochStart).AnyTimes()
	res := tpd.HandleProposal(context.Background(), "peerID", msgBytes)
	require.Equal(t, nil, res)
}

func Test_HandleProposal_Success(t *testing.T) {
	if util.IsWindows() && util.IsCi() {
		t.Skip("Skipping test in Windows on CI (https://github.com/spacemeshos/go-spacemesh/issues/3630)")
	}
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := setUpProtocolDriver(t)
	tpd.setBeginProtocol(context.Background())

	signer1, err := signing.NewEdSigner()
	require.NoError(t, err)
	vrfSigner1, err := signer1.VRFSigner()
	require.NoError(t, err)

	signer2, err := signing.NewEdSigner()
	require.NoError(t, err)
	vrfSigner2, err := signer2.VRFSigner()
	require.NoError(t, err)

	epochStart := time.Now()
	mockChecker := NewMockeligibilityChecker(gomock.NewController(t))
	minerAtxs := map[types.NodeID]types.ATXID{
		signer1.NodeID(): createATX(t, tpd.cdb, epoch.FirstLayer().Sub(1), signer1, 10, epochStart.Add(-1*time.Minute)),
		signer2.NodeID(): createATX(t, tpd.cdb, epoch.FirstLayer().Sub(1), signer2, 10, epochStart.Add(-2*time.Minute)),
	}
	createEpochState(t, tpd.ProtocolDriver, epoch, minerAtxs, mockChecker)

	msg1 := createProposal(t, vrfSigner1, epoch, false)
	msgBytes1, err := codec.Encode(msg1)
	require.NoError(t, err)

	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	tpd.mClock.EXPECT().LayerToTime(epoch.FirstLayer()).Return(epochStart).AnyTimes()
	mockChecker.EXPECT().PassStrictThreshold(gomock.Any()).Return(true)
	res := tpd.HandleProposal(context.Background(), "peerID", msgBytes1)
	require.Equal(t, nil, res)

	require.NoError(t, tpd.markProposalPhaseFinished(epoch, time.Now()))

	msg2 := createProposal(t, vrfSigner2, epoch, false)
	msgBytes2, err := codec.Encode(msg2)
	require.NoError(t, err)

	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	// tpd.mClock.EXPECT().LayerToTime(epoch.FirstLayer()).Return(epochStart)
	mockChecker.EXPECT().PassThreshold(gomock.Any()).Return(true)
	res = tpd.HandleProposal(context.Background(), "peerID", msgBytes2)
	require.Equal(t, nil, res)

	p1 := ProposalFromVrf(msg1.VRFSignature)
	p2 := ProposalFromVrf(msg2.VRFSignature)
	checkProposed(t, tpd.ProtocolDriver, epoch, vrfSigner1.NodeID(), true)
	expectedProposals := proposals{
		valid:            proposalSet{p1: struct{}{}},
		potentiallyValid: proposalSet{p2: struct{}{}},
	}
	checkProposals(t, tpd.ProtocolDriver, epoch, expectedProposals)
}

func Test_HandleProposal_Shutdown(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := setUpProtocolDriver(t)
	tpd.setBeginProtocol(context.Background())
	tpd.Close()

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	vrfSigner, err := signer.VRFSigner()
	require.NoError(t, err)

	msg := createProposal(t, vrfSigner, epoch, false)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)

	res := tpd.HandleProposal(context.Background(), "peerID", msgBytes)
	require.Error(t, res)

	checkProposed(t, tpd.ProtocolDriver, epoch, vrfSigner.NodeID(), false)
	checkProposals(t, tpd.ProtocolDriver, epoch, proposals{})
}

func Test_HandleProposal_NotInProtocolStillWorks(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := setUpProtocolDriver(t)
	tpd.setBeginProtocol(context.Background())

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	vrfSigner, err := signer.VRFSigner()
	require.NoError(t, err)

	msg := createProposal(t, vrfSigner, epoch, false)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)

	epochStart := time.Now()
	mockChecker := NewMockeligibilityChecker(gomock.NewController(t))
	minerAtxs := map[types.NodeID]types.ATXID{
		signer.NodeID(): createATX(t, tpd.cdb, epoch.FirstLayer().Sub(1), signer, 10, epochStart.Add(-1*time.Minute)),
	}
	createEpochState(t, tpd.ProtocolDriver, epoch, minerAtxs, mockChecker)

	tpd.setEndProtocol(context.Background())
	require.False(t, tpd.isInProtocol())
	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	tpd.mClock.EXPECT().LayerToTime(epoch.FirstLayer()).Return(epochStart).AnyTimes()
	mockChecker.EXPECT().PassStrictThreshold(gomock.Any()).Return(true)
	res := tpd.HandleProposal(context.Background(), "peerID", msgBytes)
	require.Equal(t, nil, res)

	p := ProposalFromVrf(msg.VRFSignature)
	checkProposed(t, tpd.ProtocolDriver, epoch, vrfSigner.NodeID(), true)
	expectedProposals := proposals{
		valid: proposalSet{p: struct{}{}},
	}
	checkProposals(t, tpd.ProtocolDriver, epoch, expectedProposals)
}

func Test_handleProposal_Corrupted(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := setUpProtocolDriver(t)
	tpd.setBeginProtocol(context.Background())

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	vrfSigner, err := signer.VRFSigner()
	require.NoError(t, err)

	msg := []byte("guaranteed to be  malformed")
	got := tpd.HandleProposal(context.Background(), "peerID", msg)
	require.ErrorIs(t, got, errMalformedMessage)

	checkProposed(t, tpd.ProtocolDriver, epoch, vrfSigner.NodeID(), false)
	checkProposals(t, tpd.ProtocolDriver, epoch, proposals{})
}

func Test_handleProposal_EpochTooOld(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := setUpProtocolDriver(t)
	tpd.setBeginProtocol(context.Background())

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	vrfSigner, err := signer.VRFSigner()
	require.NoError(t, err)

	msg := createProposal(t, vrfSigner, epoch-1, false)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	tpd.mClock.EXPECT().LayerToTime(gomock.Any()).Return(time.Now()).AnyTimes()
	got := tpd.HandleProposal(context.Background(), "peerID", msgBytes)
	require.ErrorIs(t, got, errUntimelyMessage)

	checkProposed(t, tpd.ProtocolDriver, epoch, vrfSigner.NodeID(), false)
	checkProposals(t, tpd.ProtocolDriver, epoch, proposals{})
}

func Test_handleProposal_NextEpoch(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const nextEpoch = epoch + 1
	tpd := setUpProtocolDriver(t)
	tpd.setBeginProtocol(context.Background())

	rng := rand.New(rand.NewSource(1))
	signer, err := signing.NewEdSigner(signing.WithKeyFromRand(rng))
	require.NoError(t, err)
	vrfSigner, err := signer.VRFSigner()
	require.NoError(t, err)

	now := time.Now()
	mockChecker := NewMockeligibilityChecker(gomock.NewController(t))
	minerAtxs := map[types.NodeID]types.ATXID{
		signer.NodeID(): createATX(t, tpd.cdb, epoch.FirstLayer().Sub(1), signer, 10, now),
	}
	createEpochState(t, tpd.ProtocolDriver, nextEpoch, minerAtxs, mockChecker)

	msg := createProposal(t, vrfSigner, nextEpoch, false)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)

	setEarliestProposalTime(tpd.ProtocolDriver, now.Add(-1*time.Second))
	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer()).AnyTimes()
	tpd.mClock.EXPECT().LayerToTime((nextEpoch).FirstLayer()).Return(time.Now()).AnyTimes()
	mockChecker.EXPECT().PassStrictThreshold(gomock.Any()).Return(true)
	got := tpd.HandleProposal(context.Background(), "peerID", msgBytes)
	require.NoError(t, got)

	// nothing added to the current epoch
	checkProposed(t, tpd.ProtocolDriver, epoch, vrfSigner.NodeID(), false)
	checkProposals(t, tpd.ProtocolDriver, epoch, proposals{})

	// proposal added to the next epoch
	p := ProposalFromVrf(msg.VRFSignature)
	checkProposed(t, tpd.ProtocolDriver, nextEpoch, vrfSigner.NodeID(), true)
	expectedProposals := proposals{
		valid: proposalSet{p: struct{}{}},
	}
	checkProposals(t, tpd.ProtocolDriver, nextEpoch, expectedProposals)
}

func Test_handleProposal_NextEpochTooEarly(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const nextEpoch = epoch + 1
	tpd := setUpProtocolDriver(t)
	tpd.setBeginProtocol(context.Background())

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	vrfSigner, err := signer.VRFSigner()
	require.NoError(t, err)

	msg := createProposal(t, vrfSigner, nextEpoch, false)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)

	now := time.Now()
	setEarliestProposalTime(tpd.ProtocolDriver, now.Add(1*time.Second))
	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer()).AnyTimes()
	tpd.mClock.EXPECT().LayerToTime(gomock.Any()).Return(time.Now()).AnyTimes()
	got := tpd.HandleProposal(context.Background(), "peerID", msgBytes)
	require.ErrorIs(t, got, errUntimelyMessage)

	// nothing added to the current epoch
	checkProposed(t, tpd.ProtocolDriver, epoch, vrfSigner.NodeID(), false)
	checkProposals(t, tpd.ProtocolDriver, epoch, proposals{})

	// proposal added to the next epoch
	checkProposed(t, tpd.ProtocolDriver, nextEpoch, vrfSigner.NodeID(), false)
	checkProposals(t, tpd.ProtocolDriver, nextEpoch, proposals{})
}

func Test_handleProposal_EpochTooFarAhead(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := setUpProtocolDriver(t)
	tpd.setBeginProtocol(context.Background())

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	vrfSigner, err := signer.VRFSigner()
	require.NoError(t, err)

	msg := createProposal(t, vrfSigner, epoch+2, false)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	tpd.mClock.EXPECT().LayerToTime(gomock.Any()).Return(time.Now()).AnyTimes()
	got := tpd.HandleProposal(context.Background(), "peerID", msgBytes)
	require.ErrorIs(t, got, errUntimelyMessage)

	checkProposed(t, tpd.ProtocolDriver, epoch, vrfSigner.NodeID(), false)
	checkProposals(t, tpd.ProtocolDriver, epoch, proposals{})
}

func Test_handleProposal_BadVrfSignature(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := setUpProtocolDriver(t)
	tpd.setBeginProtocol(context.Background())

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	vrfSigner, err := signer.VRFSigner()
	require.NoError(t, err)

	epochStart := time.Now()
	mockChecker := NewMockeligibilityChecker(gomock.NewController(t))
	minerAtxs := map[types.NodeID]types.ATXID{
		signer.NodeID(): createATX(t, tpd.cdb, epoch.FirstLayer().Sub(1), signer, 10, epochStart.Add(-1*time.Minute)),
	}
	createEpochState(t, tpd.ProtocolDriver, epoch, minerAtxs, mockChecker)

	msg := createProposal(t, vrfSigner, epoch, true)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)

	mVerifier := NewMockvrfVerifier(tpd.ctrl)
	mVerifier.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any()).Return(false)
	tpd.vrfVerifier = mVerifier

	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	tpd.mClock.EXPECT().LayerToTime(gomock.Any()).Return(time.Now()).AnyTimes()
	got := tpd.HandleProposal(context.Background(), "peerID", msgBytes)
	require.ErrorIs(t, got, errVRFNotVerified)

	checkProposed(t, tpd.ProtocolDriver, epoch, vrfSigner.NodeID(), false)
	checkProposals(t, tpd.ProtocolDriver, epoch, proposals{})
}

func Test_handleProposal_AlreadyProposed(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := setUpProtocolDriver(t)
	tpd.setBeginProtocol(context.Background())

	rng := rand.New(rand.NewSource(101))
	signer, err := signing.NewEdSigner(signing.WithKeyFromRand(rng))
	require.NoError(t, err)
	vrfSigner, err := signer.VRFSigner()
	require.NoError(t, err)

	epochStart := time.Now()
	mockChecker := NewMockeligibilityChecker(gomock.NewController(t))
	minerAtxs := map[types.NodeID]types.ATXID{
		signer.NodeID(): createATX(t, tpd.cdb, epoch.FirstLayer().Sub(1), signer, 10, epochStart.Add(-1*time.Minute)),
	}
	createEpochState(t, tpd.ProtocolDriver, epoch, minerAtxs, mockChecker)

	msg1 := createProposal(t, vrfSigner, epoch, false)
	msgBytes1, err := codec.Encode(msg1)
	require.NoError(t, err)

	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	tpd.mClock.EXPECT().LayerToTime(epoch.FirstLayer()).Return(epochStart).AnyTimes()
	mockChecker.EXPECT().PassStrictThreshold(gomock.Any()).Return(true)
	got := tpd.HandleProposal(context.Background(), "peerID", msgBytes1)
	require.NoError(t, got)

	p := ProposalFromVrf(msg1.VRFSignature)
	checkProposed(t, tpd.ProtocolDriver, epoch, vrfSigner.NodeID(), true)
	expectedProposals := proposals{
		valid: proposalSet{p: struct{}{}},
	}
	checkProposals(t, tpd.ProtocolDriver, epoch, expectedProposals)

	// the same vrf key will not cause double-proposal
	msg2 := createProposal(t, vrfSigner, epoch, false)
	msgBytes2, err := codec.Encode(msg2)
	require.NoError(t, err)

	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	got = tpd.HandleProposal(context.Background(), "peerID", msgBytes2)
	require.ErrorIs(t, got, errAlreadyProposed)

	checkProposed(t, tpd.ProtocolDriver, epoch, vrfSigner.NodeID(), true)
	checkProposals(t, tpd.ProtocolDriver, epoch, expectedProposals)
}

func Test_handleProposal_PotentiallyValid_Timing(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := setUpProtocolDriver(t)
	tpd.setBeginProtocol(context.Background())

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	vrfSigner, err := signer.VRFSigner()
	require.NoError(t, err)

	epochStart := time.Now()
	mockChecker := NewMockeligibilityChecker(gomock.NewController(t))
	minerAtxs := map[types.NodeID]types.ATXID{
		signer.NodeID(): createATX(t, tpd.cdb, epoch.FirstLayer().Sub(1), signer, 10, epochStart.Add(tpd.config.GracePeriodDuration)),
	}
	createEpochState(t, tpd.ProtocolDriver, epoch, minerAtxs, mockChecker)

	msg := createProposal(t, vrfSigner, epoch, false)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)
	p := ProposalFromVrf(msg.VRFSignature)
	expectedProposals := proposals{
		potentiallyValid: proposalSet{p: struct{}{}},
	}

	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	tpd.mClock.EXPECT().LayerToTime(epoch.FirstLayer()).Return(epochStart).AnyTimes()
	mockChecker.EXPECT().PassThreshold(gomock.Any()).Return(true)
	got := tpd.HandleProposal(context.Background(), "peerID", msgBytes)
	require.NoError(t, got)

	checkProposed(t, tpd.ProtocolDriver, epoch, signer.NodeID(), true)
	checkProposals(t, tpd.ProtocolDriver, epoch, expectedProposals)
}

func Test_handleProposal_PotentiallyValid_Threshold(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := setUpProtocolDriver(t)
	tpd.setBeginProtocol(context.Background())

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	vrfSigner, err := signer.VRFSigner()
	require.NoError(t, err)

	epochStart := time.Now()
	mockChecker := NewMockeligibilityChecker(gomock.NewController(t))
	minerAtxs := map[types.NodeID]types.ATXID{
		signer.NodeID(): createATX(t, tpd.cdb, epoch.FirstLayer().Sub(1), signer, 10, epochStart.Add(-1*time.Minute)),
	}
	createEpochState(t, tpd.ProtocolDriver, epoch, minerAtxs, mockChecker)

	msg := createProposal(t, vrfSigner, epoch, false)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)
	p := ProposalFromVrf(msg.VRFSignature)
	expectedProposals := proposals{
		potentiallyValid: proposalSet{p: struct{}{}},
	}

	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	tpd.mClock.EXPECT().LayerToTime(epoch.FirstLayer()).Return(epochStart).AnyTimes()
	mockChecker.EXPECT().PassStrictThreshold(gomock.Any()).Return(false)
	mockChecker.EXPECT().PassThreshold(gomock.Any()).Return(true)
	got := tpd.HandleProposal(context.Background(), "peerID", msgBytes)
	require.NoError(t, got)

	checkProposed(t, tpd.ProtocolDriver, epoch, signer.NodeID(), true)
	checkProposals(t, tpd.ProtocolDriver, epoch, expectedProposals)
}

func Test_handleProposal_Invalid_Timing(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := setUpProtocolDriver(t)
	tpd.setBeginProtocol(context.Background())

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	vrfSigner, err := signer.VRFSigner()
	require.NoError(t, err)

	epochStart := time.Now()
	mockChecker := NewMockeligibilityChecker(gomock.NewController(t))
	minerAtxs := map[types.NodeID]types.ATXID{
		signer.NodeID(): createATX(t, tpd.cdb, epoch.FirstLayer().Sub(1), signer, 10, epochStart.Add(2*tpd.config.GracePeriodDuration)),
	}
	createEpochState(t, tpd.ProtocolDriver, epoch, minerAtxs, mockChecker)

	msg := createProposal(t, vrfSigner, epoch, false)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	tpd.mClock.EXPECT().LayerToTime(epoch.FirstLayer()).Return(epochStart).AnyTimes()
	got := tpd.HandleProposal(context.Background(), "peerID", msgBytes)
	require.NoError(t, got)

	checkProposed(t, tpd.ProtocolDriver, epoch, signer.NodeID(), true)
	checkProposals(t, tpd.ProtocolDriver, epoch, proposals{})
}

func Test_handleProposal_Invalid_threshold(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := setUpProtocolDriver(t)
	tpd.setBeginProtocol(context.Background())

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	vrfSigner, err := signer.VRFSigner()
	require.NoError(t, err)

	epochStart := time.Now()
	mockChecker := NewMockeligibilityChecker(gomock.NewController(t))
	minerAtxs := map[types.NodeID]types.ATXID{
		signer.NodeID(): createATX(t, tpd.cdb, epoch.FirstLayer().Sub(1), signer, 10, epochStart.Add(-1*time.Minute)),
	}
	createEpochState(t, tpd.ProtocolDriver, epoch, minerAtxs, mockChecker)

	msg := createProposal(t, vrfSigner, epoch, false)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	tpd.mClock.EXPECT().LayerToTime(epoch.FirstLayer()).Return(epochStart).AnyTimes()
	mockChecker.EXPECT().PassStrictThreshold(gomock.Any()).Return(false)
	mockChecker.EXPECT().PassThreshold(gomock.Any()).Return(false)
	got := tpd.HandleProposal(context.Background(), "peerID", msgBytes)
	require.NoError(t, got)

	checkProposed(t, tpd.ProtocolDriver, epoch, signer.NodeID(), true)
	checkProposals(t, tpd.ProtocolDriver, epoch, proposals{})
}

func Test_handleProposal_MinerMissingATX(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := setUpProtocolDriver(t)
	tpd.setBeginProtocol(context.Background())

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	vrfSigner, err := signer.VRFSigner()
	require.NoError(t, err)

	epochStart := time.Now()
	mockChecker := NewMockeligibilityChecker(gomock.NewController(t))
	minerAtxs := map[types.NodeID]types.ATXID{}
	createEpochState(t, tpd.ProtocolDriver, epoch, minerAtxs, mockChecker)

	msg := createProposal(t, vrfSigner, epoch, false)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)
	p := ProposalFromVrf(msg.VRFSignature)
	expectedProposals := proposals{
		potentiallyValid: proposalSet{p: struct{}{}},
	}

	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	tpd.mClock.EXPECT().LayerToTime(gomock.Any()).Return(epochStart).AnyTimes()
	got := tpd.HandleProposal(context.Background(), "peerID", msgBytes)
	require.ErrorIs(t, got, errMinerNotActive)

	checkProposed(t, tpd.ProtocolDriver, epoch, vrfSigner.NodeID(), false)
	checkProposals(t, tpd.ProtocolDriver, epoch, proposals{})

	id := createATX(t, tpd.cdb, epoch.FirstLayer().Sub(1), signer, 10, epochStart.Add(tpd.config.GracePeriodDuration))
	hdr, err := tpd.cdb.GetAtxHeader(id)
	require.NoError(t, err)
	tpd.OnAtx(hdr)
	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	mockChecker.EXPECT().PassThreshold(gomock.Any()).Return(true)
	got = tpd.HandleProposal(context.Background(), "peerID", msgBytes)
	require.NoError(t, got)
	checkProposed(t, tpd.ProtocolDriver, epoch, signer.NodeID(), true)
	checkProposals(t, tpd.ProtocolDriver, epoch, expectedProposals)
}

func Test_HandleFirstVotes_Success(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := setUpProtocolDriver(t)
	tpd.setBeginProtocol(context.Background())

	validVotes := []Proposal{{0x12, 0x34, 0x56, 0x78}, {0x87, 0x65, 0x43, 0x21}}
	pValidVotes := []Proposal{{0x23, 0x45, 0x67, 0x89}}

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	minerAtxs := map[types.NodeID]types.ATXID{
		signer.NodeID(): createATX(t, tpd.cdb, epoch.FirstLayer().Sub(1), signer, 10, time.Now()),
	}
	createEpochState(t, tpd.ProtocolDriver, epoch, minerAtxs, nil)

	msg := createFirstVote(t, signer, epoch, validVotes, pValidVotes, false)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	tpd.mClock.EXPECT().LayerToTime(gomock.Any()).Return(time.Now()).AnyTimes()
	res := tpd.HandleFirstVotes(context.Background(), "peerID", msgBytes)
	require.Equal(t, nil, res)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, types.FirstRound, true)
	expected := map[types.NodeID]proposalList{
		signer.NodeID(): append(validVotes, pValidVotes...),
	}
	checkFirstIncomingVotes(t, tpd.ProtocolDriver, epoch, expected)
}

func Test_HandleFirstVotes_Shutdown(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := setUpProtocolDriver(t)
	tpd.setBeginProtocol(context.Background())
	tpd.Close()

	validVotes := []Proposal{{0x12, 0x34, 0x56, 0x78}, {0x87, 0x65, 0x43, 0x21}}
	pValidVotes := []Proposal{{0x23, 0x45, 0x67, 0x89}}
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	minerAtxs := map[types.NodeID]types.ATXID{
		signer.NodeID(): createATX(t, tpd.cdb, epoch.FirstLayer().Sub(1), signer, 10, time.Now()),
	}
	createEpochState(t, tpd.ProtocolDriver, epoch, minerAtxs, nil)

	msg := createFirstVote(t, signer, epoch, validVotes, pValidVotes, false)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)

	res := tpd.HandleFirstVotes(context.Background(), "peerID", msgBytes)
	require.Error(t, res)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, types.FirstRound, false)
	checkFirstIncomingVotes(t, tpd.ProtocolDriver, epoch, map[types.NodeID]proposalList{})
}

func Test_HandleFirstVotes_NotInProtocol(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := setUpProtocolDriver(t)
	tpd.setBeginProtocol(context.Background())

	validVotes := []Proposal{{0x12, 0x34, 0x56, 0x78}, {0x87, 0x65, 0x43, 0x21}}
	pValidVotes := []Proposal{{0x23, 0x45, 0x67, 0x89}}
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	minerAtxs := map[types.NodeID]types.ATXID{
		signer.NodeID(): createATX(t, tpd.cdb, epoch.FirstLayer().Sub(1), signer, 10, time.Now()),
	}
	createEpochState(t, tpd.ProtocolDriver, epoch, minerAtxs, nil)
	msg := createFirstVote(t, signer, epoch, validVotes, pValidVotes, false)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)

	tpd.setEndProtocol(context.Background())
	res := tpd.HandleFirstVotes(context.Background(), "peerID", msgBytes)
	require.Error(t, res)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, types.FirstRound, false)
	checkFirstIncomingVotes(t, tpd.ProtocolDriver, epoch, map[types.NodeID]proposalList{})
}

func Test_handleFirstVotes_CorruptMsg(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := setUpProtocolDriver(t)
	tpd.setBeginProtocol(context.Background())

	validVotes := []Proposal{{0x12, 0x34, 0x56, 0x78}, {0x87, 0x65, 0x43, 0x21}}
	pValidVotes := []Proposal{{0x23, 0x45, 0x67, 0x89}}
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	minerAtxs := map[types.NodeID]types.ATXID{
		signer.NodeID(): createATX(t, tpd.cdb, epoch.FirstLayer().Sub(1), signer, 10, time.Now()),
	}
	createEpochState(t, tpd.ProtocolDriver, epoch, minerAtxs, nil)
	msg := createFirstVote(t, signer, epoch, validVotes, pValidVotes, false)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)

	got := tpd.HandleFirstVotes(context.Background(), "peerID", msgBytes[1:])
	require.ErrorIs(t, got, errMalformedMessage)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, types.FirstRound, false)
	checkFirstIncomingVotes(t, tpd.ProtocolDriver, epoch, map[types.NodeID]proposalList{})
}

func Test_handleFirstVotes_WrongEpoch(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := setUpProtocolDriver(t)
	tpd.setBeginProtocol(context.Background())

	validVotes := []Proposal{{0x12, 0x34, 0x56, 0x78}, {0x87, 0x65, 0x43, 0x21}}
	pValidVotes := []Proposal{{0x23, 0x45, 0x67, 0x89}}
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	minerAtxs := map[types.NodeID]types.ATXID{
		signer.NodeID(): createATX(t, tpd.cdb, epoch.FirstLayer().Sub(1), signer, 10, time.Now()),
	}
	createEpochState(t, tpd.ProtocolDriver, epoch-1, minerAtxs, nil)
	createEpochState(t, tpd.ProtocolDriver, epoch, minerAtxs, nil)
	createEpochState(t, tpd.ProtocolDriver, epoch+1, minerAtxs, nil)
	msg := createFirstVote(t, signer, epoch+1, validVotes, pValidVotes, false)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().LayerToTime(gomock.Any()).Return(time.Now()).AnyTimes()
	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	got := tpd.HandleFirstVotes(context.Background(), "peerID", msgBytes)
	require.ErrorIs(t, got, errEpochNotActive)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, types.FirstRound, false)
	checkFirstIncomingVotes(t, tpd.ProtocolDriver, epoch, map[types.NodeID]proposalList{})
	checkVoted(t, tpd.ProtocolDriver, epoch+1, signer, types.FirstRound, false)
	checkFirstIncomingVotes(t, tpd.ProtocolDriver, epoch+1, map[types.NodeID]proposalList{})

	msg = createFirstVote(t, signer, epoch-1, validVotes, pValidVotes, false)
	msgBytes, err = codec.Encode(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	got = tpd.HandleFirstVotes(context.Background(), "peerID", msgBytes)
	require.ErrorIs(t, got, errEpochNotActive)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, types.FirstRound, false)
	checkFirstIncomingVotes(t, tpd.ProtocolDriver, epoch, map[types.NodeID]proposalList{})
	checkVoted(t, tpd.ProtocolDriver, epoch-1, signer, types.FirstRound, false)
	checkFirstIncomingVotes(t, tpd.ProtocolDriver, epoch-1, map[types.NodeID]proposalList{})
}

func Test_handleFirstVotes_TooLate(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := setUpProtocolDriver(t)
	tpd.setBeginProtocol(context.Background())

	validVotes := []Proposal{{0x12, 0x34, 0x56, 0x78}, {0x87, 0x65, 0x43, 0x21}}
	pValidVotes := []Proposal{{0x23, 0x45, 0x67, 0x89}}
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	minerAtxs := map[types.NodeID]types.ATXID{
		signer.NodeID(): createATX(t, tpd.cdb, epoch.FirstLayer().Sub(1), signer, 10, time.Now()),
	}
	createEpochState(t, tpd.ProtocolDriver, epoch, minerAtxs, nil)
	msg := createFirstVote(t, signer, epoch, validVotes, pValidVotes, false)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	tpd.mClock.EXPECT().LayerToTime(gomock.Any()).Return(time.Now()).AnyTimes()
	tpd.setRoundInProgress(types.RoundID(1))
	got := tpd.HandleFirstVotes(context.Background(), "peerID", msgBytes)
	require.ErrorIs(t, got, errUntimelyMessage)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, types.FirstRound, false)
	checkFirstIncomingVotes(t, tpd.ProtocolDriver, epoch, map[types.NodeID]proposalList{})
}

func Test_HandleFirstVotes_FailedToVerifySig(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := setUpProtocolDriver(t)
	tpd.setBeginProtocol(context.Background())

	validVotes := []Proposal{{0x12, 0x34, 0x56, 0x78}, {0x87, 0x65, 0x43, 0x21}}
	pValidVotes := []Proposal{{0x23, 0x45, 0x67, 0x89}}
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	minerAtxs := map[types.NodeID]types.ATXID{
		signer.NodeID(): createATX(t, tpd.cdb, epoch.FirstLayer().Sub(1), signer, 10, time.Now()),
	}
	createEpochState(t, tpd.ProtocolDriver, epoch, minerAtxs, nil)
	msg := createFirstVote(t, signer, epoch, validVotes, pValidVotes, true)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	tpd.mClock.EXPECT().LayerToTime(gomock.Any()).Return(time.Now()).AnyTimes()
	got := tpd.HandleFirstVotes(context.Background(), "peerID", msgBytes)
	require.Contains(t, got.Error(), fmt.Sprintf("verify signature %s: failed", msg.Signature))
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, types.FirstRound, false)
	checkFirstIncomingVotes(t, tpd.ProtocolDriver, epoch, map[types.NodeID]proposalList{})
}

func Test_HandleFirstVotes_AlreadyVoted(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := setUpProtocolDriver(t)
	tpd.setBeginProtocol(context.Background())

	validVotes := []Proposal{{0x12, 0x34, 0x56, 0x78}, {0x87, 0x65, 0x43, 0x21}}
	pValidVotes := []Proposal{{0x23, 0x45, 0x67, 0x89}}
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	minerAtxs := map[types.NodeID]types.ATXID{
		signer.NodeID(): createATX(t, tpd.cdb, epoch.FirstLayer().Sub(1), signer, 10, time.Now()),
	}
	createEpochState(t, tpd.ProtocolDriver, epoch, minerAtxs, nil)
	msg := createFirstVote(t, signer, epoch, validVotes, pValidVotes, false)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	tpd.mClock.EXPECT().LayerToTime(gomock.Any()).Return(time.Now()).AnyTimes()
	got := tpd.HandleFirstVotes(context.Background(), "peerID", msgBytes)
	require.NoError(t, got)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, types.FirstRound, true)
	expected := map[types.NodeID]proposalList{
		signer.NodeID(): append(validVotes, pValidVotes...),
	}
	checkFirstIncomingVotes(t, tpd.ProtocolDriver, epoch, expected)

	// the same ed key will not cause double-vote
	msg2 := createFirstVote(t, signer, epoch, validVotes, proposalList{}, false)
	msgBytes2, err := codec.Encode(msg2)
	require.NoError(t, err)

	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	got = tpd.HandleFirstVotes(context.Background(), "peerID", msgBytes2)
	require.ErrorIs(t, got, errAlreadyVoted)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, types.FirstRound, true)
	checkFirstIncomingVotes(t, tpd.ProtocolDriver, epoch, expected)
}

func Test_HandleFirstVotes_MinerMissingATX(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	tpd := setUpProtocolDriver(t)
	tpd.setBeginProtocol(context.Background())

	validVotes := []Proposal{{0x12, 0x34, 0x56, 0x78}, {0x87, 0x65, 0x43, 0x21}}
	pValidVotes := []Proposal{{0x23, 0x45, 0x67, 0x89}}
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	createEpochState(t, tpd.ProtocolDriver, epoch, map[types.NodeID]types.ATXID{}, nil)
	msg := createFirstVote(t, signer, epoch, validVotes, pValidVotes, false)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	tpd.mClock.EXPECT().LayerToTime(gomock.Any()).Return(time.Now()).AnyTimes()
	got := tpd.HandleFirstVotes(context.Background(), "peerID", msgBytes)
	require.ErrorIs(t, got, errMinerNotActive)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, types.FirstRound, true)
	checkFirstIncomingVotes(t, tpd.ProtocolDriver, epoch, map[types.NodeID]proposalList{})
}

func Test_HandleFollowingVotes_Success(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	tpd, plist := createProtocolDriverWithFirstRoundVotes(t, signer, epoch, round)

	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []byte{0b101}, false)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	tpd.mClock.EXPECT().LayerToTime(gomock.Any()).Return(time.Now()).AnyTimes()
	res := tpd.HandleFollowingVotes(context.Background(), "peerID", msgBytes)
	require.Equal(t, nil, res)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, round, true)
	expected := make(map[Proposal]*big.Int, len(plist))
	for i, p := range plist {
		if i == 0 || i == 2 {
			expected[p] = big.NewInt(10)
		} else {
			expected[p] = big.NewInt(-10)
		}
	}
	checkVoteMargins(t, tpd.ProtocolDriver, epoch, expected)
}

func Test_HandleFollowingVotes_Shutdown(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	tpd, plist := createProtocolDriverWithFirstRoundVotes(t, signer, epoch, round)
	tpd.Close()

	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []byte{0b101}, false)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)

	res := tpd.HandleFollowingVotes(context.Background(), "peerID", msgBytes)
	require.Error(t, res)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, round, false)
	checkVoteMargins(t, tpd.ProtocolDriver, epoch, emptyVoteMargins(plist))
}

func Test_HandleFollowingVotes_NotInProtocol(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	tpd, plist := createProtocolDriverWithFirstRoundVotes(t, signer, epoch, round)

	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []byte{0b101}, false)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)

	tpd.setEndProtocol(context.Background())
	res := tpd.HandleFollowingVotes(context.Background(), "peerID", msgBytes)
	require.Error(t, res)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, round, false)
	checkVoteMargins(t, tpd.ProtocolDriver, epoch, emptyVoteMargins(plist))
}

func Test_handleFollowingVotes_CorruptMsg(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	tpd, plist := createProtocolDriverWithFirstRoundVotes(t, signer, epoch, round)

	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []byte{0b101}, false)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)

	got := tpd.HandleFollowingVotes(context.Background(), "peerID", msgBytes[1:])
	require.ErrorIs(t, got, errMalformedMessage)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, round, false)
	checkVoteMargins(t, tpd.ProtocolDriver, epoch, emptyVoteMargins(plist))
}

func Test_handleFollowingVotes_WrongEpoch(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	tpd, plist := createProtocolDriverWithFirstRoundVotes(t, signer, epoch, round)
	minerAtxs := map[types.NodeID]types.ATXID{
		signer.NodeID(): createATX(t, tpd.cdb, (epoch - 1).FirstLayer().Sub(1), signer, 10, time.Now()),
	}
	createEpochState(t, tpd.ProtocolDriver, epoch-1, minerAtxs, nil)
	minerAtxs = map[types.NodeID]types.ATXID{
		signer.NodeID(): createATX(t, tpd.cdb, (epoch + 1).FirstLayer().Sub(1), signer, 10, time.Now()),
	}
	createEpochState(t, tpd.ProtocolDriver, epoch+1, minerAtxs, nil)

	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch+1, round, []byte{0b101}, false)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	got := tpd.HandleFollowingVotes(context.Background(), "peerID", msgBytes)
	require.ErrorIs(t, got, errEpochNotActive)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, round, false)
	checkVoteMargins(t, tpd.ProtocolDriver, epoch, emptyVoteMargins(plist))
	checkVoted(t, tpd.ProtocolDriver, epoch+1, signer, round, false)
	checkVoteMargins(t, tpd.ProtocolDriver, epoch+1, emptyVoteMargins(proposalList{}))

	msg = createFollowingVote(t, signer, epoch-1, round, []byte{0b101}, false)
	msgBytes, err = codec.Encode(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	got = tpd.HandleFollowingVotes(context.Background(), "peerID", msgBytes)
	require.ErrorIs(t, got, errEpochNotActive)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, round, false)
	checkVoteMargins(t, tpd.ProtocolDriver, epoch, emptyVoteMargins(plist))
	checkVoted(t, tpd.ProtocolDriver, epoch-1, signer, round, false)
	checkVoteMargins(t, tpd.ProtocolDriver, epoch-1, emptyVoteMargins(proposalList{}))
}

func Test_handleFollowingVotes_TooEarly(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	tpd, plist := createProtocolDriverWithFirstRoundVotes(t, signer, epoch, round)

	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []byte{0b101}, false)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)

	tpd.setRoundInProgress(round - 1)
	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	tpd.mClock.EXPECT().LayerToTime(gomock.Any()).Return(time.Now()).AnyTimes()
	got := tpd.HandleFollowingVotes(context.Background(), "peerID", msgBytes)
	require.ErrorIs(t, got, errUntimelyMessage)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, round, false)
	checkVoteMargins(t, tpd.ProtocolDriver, epoch, emptyVoteMargins(plist))
}

func Test_handleFollowingVotes_FailedToVerifySig(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	tpd, plist := createProtocolDriverWithFirstRoundVotes(t, signer, epoch, round)

	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []byte{0b101}, true)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	tpd.mClock.EXPECT().LayerToTime(gomock.Any()).Return(time.Now()).AnyTimes()
	got := tpd.HandleFollowingVotes(context.Background(), "peerID", msgBytes)
	require.Contains(t, got.Error(), fmt.Sprintf("verify signature %s: failed", msg.Signature))
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, round, false)
	checkVoteMargins(t, tpd.ProtocolDriver, epoch, emptyVoteMargins(plist))
}

func Test_handleFollowingVotes_AlreadyVoted(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	tpd, plist := createProtocolDriverWithFirstRoundVotes(t, signer, epoch, round)

	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, signer, epoch, round, []byte{0b101}, false)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	tpd.mClock.EXPECT().LayerToTime(gomock.Any()).Return(time.Now()).AnyTimes()
	got := tpd.HandleFollowingVotes(context.Background(), "peerID", msgBytes)
	require.NoError(t, got)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, round, true)
	expected := make(map[Proposal]*big.Int, len(plist))
	for i, p := range plist {
		if i == 0 || i == 2 {
			expected[p] = big.NewInt(10)
		} else {
			expected[p] = big.NewInt(-10)
		}
	}
	checkVoteMargins(t, tpd.ProtocolDriver, epoch, expected)

	// now vote again
	msg = createFollowingVote(t, signer, epoch, round, []byte{0b111}, false)
	msgBytes, err = codec.Encode(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	tpd.mClock.EXPECT().LayerToTime(gomock.Any()).Return(time.Now()).AnyTimes()
	got = tpd.HandleFollowingVotes(context.Background(), "peerID", msgBytes)
	require.ErrorIs(t, got, errAlreadyVoted)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, round, true)
	checkVoteMargins(t, tpd.ProtocolDriver, epoch, expected)
}

func Test_handleFollowingVotes_MinerMissingATX(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	tpd, plist := createProtocolDriverWithFirstRoundVotes(t, signer, epoch, round)

	miner, err := signing.NewEdSigner()
	require.NoError(t, err)

	// this msg will contain a bit vector that set bit 0 and 2
	msg := createFollowingVote(t, miner, epoch, round, []byte{0b101}, false)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	tpd.mClock.EXPECT().LayerToTime(gomock.Any()).Return(time.Now()).AnyTimes()
	got := tpd.HandleFollowingVotes(context.Background(), "peerID", msgBytes)
	require.ErrorIs(t, got, errMinerNotActive)
	checkVoted(t, tpd.ProtocolDriver, epoch, miner, round, true)
	checkVoteMargins(t, tpd.ProtocolDriver, epoch, emptyVoteMargins(plist))
}

func Test_handleFollowingVotes_IgnoreUnknownProposal(t *testing.T) {
	t.Parallel()

	const epoch = types.EpochID(10)
	const round = types.RoundID(5)
	tpd := setUpProtocolDriver(t)
	tpd.setBeginProtocol(context.Background())
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	minerAtxs := map[types.NodeID]types.ATXID{
		signer.NodeID(): createATX(t, tpd.cdb, epoch.FirstLayer().Sub(1), signer, 10, time.Now()),
	}
	createEpochState(t, tpd.ProtocolDriver, epoch, minerAtxs, nil)

	known := make([]Proposal, 3)
	for i := range known {
		copy(known[i][:], types.RandomBytes(types.BeaconSize))
	}

	unknown := make([]Proposal, 2)
	for i := range unknown {
		copy(unknown[i][:], types.RandomBytes(types.BeaconSize))
	}

	plist := append(known, unknown...)
	setOwnFirstRoundVotes(t, tpd.ProtocolDriver, epoch, known)
	setMinerFirstRoundVotes(t, tpd.ProtocolDriver, epoch, signer.NodeID(), plist)
	tpd.setRoundInProgress(round)

	// this msg will contain a bit vector that set bit 0 and 2-4. the miner voted for two proposals
	// we don't know about locally
	msg := createFollowingVote(t, signer, epoch, round, []byte{0b11101}, false)
	msgBytes, err := codec.Encode(msg)
	require.NoError(t, err)

	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	tpd.mClock.EXPECT().LayerToTime(gomock.Any()).Return(time.Now()).AnyTimes()
	got := tpd.HandleFollowingVotes(context.Background(), "peerID", msgBytes)
	require.NoError(t, got)
	checkVoted(t, tpd.ProtocolDriver, epoch, signer, round, true)
	// unknown proposals' votes are ignored
	expected := make(map[Proposal]*big.Int, len(known))
	for i, p := range known {
		if i == 0 || i == 2 {
			expected[p] = big.NewInt(10)
		} else {
			expected[p] = big.NewInt(-10)
		}
	}
	checkVoteMargins(t, tpd.ProtocolDriver, epoch, expected)
}

func Test_UniqueFollowingVotingMessages(t *testing.T) {
	round := types.RoundID(3)
	votesBitVector := []byte{0b101}
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	msg1 := FollowingVotingMessage{
		FollowingVotingMessageBody: FollowingVotingMessageBody{
			RoundID:        round,
			VotesBitVector: votesBitVector,
		},
	}
	logger := logtest.New(t)
	encodedMsg1FollowingVotingMessageBody, err := codec.Encode(&msg1.FollowingVotingMessageBody)
	if err != nil {
		logger.With().Panic("failed to serialize msg1.FollowingVotingMessageBody for signing", log.Err(err))
	}
	msg1.Signature = signer.Sign(signing.BEACON_FOLLOWUP_MSG, encodedMsg1FollowingVotingMessageBody)

	data1, err := codec.Encode(&msg1)
	require.NoError(t, err)

	msg2 := FollowingVotingMessage{
		FollowingVotingMessageBody: FollowingVotingMessageBody{
			RoundID:        round,
			VotesBitVector: votesBitVector,
		},
	}
	encodedMsg2FollowingVotingMessageBody, err := codec.Encode(&msg2.FollowingVotingMessageBody)
	if err != nil {
		logger.With().Panic("failed to serialize msg2.FollowingVotingMessageBody for signing", log.Err(err))
	}
	msg2.Signature = signer.Sign(signing.BEACON_FOLLOWUP_MSG, encodedMsg2FollowingVotingMessageBody)

	data2, err := codec.Encode(&msg2)
	require.NoError(t, err)

	// without EpochID, we cannot tell the following messages apart
	require.Equal(t, data1, data2)

	msg1.EpochID = types.EpochID(5)
	encodedMsg1FollowingVotingMessageBody, err = codec.Encode(&msg1.FollowingVotingMessageBody)
	if err != nil {
		logger.With().Panic("failed to serialize msg1.FollowingVotingMessageBody for signing", log.Err(err))
	}
	msg1.Signature = signer.Sign(signing.BEACON_FOLLOWUP_MSG, encodedMsg1FollowingVotingMessageBody)
	data1, err = codec.Encode(&msg1)
	require.NoError(t, err)

	msg2.EpochID = msg1.EpochID + 1
	encodedMsg2FollowingVotingMessageBody, err = codec.Encode(&msg2.FollowingVotingMessageBody)
	if err != nil {
		logger.With().Panic("failed to serialize msg2.FollowingVotingMessageBody for signing", log.Err(err))
	}
	msg2.Signature = signer.Sign(signing.BEACON_FOLLOWUP_MSG, encodedMsg2FollowingVotingMessageBody)

	data2, err = codec.Encode(&msg2)
	require.NoError(t, err)

	// with EpochID, voting messages from the same miner with the same bit vector will
	// not be considered duplicate gossip messages.
	require.NotEqual(t, data1, data2)
}

func TestTracker(t *testing.T) {
	track := newVotesTracker()
	for i := 0; i < 1000; i++ {
		require.True(t, track.register(types.RoundID(i)), i)
	}
	for i := 0; i < 1000; i++ {
		require.False(t, track.register(types.RoundID(i)), i)
	}
}
