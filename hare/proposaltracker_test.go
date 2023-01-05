package hare

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
)

func buildProposalMsg(signing Signer, s *Set, signature []byte) *Msg {
	builder := newMessageBuilder().SetRoleProof(signature)
	builder.SetType(proposal).SetLayer(instanceID1).SetRoundCounter(proposalRound).SetCommittedRound(ki).SetValues(s).SetSVP(buildSVP(ki, NewSetFromValues(value1)))
	builder.SetEligibilityCount(1)
	builder = builder.SetPubKey(signing.PublicKey()).Sign(signing)

	return builder.Build()
}

func BuildProposalMsg(signing Signer, s *Set) *Msg {
	return buildProposalMsg(signing, s, []byte{})
}

func TestProposalTracker_OnProposalConflict(t *testing.T) {
	s := NewSetFromValues(value1, value2)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)

	m1 := BuildProposalMsg(signer, s)
	tracker := newProposalTracker(logtest.New(t))
	tracker.OnProposal(context.Background(), m1)
	assert.False(t, tracker.IsConflicting())
	g := NewSetFromValues(value3)
	m2 := BuildProposalMsg(signer, g)
	tracker.OnProposal(context.Background(), m2)
	assert.True(t, tracker.IsConflicting())
}

func TestProposalTracker_IsConflicting(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	tracker := newProposalTracker(logtest.New(t))

	for i := 0; i < lowThresh10; i++ {
		signer, err := signing.NewEdSigner()
		require.NoError(t, err)

		tracker.OnProposal(context.Background(), BuildProposalMsg(signer, s))
		assert.False(t, tracker.IsConflicting())
	}
}

func TestProposalTracker_OnLateProposal(t *testing.T) {
	s := NewSetFromValues(value1, value2)
	signer, err := signing.NewEdSigner()
	require.NoError(t, err)
	m1 := BuildProposalMsg(signer, s)
	tracker := newProposalTracker(logtest.New(t))
	tracker.OnProposal(context.Background(), m1)
	assert.False(t, tracker.IsConflicting())
	g := NewSetFromValues(value3)
	m2 := BuildProposalMsg(signer, g)
	tracker.OnLateProposal(context.Background(), m2)
	assert.True(t, tracker.IsConflicting())
}

func TestProposalTracker_ProposedSet(t *testing.T) {
	tracker := newProposalTracker(logtest.New(t))
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
	tracker.OnProposal(context.Background(), buildProposalMsg(signer2, s2, []byte{0}))
	proposedSet = tracker.ProposedSet()
	assert.True(t, s2.Equals(proposedSet))
	tracker.OnProposal(context.Background(), buildProposalMsg(signer2, s1, []byte{0}))
	proposedSet = tracker.ProposedSet()
	assert.Nil(t, proposedSet)
}
