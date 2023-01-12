package hare

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func buildStatusMsg(signing Signer, s *Set, ki uint32) *Msg {
	builder := newMessageBuilder()
	builder.SetType(status).SetLayer(instanceID1).SetRoundCounter(statusRound).SetCommittedRound(ki).SetValues(s)
	builder.SetEligibilityCount(1)
	return builder.SetPubKey(signing.PublicKey()).Sign(signing).Build()
}

func BuildStatusMsg(signing Signer, s *Set) *Msg {
	return buildStatusMsg(signing, s, preRound)
}

func validate(m *Msg) bool {
	return true
}

func TestStatusTracker_RecordStatus(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	s.Add(value2)

	mch := make(chan types.MalfeasanceGossip, lowThresh10)
	tracker := newStatusTracker(logtest.New(t), mch, lowThresh10, lowThresh10)
	assert.False(t, tracker.IsSVPReady())

	for i := 0; i < lowThresh10; i++ {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		tracker.RecordStatus(context.Background(), BuildPreRoundMsg(sig, s, nil))
		assert.False(t, tracker.IsSVPReady())
	}

	tracker.AnalyzeStatuses(validate)
	assert.True(t, tracker.IsSVPReady())
	require.Empty(t, mch)
}

func TestStatusTracker_BuildUnionSet(t *testing.T) {
	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig3, err := signing.NewEdSigner()
	require.NoError(t, err)

	mch := make(chan types.MalfeasanceGossip, lowThresh10)
	tracker := newStatusTracker(logtest.New(t), mch, lowThresh10, lowThresh10)

	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	tracker.RecordStatus(context.Background(), BuildStatusMsg(sig1, s))
	s.Add(value2)
	tracker.RecordStatus(context.Background(), BuildStatusMsg(sig2, s))
	s.Add(value3)
	tracker.RecordStatus(context.Background(), BuildStatusMsg(sig3, s))

	g := tracker.buildUnionSet(defaultSetSize)
	assert.True(t, s.Equals(g))
	require.Empty(t, mch)
}

func TestStatusTracker_IsSVPReady(t *testing.T) {
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	mch := make(chan types.MalfeasanceGossip, 1)
	tracker := newStatusTracker(logtest.New(t), mch, 1, 1)
	assert.False(t, tracker.IsSVPReady())
	s := NewSetFromValues(value1)
	tracker.RecordStatus(context.Background(), BuildStatusMsg(sig, s))
	tracker.AnalyzeStatuses(validate)
	assert.True(t, tracker.IsSVPReady())
	require.Empty(t, mch)
}

func TestStatusTracker_BuildSVP(t *testing.T) {
	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)

	mch := make(chan types.MalfeasanceGossip, 1)
	tracker := newStatusTracker(logtest.New(t), mch, 2, 1)
	s := NewSetFromValues(value1)
	tracker.RecordStatus(context.Background(), BuildStatusMsg(sig1, s))
	tracker.RecordStatus(context.Background(), BuildStatusMsg(sig2, s))
	tracker.AnalyzeStatuses(validate)
	svp := tracker.BuildSVP()
	assert.Equal(t, 2, len(svp.Messages))
	require.Empty(t, mch)
}

func TestStatusTracker_ProposalSetTypeA(t *testing.T) {
	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)

	mch := make(chan types.MalfeasanceGossip, 1)
	tracker := newStatusTracker(logtest.New(t), mch, 2, 1)
	s1 := NewSetFromValues(value1)
	s2 := NewSetFromValues(value1, value2)
	tracker.RecordStatus(context.Background(), buildStatusMsg(sig1, s1, preRound))
	tracker.RecordStatus(context.Background(), buildStatusMsg(sig2, s2, preRound))
	proposedSet := tracker.ProposalSet(2)
	assert.NotNil(t, proposedSet)
	assert.True(t, proposedSet.Equals(s1.Union(s2)))
	require.Empty(t, mch)
}

func TestStatusTracker_ProposalSetTypeB(t *testing.T) {
	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)

	mch := make(chan types.MalfeasanceGossip, 1)
	tracker := newStatusTracker(logtest.New(t), mch, 2, 1)
	s1 := NewSetFromValues(value1, value3)
	s2 := NewSetFromValues(value1, value2)
	tracker.RecordStatus(context.Background(), buildStatusMsg(sig1, s1, 0))
	tracker.RecordStatus(context.Background(), buildStatusMsg(sig2, s2, 2))
	tracker.AnalyzeStatuses(validate)
	proposedSet := tracker.ProposalSet(2)
	assert.NotNil(t, proposedSet)
	assert.True(t, proposedSet.Equals(s2))
	require.Empty(t, mch)
}

func TestStatusTracker_Equivocate(t *testing.T) {
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	mch := make(chan types.MalfeasanceGossip, 1)
	tracker := newStatusTracker(logtest.New(t), mch, 2, 1)
	s1 := NewSetFromValues(value1, value3)
	s2 := NewSetFromValues(value1, value2)
	m1 := buildStatusMsg(sig, s1, 0)
	m2 := buildStatusMsg(sig, s2, 0)
	tracker.RecordStatus(context.Background(), m1)
	tracker.RecordStatus(context.Background(), m2)
	tracker.AnalyzeStatuses(validate)
	proposedSet := tracker.ProposalSet(2)
	assert.NotNil(t, proposedSet)
	assert.True(t, proposedSet.Equals(s1), proposedSet)
	require.Len(t, mch, 1)
	expected := types.MalfeasanceGossip{
		MalfeasanceProof: types.MalfeasanceProof{
			Layer: m1.Layer,
			ProofData: types.TypedProof{
				Type: types.HareEquivocation,
				Proof: &types.HareProof{
					Messages: [2]types.HareProofMsg{
						{
							InnerMsg:  m1.HareMetadata,
							Signature: m1.Signature,
						},
						{
							InnerMsg:  m2.HareMetadata,
							Signature: m2.Signature,
						},
					},
				},
			},
		},
		Eligibility: &types.HareEligibilityGossip{
			Layer:       m2.Layer,
			Round:       m2.Round,
			PubKey:      m2.PubKey.Bytes(),
			Eligibility: m2.Eligibility,
		},
	}
	gossip := <-mch
	require.Equal(t, expected, gossip)
}

func TestStatusTracker_AnalyzeStatuses(t *testing.T) {
	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig3, err := signing.NewEdSigner()
	require.NoError(t, err)

	mch := make(chan types.MalfeasanceGossip, 1)
	tracker := newStatusTracker(logtest.New(t), mch, 2, 1)
	s1 := NewSetFromValues(value1, value3)
	s2 := NewSetFromValues(value1, value2)
	tracker.RecordStatus(context.Background(), buildStatusMsg(sig1, s1, 2))
	tracker.RecordStatus(context.Background(), buildStatusMsg(sig2, s2, 1))
	tracker.RecordStatus(context.Background(), buildStatusMsg(sig3, s2, 2))
	tracker.AnalyzeStatuses(validate)
	assert.Equal(t, 3, int(tracker.count))
	require.Empty(t, mch)
}
