package hare

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func buildStatusMsg(sig *signing.EdSigner, s *Set, ki uint32) *Msg {
	builder := newMessageBuilder()
	builder.
		SetType(status).
		SetLayer(instanceID1).
		SetRoundCounter(statusRound).
		SetCommittedRound(ki).
		SetValues(s).
		SetEligibilityCount(1)
	return builder.Sign(sig).Build()
}

func BuildStatusMsg(sig *signing.EdSigner, s *Set) *Msg {
	return buildStatusMsg(sig, s, preRound)
}

func TestStatusTracker_RecordStatus(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(types.ProposalID{1})
	s.Add(types.ProposalID{2})

	mch := make(chan *types.MalfeasanceGossip, lowThresh10)
	et := NewEligibilityTracker(lowThresh10)
	tracker := newStatusTracker(logtest.New(t), statusRound, mch, et, lowThresh10, lowThresh10)
	require.False(t, tracker.IsSVPReady())

	for i := 0; i < lowThresh10; i++ {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		m := BuildStatusMsg(sig, s)
		et.Track(m.SmesherID, m.Round, m.Eligibility.Count, true)
		tracker.RecordStatus(context.Background(), m)
		require.False(t, tracker.IsSVPReady())
	}

	tracker.AnalyzeStatusMessages(func(m *Msg) bool { return true })
	require.True(t, tracker.IsSVPReady())
	require.Empty(t, mch)
}

func TestStatusTracker_BuildUnionSet(t *testing.T) {
	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig3, err := signing.NewEdSigner()
	require.NoError(t, err)

	et := NewEligibilityTracker(lowThresh10)
	mch := make(chan *types.MalfeasanceGossip, lowThresh10)
	tracker := newStatusTracker(logtest.New(t), statusRound, mch, et, lowThresh10, lowThresh10)

	s := NewEmptySet(lowDefaultSize)
	s.Add(types.ProposalID{1})
	m1 := BuildStatusMsg(sig1, s)
	et.Track(m1.SmesherID, m1.Round, m1.Eligibility.Count, true)
	tracker.RecordStatus(context.Background(), m1)
	s.Add(types.ProposalID{2})
	m2 := BuildStatusMsg(sig2, s)
	et.Track(m2.SmesherID, m2.Round, m2.Eligibility.Count, true)
	tracker.RecordStatus(context.Background(), m2)
	s.Add(types.ProposalID{3})
	m3 := BuildStatusMsg(sig3, s)
	et.Track(m3.SmesherID, m3.Round, m3.Eligibility.Count, true)
	tracker.RecordStatus(context.Background(), m3)

	g := tracker.buildUnionSet(defaultSetSize)
	require.True(t, s.Equals(g))
	require.Empty(t, mch)
}

func TestStatusTracker_IsSVPReady(t *testing.T) {
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	et := NewEligibilityTracker(1)
	mch := make(chan *types.MalfeasanceGossip, 1)
	tracker := newStatusTracker(logtest.New(t), statusRound, mch, et, 1, 1)
	require.False(t, tracker.IsSVPReady())
	s := NewSetFromValues(types.ProposalID{1})
	m := BuildStatusMsg(sig, s)
	et.Track(m.SmesherID, m.Round, m.Eligibility.Count, true)
	tracker.RecordStatus(context.Background(), BuildStatusMsg(sig, s))
	tracker.AnalyzeStatusMessages(func(m *Msg) bool { return true })
	require.True(t, tracker.IsSVPReady())
	require.Empty(t, mch)
}

func TestStatusTracker_BuildSVP(t *testing.T) {
	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)

	et := NewEligibilityTracker(1)
	mch := make(chan *types.MalfeasanceGossip, 1)
	tracker := newStatusTracker(logtest.New(t), statusRound, mch, et, 2, 1)
	s := NewSetFromValues(types.ProposalID{1})
	m1 := BuildStatusMsg(sig1, s)
	et.Track(m1.SmesherID, m1.Round, m1.Eligibility.Count, true)
	tracker.RecordStatus(context.Background(), m1)
	m2 := BuildStatusMsg(sig2, s)
	et.Track(m2.SmesherID, m2.Round, m2.Eligibility.Count, true)
	tracker.RecordStatus(context.Background(), m2)
	tracker.AnalyzeStatusMessages(func(m *Msg) bool { return true })
	svp := tracker.BuildSVP()
	require.Equal(t, 2, len(svp.Messages))
	require.Empty(t, mch)
}

func TestStatusTracker_ProposalSetTypeA(t *testing.T) {
	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)

	et := NewEligibilityTracker(1)
	mch := make(chan *types.MalfeasanceGossip, 1)
	tracker := newStatusTracker(logtest.New(t), statusRound, mch, et, 2, 1)
	s1 := NewSetFromValues(types.ProposalID{1})
	m1 := buildStatusMsg(sig1, s1, preRound)
	et.Track(m1.SmesherID, m1.Round, m1.Eligibility.Count, true)
	tracker.RecordStatus(context.Background(), m1)
	s2 := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2})
	m2 := buildStatusMsg(sig2, s2, preRound)
	et.Track(m2.SmesherID, m2.Round, m2.Eligibility.Count, true)
	tracker.RecordStatus(context.Background(), m2)

	proposedSet := tracker.ProposalSet(2)
	require.NotNil(t, proposedSet)
	require.True(t, proposedSet.Equals(s1.Union(s2)))
	require.Empty(t, mch)
}

func TestStatusTracker_ProposalSetTypeB(t *testing.T) {
	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)

	et := NewEligibilityTracker(1)
	mch := make(chan *types.MalfeasanceGossip, 1)
	tracker := newStatusTracker(logtest.New(t), statusRound, mch, et, 2, 1)
	s1 := NewSetFromValues(types.ProposalID{1}, types.ProposalID{3})
	s2 := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2})
	m1 := buildStatusMsg(sig1, s1, 0)
	et.Track(m1.SmesherID, m1.Round, m1.Eligibility.Count, true)
	tracker.RecordStatus(context.Background(), m1)
	m2 := buildStatusMsg(sig2, s2, 2)
	et.Track(m2.SmesherID, m2.Round, m2.Eligibility.Count, true)
	tracker.RecordStatus(context.Background(), m2)
	tracker.AnalyzeStatusMessages(func(m *Msg) bool { return true })
	proposedSet := tracker.ProposalSet(2)
	require.NotNil(t, proposedSet)
	require.True(t, proposedSet.Equals(s2))
	require.Empty(t, mch)
}

func TestStatusTracker_Equivocate_Fail(t *testing.T) {
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	et := NewEligibilityTracker(1)
	mch := make(chan *types.MalfeasanceGossip, 1)
	tracker := newStatusTracker(logtest.New(t), statusRound, mch, et, 2, 3)
	s1 := NewSetFromValues(types.ProposalID{1}, types.ProposalID{3})
	s2 := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2})
	m1 := buildStatusMsg(sig, s1, 0)
	et.Track(m1.SmesherID, m1.Round, m1.Eligibility.Count, true)
	m2 := buildStatusMsg(sig, s2, 0)
	et.Track(m2.SmesherID, m2.Round, m2.Eligibility.Count, true)
	tracker.RecordStatus(context.Background(), m1)
	tracker.RecordStatus(context.Background(), m2)
	tracker.AnalyzeStatusMessages(func(m *Msg) bool { return true })
	proposedSet := tracker.ProposalSet(2)
	require.NotNil(t, proposedSet)
	require.True(t, proposedSet.Equals(s1), proposedSet)
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
							Signature: m1.Signature,
						},
						{
							InnerMsg: types.HareMetadata{
								Layer:   m2.Layer,
								Round:   m2.Round,
								MsgHash: types.BytesToHash(m2.HashBytes()),
							},
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
	require.Equal(t, expected, *gossip)
	require.False(t, tracker.IsSVPReady())
	require.NotNil(t, tracker.tally)
	expTally := CountInfo{dhCount: 1, numDishonest: 1}
	require.Equal(t, expTally, *tracker.tally)
}

func TestStatusTracker_Equivocate_Pass(t *testing.T) {
	sigBad, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)

	et := NewEligibilityTracker(1)
	mch := make(chan *types.MalfeasanceGossip, 1)
	tracker := newStatusTracker(logtest.New(t), statusRound, mch, et, 2, 3)
	s1 := NewSetFromValues(types.ProposalID{1}, types.ProposalID{3})
	s2 := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2})
	m1 := buildStatusMsg(sigBad, s1, 0)
	et.Track(m1.SmesherID, m1.Round, m1.Eligibility.Count, true)
	m2 := buildStatusMsg(sigBad, s2, 0)
	et.Track(m2.SmesherID, m2.Round, m2.Eligibility.Count, true)
	tracker.RecordStatus(context.Background(), m1)
	tracker.RecordStatus(context.Background(), m2)

	for _, sig := range []*signing.EdSigner{sig1, sig2} {
		m := buildStatusMsg(sig, s1, 0)
		et.Track(m.SmesherID, m.Round, m.Eligibility.Count, true)
		tracker.RecordStatus(context.Background(), m)
	}

	tracker.AnalyzeStatusMessages(func(m *Msg) bool { return true })
	proposedSet := tracker.ProposalSet(2)
	require.NotNil(t, proposedSet)
	require.True(t, proposedSet.Equals(s1), proposedSet)
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
							Signature: m1.Signature,
						},
						{
							InnerMsg: types.HareMetadata{
								Layer:   m2.Layer,
								Round:   m2.Round,
								MsgHash: types.BytesToHash(m2.HashBytes()),
							},
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
	require.Equal(t, expected, *gossip)
	require.True(t, tracker.IsSVPReady())
	require.NotNil(t, tracker.tally)
	expTally := CountInfo{hCount: 2, dhCount: 1, numHonest: 2, numDishonest: 1}
	require.Equal(t, expTally, *tracker.tally)
}

func TestStatusTracker_WithKnownEquivocator(t *testing.T) {
	sigBad, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)

	et := NewEligibilityTracker(1)
	mch := make(chan *types.MalfeasanceGossip, 1)
	tracker := newStatusTracker(logtest.New(t), statusRound, mch, et, 3, 5)
	s1 := NewSetFromValues(types.ProposalID{1}, types.ProposalID{3})

	for _, sig := range []*signing.EdSigner{sig1, sig2} {
		m := buildStatusMsg(sig, s1, 0)
		et.Track(m.SmesherID, m.Round, m.Eligibility.Count, true)
		tracker.RecordStatus(context.Background(), m)
	}
	// received a gossiped eligibility for this round
	et.Track(sigBad.NodeID(), statusRound, 1, false)

	tracker.AnalyzeStatusMessages(func(m *Msg) bool { return true })
	proposedSet := tracker.ProposalSet(2)
	require.NotNil(t, proposedSet)
	require.True(t, proposedSet.Equals(s1), proposedSet)
	require.Len(t, mch, 0)
	require.True(t, tracker.IsSVPReady())
	require.NotNil(t, tracker.tally)
	expected := CountInfo{hCount: 2, dhCount: 0, keCount: 1, numHonest: 2, numDishonest: 0, numKE: 1}
	require.Equal(t, expected, *tracker.tally)
}

func TestStatusTracker_NotEnoughKnownEquivocators(t *testing.T) {
	et := NewEligibilityTracker(1)
	mch := make(chan *types.MalfeasanceGossip, 1)
	tracker := newStatusTracker(logtest.New(t), statusRound, mch, et, lowThresh10+1, lowThresh10*2)
	for i := 0; i < lowThresh10-1; i++ {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		et.Track(sig.NodeID(), statusRound, 1, false)
	}

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	s := NewSetFromValues(types.ProposalID{1}, types.ProposalID{3})
	m := buildStatusMsg(sig, s, 0)
	et.Track(m.SmesherID, m.Round, m.Eligibility.Count, true)
	tracker.RecordStatus(context.Background(), m)

	tracker.AnalyzeStatusMessages(func(m *Msg) bool { return true })
	proposedSet := tracker.ProposalSet(2)
	require.NotNil(t, proposedSet)
	require.True(t, proposedSet.Equals(s), proposedSet)
	require.False(t, tracker.IsSVPReady())
	require.NotNil(t, tracker.tally)
	expTally := CountInfo{hCount: 1, keCount: lowThresh10 - 1, numHonest: 1, numKE: lowThresh10 - 1}
	require.Equal(t, expTally, *tracker.tally)
}

func TestStatusTracker_NotEnoughHonestVote(t *testing.T) {
	et := NewEligibilityTracker(1)
	mch := make(chan *types.MalfeasanceGossip, 1)
	tracker := newStatusTracker(logtest.New(t), statusRound, mch, et, lowThresh10+1, lowThresh10*2)
	for i := 0; i < lowThresh10*2; i++ {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		et.Track(sig.NodeID(), statusRound, 1, false)
	}

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	s := NewSetFromValues(types.ProposalID{1}, types.ProposalID{3})
	m := buildStatusMsg(sig, s, 0)
	et.Track(m.SmesherID, m.Round, m.Eligibility.Count, false)
	tracker.RecordStatus(context.Background(), m)

	tracker.AnalyzeStatusMessages(func(m *Msg) bool { return true })
	proposedSet := tracker.ProposalSet(2)
	require.NotNil(t, proposedSet)
	require.True(t, proposedSet.Equals(s), proposedSet)
	require.False(t, tracker.IsSVPReady())
	require.NotNil(t, tracker.tally)
	expTally := CountInfo{dhCount: 1, keCount: lowThresh10 * 2, numDishonest: 1, numKE: lowThresh10 * 2}
	require.Equal(t, expTally, *tracker.tally)
}

func TestStatusTracker_JustEnough(t *testing.T) {
	et := NewEligibilityTracker(1)
	mch := make(chan *types.MalfeasanceGossip, 1)
	tracker := newStatusTracker(logtest.New(t), statusRound, mch, et, lowThresh10+1, lowThresh10*2)
	for i := 0; i < lowThresh10; i++ {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		et.Track(sig.NodeID(), statusRound, 1, false)
	}

	sig, err := signing.NewEdSigner()
	require.NoError(t, err)
	s := NewSetFromValues(types.ProposalID{1}, types.ProposalID{3})
	m := buildStatusMsg(sig, s, 0)
	et.Track(m.SmesherID, m.Round, m.Eligibility.Count, true)
	tracker.RecordStatus(context.Background(), m)

	tracker.AnalyzeStatusMessages(func(m *Msg) bool { return true })
	proposedSet := tracker.ProposalSet(2)
	require.NotNil(t, proposedSet)
	require.True(t, proposedSet.Equals(s), proposedSet)
	require.True(t, tracker.IsSVPReady())
	require.NotNil(t, tracker.tally)
	expTally := CountInfo{hCount: 1, keCount: lowThresh10, numHonest: 1, numKE: lowThresh10}
	require.Equal(t, expTally, *tracker.tally)
}

func TestStatusTracker_AnalyzeStatuses(t *testing.T) {
	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig3, err := signing.NewEdSigner()
	require.NoError(t, err)

	et := NewEligibilityTracker(1)
	mch := make(chan *types.MalfeasanceGossip, 1)
	tracker := newStatusTracker(logtest.New(t), statusRound, mch, et, 2, 1)
	s1 := NewSetFromValues(types.ProposalID{1}, types.ProposalID{3})
	s2 := NewSetFromValues(types.ProposalID{1}, types.ProposalID{2})
	m1 := buildStatusMsg(sig1, s1, 2)
	et.Track(m1.SmesherID, m1.Round, m1.Eligibility.Count, true)
	m2 := buildStatusMsg(sig2, s2, 1)
	et.Track(m2.SmesherID, m2.Round, m2.Eligibility.Count, true)
	m3 := buildStatusMsg(sig3, s2, 2)
	et.Track(m3.SmesherID, m3.Round, m3.Eligibility.Count, true)
	tracker.RecordStatus(context.Background(), m1)
	tracker.RecordStatus(context.Background(), m2)
	tracker.RecordStatus(context.Background(), m3)
	tracker.AnalyzeStatusMessages(func(m *Msg) bool { return true })
	require.True(t, tracker.IsSVPReady())
	require.NotNil(t, tracker.tally)
	expected := CountInfo{hCount: 3, numHonest: 3}
	require.Equal(t, expected, *tracker.tally)
	require.Empty(t, mch)
}
