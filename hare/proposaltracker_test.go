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

func buildProposalMsg(signing Signer, s *Set, signature []byte) *Msg {
	builder := newMessageBuilder().SetRoleProof(signature)
	builder.SetType(proposal).SetLayer(instanceID1).SetRoundCounter(proposalRound).SetCommittedRound(ki).SetValues(s).SetSVP(buildSVP(ki, NewSetFromValues(value1)))
	builder.SetEligibilityCount(1)
	return builder.SetPubKey(signing.PublicKey()).Sign(signing).Build()
}

func BuildProposalMsg(signing Signer, s *Set) *Msg {
	return buildProposalMsg(signing, s, []byte{})
}

func TestProposalTracker_OnProposalConflict(t *testing.T) {
	s := NewSetFromValues(value1, value2)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	m1 := BuildProposalMsg(signer, s)
	mch := make(chan types.MalfeasanceGossip, 1)
	tracker := newProposalTracker(logtest.New(t), mch)
	tracker.OnProposal(context.Background(), m1)
	assert.False(t, tracker.IsConflicting())
	g := NewSetFromValues(value3)
	m2 := BuildProposalMsg(signer, g)
	tracker.OnProposal(context.Background(), m2)
	assert.True(t, tracker.IsConflicting())
	require.Len(t, mch, 1)
	expected := types.MalfeasanceGossip{
		MalfeasanceProof: types.MalfeasanceProof{
			Layer: m1.Layer,
			Proof: types.Proof{
				Type: types.HareEquivocation,
				Data: &types.HareProof{
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

func TestProposalTracker_IsConflicting(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	mch := make(chan types.MalfeasanceGossip, 1)
	tracker := newProposalTracker(logtest.New(t), mch)

	for i := 0; i < lowThresh10; i++ {
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)

		tracker.OnProposal(context.Background(), BuildProposalMsg(signer, s))
		assert.False(t, tracker.IsConflicting())
	}
	require.Empty(t, mch)
}

func TestProposalTracker_OnLateProposal(t *testing.T) {
	s := NewSetFromValues(value1, value2)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	m1 := BuildProposalMsg(signer, s)
	mch := make(chan types.MalfeasanceGossip, 1)
	tracker := newProposalTracker(logtest.New(t), mch)
	tracker.OnProposal(context.Background(), m1)
	assert.False(t, tracker.IsConflicting())
	g := NewSetFromValues(value3)
	m2 := BuildProposalMsg(signer, g)
	tracker.OnLateProposal(context.Background(), m2)
	assert.True(t, tracker.IsConflicting())
	require.Len(t, mch, 1)
	expected := types.MalfeasanceGossip{
		MalfeasanceProof: types.MalfeasanceProof{
			Layer: m1.Layer,
			Proof: types.Proof{
				Type: types.HareEquivocation,
				Data: &types.HareProof{
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

func TestProposalTracker_ProposedSet(t *testing.T) {
	mch := make(chan types.MalfeasanceGossip, 1)
	tracker := newProposalTracker(logtest.New(t), mch)
	proposedSet := tracker.ProposedSet()
	assert.Nil(t, proposedSet)
	s1 := NewSetFromValues(value1, value2)
	signer1, err := signing.NewEdSigner()
	require.NoError(t, err)
	tracker.OnProposal(context.Background(), buildProposalMsg(signer1, s1, []byte{1, 2, 3}))
	proposedSet = tracker.ProposedSet()
	assert.NotNil(t, proposedSet)
	assert.True(t, s1.Equals(proposedSet))
	s2 := NewSetFromValues(value3, value4, value5)
	signer2, err := signing.NewEdSigner()
	require.NoError(t, err)
	m1 := buildProposalMsg(signer2, s2, []byte{0})
	tracker.OnProposal(context.Background(), m1)
	proposedSet = tracker.ProposedSet()
	assert.True(t, s2.Equals(proposedSet))
	m2 := buildProposalMsg(signer2, s1, []byte{0})
	tracker.OnProposal(context.Background(), m2)
	proposedSet = tracker.ProposedSet()
	assert.Nil(t, proposedSet)
	require.Len(t, mch, 1)
	expected := types.MalfeasanceGossip{
		MalfeasanceProof: types.MalfeasanceProof{
			Layer: m1.Layer,
			Proof: types.Proof{
				Type: types.HareEquivocation,
				Data: &types.HareProof{
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
