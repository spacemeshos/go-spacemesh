package hare

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func BuildCommitMsg(signer *signing.EdSigner, s *Set) *Message {
	builder := newMessageBuilder()
	builder.SetType(commit).SetLayer(instanceID1).SetRoundCounter(commitRound).SetCommittedRound(ki).SetValues(s)
	builder.SetEligibilityCount(1)
	builder = builder.Sign(signer)
	return builder.Build()
}

func TestCommitTracker_OnCommit(t *testing.T) {
	s := NewSetFromValues(types.ProposalID{1})
	et := NewEligibilityTracker(lowThresh10)
	mch := make(chan *types.MalfeasanceGossip, lowThresh10)
	tracker := newCommitTracker(logtest.New(t), commitRound, mch, et, lowThresh10+1, lowThresh10, s)

	for i := 0; i < lowThresh10; i++ {
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)
		m := BuildCommitMsg(signer, s)
		et.Track(m.SmesherID, m.Round, m.Eligibility.Count, true)
		tracker.OnCommit(context.Background(), m)
		require.False(t, tracker.HasEnoughCommits())
	}
	require.Equal(t, CountInfo{hCount: lowThresh10, numHonest: lowThresh10}, *tracker.CommitCount())

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildCommitMsg(signer, s)
	et.Track(m.SmesherID, m.Round, m.Eligibility.Count, true)
	tracker.OnCommit(context.Background(), m)
	require.True(t, tracker.HasEnoughCommits())
	require.Equal(t, CountInfo{hCount: lowThresh10 + 1, numHonest: lowThresh10 + 1}, *tracker.CommitCount())
	require.Empty(t, mch)
}

func TestCommitTracker_OnCommit_TooManyDishonest(t *testing.T) {
	s := NewSetFromValues(types.ProposalID{1})
	et := NewEligibilityTracker(lowThresh10)
	mch := make(chan *types.MalfeasanceGossip, lowThresh10)
	tracker := newCommitTracker(logtest.New(t), commitRound, mch, et, lowThresh10+1, lowThresh10*2, s)

	for i := 0; i < lowThresh10; i++ {
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)
		et.Track(signer.NodeID(), commitRound, 1, false)
		require.False(t, tracker.HasEnoughCommits())
	}
	require.Equal(t, CountInfo{keCount: lowThresh10, numKE: lowThresh10}, *tracker.CommitCount())

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildCommitMsg(signer, s)
	et.Track(m.SmesherID, m.Round, m.Eligibility.Count, false)
	tracker.OnCommit(context.Background(), m)
	require.False(t, tracker.HasEnoughCommits())
	require.Equal(t, CountInfo{dhCount: 1, keCount: lowThresh10, numDishonest: 1, numKE: lowThresh10}, *tracker.CommitCount())
	require.Empty(t, mch)
}

func TestCommitTracker_OnCommit_JustEnough(t *testing.T) {
	s := NewSetFromValues(types.ProposalID{1})
	et := NewEligibilityTracker(lowThresh10)
	mch := make(chan *types.MalfeasanceGossip, lowThresh10)
	tracker := newCommitTracker(logtest.New(t), commitRound, mch, et, lowThresh10+1, lowThresh10*2, s)

	for i := 0; i < lowThresh10; i++ {
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)
		et.Track(signer.NodeID(), commitRound, 1, false)
		require.False(t, tracker.HasEnoughCommits())
	}
	require.Equal(t, CountInfo{keCount: lowThresh10, numKE: lowThresh10}, *tracker.CommitCount())

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildCommitMsg(signer, s)
	et.Track(m.SmesherID, m.Round, m.Eligibility.Count, true)
	tracker.OnCommit(context.Background(), m)
	require.True(t, tracker.HasEnoughCommits())
	require.Equal(t, CountInfo{hCount: 1, keCount: lowThresh10, numHonest: 1, numKE: lowThresh10}, *tracker.CommitCount())
	require.Empty(t, mch)
}

func TestCommitTracker_OnCommit_KnownEquivocator(t *testing.T) {
	s := NewSetFromValues(types.ProposalID{1})
	et := NewEligibilityTracker(lowThresh10)
	mch := make(chan *types.MalfeasanceGossip, lowThresh10)
	tracker := newCommitTracker(logtest.New(t), commitRound, mch, et, lowThresh10+1, lowThresh10, s)

	for i := 0; i < lowThresh10; i++ {
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)
		m := BuildCommitMsg(signer, s)
		et.Track(m.SmesherID, m.Round, m.Eligibility.Count, true)
		tracker.OnCommit(context.Background(), m)
		require.False(t, tracker.HasEnoughCommits())
	}
	require.Equal(t, CountInfo{hCount: lowThresh10, numHonest: lowThresh10}, *tracker.CommitCount())

	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	et.Track(signer.NodeID(), commitRound, 1, false)
	require.True(t, tracker.HasEnoughCommits())
	require.Empty(t, mch)
	require.Equal(t, CountInfo{hCount: lowThresh10, keCount: 1, numHonest: lowThresh10, numKE: 1}, *tracker.CommitCount())
}

func TestCommitTracker_OnCommitDuplicate(t *testing.T) {
	s := NewSetFromValues(types.ProposalID{1})
	et := NewEligibilityTracker(2)
	mch := make(chan *types.MalfeasanceGossip, 2)
	tracker := newCommitTracker(logtest.New(t), commitRound, mch, et, 2, 2, s)
	signer1, err := signing.NewEdSigner()
	require.NoError(t, err)
	signer2, err := signing.NewEdSigner()
	require.NoError(t, err)
	require.Equal(t, 0, len(tracker.seenSenders))
	tracker.OnCommit(context.Background(), BuildCommitMsg(signer1, s))
	require.Equal(t, 1, len(tracker.seenSenders))
	require.Equal(t, 1, len(tracker.commits))
	tracker.OnCommit(context.Background(), BuildCommitMsg(signer1, s))
	require.Equal(t, 1, len(tracker.seenSenders))
	tracker.OnCommit(context.Background(), BuildCommitMsg(signer2, s))
	require.Equal(t, 2, len(tracker.seenSenders))
	require.Empty(t, mch)
}

func TestCommitTracker_OnCommitEquivocate(t *testing.T) {
	s1 := NewSetFromValues(types.ProposalID{1})
	s2 := NewSetFromValues(types.ProposalID{2})
	et := NewEligibilityTracker(2)
	mch := make(chan *types.MalfeasanceGossip, 2)
	tracker := newCommitTracker(logtest.New(t), commitRound, mch, et, 2, 2, s1)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	msg1 := BuildCommitMsg(signer, s1)
	msg2 := BuildCommitMsg(signer, s2)
	tracker.OnCommit(context.Background(), msg1)
	tracker.OnCommit(context.Background(), msg2)
	require.Len(t, mch, 1)
	expected := types.MalfeasanceGossip{
		MalfeasanceProof: types.MalfeasanceProof{
			Layer: msg1.Layer,
			Proof: types.Proof{
				Type: types.HareEquivocation,
				Data: &types.HareProof{
					Messages: [2]types.HareProofMsg{
						{
							InnerMsg: types.HareMetadata{
								Layer:   msg1.Layer,
								Round:   msg1.Round,
								MsgHash: types.BytesToHash(msg1.InnerMessage.HashBytes()),
							},
							SmesherID: msg1.SmesherID,
							Signature: msg1.Signature,
						},
						{
							InnerMsg: types.HareMetadata{
								Layer:   msg2.Layer,
								Round:   msg2.Round,
								MsgHash: types.BytesToHash(msg2.InnerMessage.HashBytes()),
							},
							SmesherID: msg2.SmesherID,
							Signature: msg2.Signature,
						},
					},
				},
			},
		},
		Eligibility: &types.HareEligibilityGossip{
			Layer:       msg2.Layer,
			Round:       msg2.Round,
			NodeID:      msg2.SmesherID,
			Eligibility: msg2.Eligibility,
		},
	}
	gossip := <-mch
	verifyMalfeasanceProof(t, signer, gossip)
	require.Equal(t, expected, *gossip)
	et.ForEach(commitRound, func(nodeID types.NodeID, cred *Cred) {
		require.Equal(t, signer.NodeID(), nodeID)
		require.False(t, cred.Honest)
	})
}

func TestCommitTracker_HasEnoughCommits(t *testing.T) {
	signer1, err := signing.NewEdSigner()
	require.NoError(t, err)
	signer2, err := signing.NewEdSigner()
	require.NoError(t, err)

	s := NewSetFromValues(types.ProposalID{1})
	et := NewEligibilityTracker(2)
	mch := make(chan *types.MalfeasanceGossip, 2)
	tracker := newCommitTracker(logtest.New(t), commitRound, mch, et, 2, 2, s)
	require.False(t, tracker.HasEnoughCommits())
	m1 := BuildCommitMsg(signer1, s)
	et.Track(m1.SmesherID, m1.Round, m1.Eligibility.Count, true)
	tracker.OnCommit(context.Background(), m1)
	m2 := BuildCommitMsg(signer2, s)
	et.Track(m2.SmesherID, m2.Round, m2.Eligibility.Count, true)
	tracker.OnCommit(context.Background(), m2)
	require.True(t, tracker.HasEnoughCommits())
	require.Empty(t, mch)
}

func TestCommitTracker_HasEnoughCommits_KnownEquivocator(t *testing.T) {
	signer1, err := signing.NewEdSigner()
	require.NoError(t, err)
	signer2, err := signing.NewEdSigner()
	require.NoError(t, err)

	s := NewSetFromValues(types.ProposalID{1})
	et := NewEligibilityTracker(2)
	mch := make(chan *types.MalfeasanceGossip, 2)
	tracker := newCommitTracker(logtest.New(t), commitRound, mch, et, 2, 2, s)
	require.False(t, tracker.HasEnoughCommits())
	m := BuildCommitMsg(signer1, s)
	et.Track(m.SmesherID, m.Round, m.Eligibility.Count, true)
	tracker.OnCommit(context.Background(), m)
	require.False(t, tracker.HasEnoughCommits())

	// add a known equivocator
	et.Track(signer2.NodeID(), m.Round, m.Eligibility.Count, false)
	require.True(t, tracker.HasEnoughCommits())
	require.Empty(t, mch)
}

func TestCommitTracker_HasEnoughCommits_TooFewKnownEquivocator(t *testing.T) {
	signer1, err := signing.NewEdSigner()
	require.NoError(t, err)
	signer2, err := signing.NewEdSigner()
	require.NoError(t, err)

	s := NewSetFromValues(types.ProposalID{1})
	et := NewEligibilityTracker(2)
	mch := make(chan *types.MalfeasanceGossip, 2)
	tracker := newCommitTracker(logtest.New(t), commitRound, mch, et, 3, 5, s)
	require.False(t, tracker.HasEnoughCommits())
	m := BuildCommitMsg(signer1, s)
	et.Track(m.SmesherID, m.Round, m.Eligibility.Count, true)
	tracker.OnCommit(context.Background(), m)
	require.False(t, tracker.HasEnoughCommits())

	et.Track(signer2.NodeID(), m.Round, m.Eligibility.Count, false)
	require.False(t, tracker.HasEnoughCommits())
	require.Empty(t, mch)
}

func TestCommitTracker_HasEnoughCommits_TooFewHonestVotes(t *testing.T) {
	s := NewSetFromValues(types.ProposalID{1})
	et := NewEligibilityTracker(2)
	mch := make(chan *types.MalfeasanceGossip, 2)
	threshold := 3
	tracker := newCommitTracker(logtest.New(t), commitRound, mch, et, threshold, 2*threshold-1, s)
	require.False(t, tracker.HasEnoughCommits())
	for i := 0; i < threshold; i++ {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		et.Track(sig.NodeID(), commitRound, 1, false)
	}
	require.False(t, tracker.HasEnoughCommits())
	require.Empty(t, mch)
}

func TestCommitTracker_BuildCertificate(t *testing.T) {
	signer1, err := signing.NewEdSigner()
	require.NoError(t, err)
	signer2, err := signing.NewEdSigner()
	require.NoError(t, err)

	s := NewSetFromValues(types.ProposalID{1})
	et := NewEligibilityTracker(2)
	tracker := newCommitTracker(logtest.New(t), commitRound, make(chan *types.MalfeasanceGossip, 2), et, 2, 2, s)
	require.Nil(t, tracker.BuildCertificate())
	m1 := BuildCommitMsg(signer1, s)
	et.Track(m1.SmesherID, m1.Round, m1.Eligibility.Count, true)
	tracker.OnCommit(context.Background(), m1)
	m2 := BuildCommitMsg(signer2, s)
	et.Track(m2.SmesherID, m2.Round, m2.Eligibility.Count, true)
	tracker.OnCommit(context.Background(), m2)
	cert := tracker.BuildCertificate()
	require.NotNil(t, cert)
	require.Equal(t, 2, len(cert.AggMsgs.Messages))
	require.Nil(t, cert.AggMsgs.Messages[0].Values)
	require.Nil(t, cert.AggMsgs.Messages[1].Values)
}
