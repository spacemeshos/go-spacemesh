package hare

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func BuildNotifyMsg(signing *signing.EdSigner, s *Set) *Message {
	builder := newMessageBuilder()
	builder.SetType(notify).SetLayer(instanceID1).SetRoundCounter(notifyRound).SetCommittedRound(ki).SetValues(s)
	cert := &Certificate{}
	cert.Values = NewSetFromValues(types.ProposalID{1}).ToSlice()
	cert.AggMsgs = &AggregatedMessages{}
	cert.AggMsgs.Messages = []Message{*BuildCommitMsg(signing, s)}
	builder.SetCertificate(cert)
	builder.SetEligibilityCount(1)
	return builder.Sign(signing).Build()
}

func TestNotifyTracker_OnNotify(t *testing.T) {
	s1 := NewEmptySet(lowDefaultSize)
	s1.Add(types.ProposalID{1})
	s1.Add(types.ProposalID{2})
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	et := NewEligibilityTracker(lowDefaultSize)
	mch := make(chan *types.MalfeasanceGossip, lowDefaultSize)
	tracker := newNotifyTracker(logtest.New(t), notifyRound, mch, et, lowDefaultSize)
	m1 := BuildNotifyMsg(signer, s1)
	et.Track(m1.SmesherID, m1.Round, m1.Eligibility.Count, true)
	exist := tracker.OnNotify(context.Background(), m1)
	require.Equal(t, CountInfo{hCount: 1, numHonest: 1}, *tracker.NotificationsCount(s1))
	require.False(t, exist)
	exist = tracker.OnNotify(context.Background(), m1)
	require.True(t, exist)
	require.Empty(t, mch)
	require.Equal(t, CountInfo{hCount: 1, numHonest: 1}, *tracker.NotificationsCount(s1))

	s2 := s1.Clone()
	s2.Add(types.ProposalID{3})
	m2 := BuildNotifyMsg(signer, s2)
	tracker.OnNotify(context.Background(), m2)
	require.Equal(t, CountInfo{dhCount: 1, numDishonest: 1}, *tracker.NotificationsCount(s1))
	require.Equal(t, CountInfo{}, *tracker.NotificationsCount(s2))
	require.Len(t, mch, 1)
	expected := types.MalfeasanceGossip{
		MalfeasanceProof: types.MalfeasanceProof{
			Layer: m1.Layer,
			Proof: types.Proof{
				Type: types.HareEquivocation,
				Data: &types.HareProof{
					Messages: [2]types.HareProofMsg{
						{
							InnerMsg: types.HareMetadata{
								Layer:   m1.Layer,
								Round:   m1.Round,
								MsgHash: types.BytesToHash(m1.HashBytes()),
							},
							SmesherID: m1.SmesherID,
							Signature: m1.Signature,
						},
						{
							InnerMsg: types.HareMetadata{
								Layer:   m2.Layer,
								Round:   m2.Round,
								MsgHash: types.BytesToHash(m2.HashBytes()),
							},
							SmesherID: m2.SmesherID,
							Signature: m2.Signature,
						},
					},
				},
			},
		},
		Eligibility: &types.HareEligibilityGossip{
			Layer:       m2.Layer,
			Round:       m2.Round,
			NodeID:      m2.SmesherID,
			Eligibility: m2.Eligibility,
		},
	}
	gossip := <-mch
	verifyMalfeasanceProof(t, signer, gossip)
	require.Equal(t, expected, *gossip)
}

func TestNotifyTracker_NotificationsCount(t *testing.T) {
	signer1, err := signing.NewEdSigner()
	require.NoError(t, err)

	signer2, err := signing.NewEdSigner()
	require.NoError(t, err)

	s := NewEmptySet(lowDefaultSize)
	s.Add(types.ProposalID{1})
	et := NewEligibilityTracker(lowDefaultSize)
	mch := make(chan *types.MalfeasanceGossip, lowDefaultSize)
	tracker := newNotifyTracker(logtest.New(t), notifyRound, mch, et, lowDefaultSize)
	m1 := BuildNotifyMsg(signer1, s)
	et.Track(m1.SmesherID, m1.Round, m1.Eligibility.Count, true)
	tracker.OnNotify(context.Background(), m1)
	require.Equal(t, CountInfo{hCount: 1, numHonest: 1}, *tracker.NotificationsCount(s))

	m2 := BuildNotifyMsg(signer2, s)
	et.Track(m2.SmesherID, m2.Round, m2.Eligibility.Count, false)
	tracker.OnNotify(context.Background(), m2)
	ci := tracker.NotificationsCount(s)
	require.Equal(t, CountInfo{hCount: 1, dhCount: 1, numHonest: 1, numDishonest: 1}, *ci)
	require.False(t, ci.Meet(2))
	require.Empty(t, mch)

	// add a known equivocator
	signer3, err := signing.NewEdSigner()
	require.NoError(t, err)
	et.Track(signer3.NodeID(), m2.Round, 1, false)
	ci = tracker.NotificationsCount(s)
	require.Equal(t, CountInfo{hCount: 1, dhCount: 1, keCount: 1, numHonest: 1, numDishonest: 1, numKE: 1}, *ci)
	require.True(t, ci.Meet(2))
}

func TestNotifyTracker_NotificationsCount_TooFewKnownEquivocator(t *testing.T) {
	const threshold = 10
	s := NewEmptySet(lowDefaultSize)
	s.Add(types.ProposalID{1})
	et := NewEligibilityTracker(lowDefaultSize)
	mch := make(chan *types.MalfeasanceGossip, lowDefaultSize)
	tracker := newNotifyTracker(logtest.New(t), notifyRound, mch, et, lowDefaultSize)

	for i := 0; i < threshold-2; i++ {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		et.Track(sig.NodeID(), notifyRound, 1, false)
	}

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	m := BuildNotifyMsg(sig, s)
	et.Track(m.SmesherID, m.Round, m.Eligibility.Count, true)
	tracker.OnNotify(context.Background(), m)

	ci := tracker.NotificationsCount(s)
	require.False(t, ci.Meet(threshold))

	// add another known equivocator
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)
	et.Track(sig2.NodeID(), notifyRound, 1, false)
	ci = tracker.NotificationsCount(s)
	require.True(t, ci.Meet(threshold))
}

func TestNotifyTracker_NotificationsCount_NoHonestVotes(t *testing.T) {
	const threshold = 10
	s := NewEmptySet(lowDefaultSize)
	s.Add(types.ProposalID{1})
	et := NewEligibilityTracker(lowDefaultSize)
	mch := make(chan *types.MalfeasanceGossip, lowDefaultSize)
	tracker := newNotifyTracker(logtest.New(t), notifyRound, mch, et, lowDefaultSize)

	for i := 0; i < threshold; i++ {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		et.Track(sig.NodeID(), notifyRound, 1, false)
	}

	ci := tracker.NotificationsCount(s)
	require.False(t, ci.Meet(threshold))
}
