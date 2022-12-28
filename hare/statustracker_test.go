package hare

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/signing"
)

func buildStatusMsg(signing Signer, s *Set, ki uint32) *Msg {
	builder := newMessageBuilder()
	builder.SetType(status).SetInstanceID(instanceID1).SetRoundCounter(statusRound).SetKi(ki).SetValues(s)
	builder.SetEligibilityCount(1)
	builder = builder.SetPubKey(signing.PublicKey()).Sign(signing)

	return builder.Build()
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

	tracker := newStatusTracker(lowThresh10, lowThresh10)
	assert.False(t, tracker.IsSVPReady())

	for i := 0; i < lowThresh10; i++ {
		sig, err := signing.NewEdSigner()
		require.NoError(t, err)
		tracker.RecordStatus(context.Background(), BuildPreRoundMsg(sig, s, nil))
		assert.False(t, tracker.IsSVPReady())
	}

	tracker.AnalyzeStatuses(validate)
	assert.True(t, tracker.IsSVPReady())
}

func TestStatusTracker_BuildUnionSet(t *testing.T) {
	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig3, err := signing.NewEdSigner()
	require.NoError(t, err)

	tracker := newStatusTracker(lowThresh10, lowThresh10)

	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	tracker.RecordStatus(context.Background(), BuildStatusMsg(sig1, s))
	s.Add(value2)
	tracker.RecordStatus(context.Background(), BuildStatusMsg(sig2, s))
	s.Add(value3)
	tracker.RecordStatus(context.Background(), BuildStatusMsg(sig3, s))

	g := tracker.buildUnionSet(defaultSetSize)
	assert.True(t, s.Equals(g))
}

func TestStatusTracker_IsSVPReady(t *testing.T) {
	sig, err := signing.NewEdSigner()
	require.NoError(t, err)

	tracker := newStatusTracker(1, 1)
	assert.False(t, tracker.IsSVPReady())
	s := NewSetFromValues(value1)
	tracker.RecordStatus(context.Background(), BuildStatusMsg(sig, s))
	tracker.AnalyzeStatuses(validate)
	assert.True(t, tracker.IsSVPReady())
}

func TestStatusTracker_BuildSVP(t *testing.T) {
	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)

	tracker := newStatusTracker(2, 1)
	s := NewSetFromValues(value1)
	tracker.RecordStatus(context.Background(), BuildStatusMsg(sig1, s))
	tracker.RecordStatus(context.Background(), BuildStatusMsg(sig2, s))
	tracker.AnalyzeStatuses(validate)
	svp := tracker.BuildSVP()
	assert.Equal(t, 2, len(svp.Messages))
}

func TestStatusTracker_ProposalSetTypeA(t *testing.T) {
	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)

	tracker := newStatusTracker(2, 1)
	s1 := NewSetFromValues(value1)
	s2 := NewSetFromValues(value1, value2)
	tracker.RecordStatus(context.Background(), buildStatusMsg(sig1, s1, preRound))
	tracker.RecordStatus(context.Background(), buildStatusMsg(sig2, s2, preRound))
	proposedSet := tracker.ProposalSet(2)
	assert.NotNil(t, proposedSet)
	assert.True(t, proposedSet.Equals(s1.Union(s2)))
}

func TestStatusTracker_ProposalSetTypeB(t *testing.T) {
	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)

	tracker := newStatusTracker(2, 1)
	s1 := NewSetFromValues(value1, value3)
	s2 := NewSetFromValues(value1, value2)
	tracker.RecordStatus(context.Background(), buildStatusMsg(sig1, s1, 0))
	tracker.RecordStatus(context.Background(), buildStatusMsg(sig2, s2, 2))
	tracker.AnalyzeStatuses(validate)
	proposedSet := tracker.ProposalSet(2)
	assert.NotNil(t, proposedSet)
	assert.True(t, proposedSet.Equals(s2))
}

func TestStatusTracker_AnalyzeStatuses(t *testing.T) {
	sig1, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig2, err := signing.NewEdSigner()
	require.NoError(t, err)
	sig3, err := signing.NewEdSigner()
	require.NoError(t, err)

	tracker := newStatusTracker(2, 1)
	s1 := NewSetFromValues(value1, value3)
	s2 := NewSetFromValues(value1, value2)
	tracker.RecordStatus(context.Background(), buildStatusMsg(sig1, s1, 2))
	tracker.RecordStatus(context.Background(), buildStatusMsg(sig2, s2, 1))
	tracker.RecordStatus(context.Background(), buildStatusMsg(sig3, s2, 2))
	tracker.AnalyzeStatuses(validate)
	assert.Equal(t, 3, int(tracker.count))
}
