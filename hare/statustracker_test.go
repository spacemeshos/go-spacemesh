package hare

import (
	"context"
	"github.com/stretchr/testify/assert"
	"testing"
)

func buildStatusMsg(signing Signer, s *Set, ki int32) *Msg {
	builder := newMessageBuilder()
	builder.SetType(status).SetInstanceID(instanceID1).SetRoundCounter(statusRound).SetKi(ki).SetValues(s)
	builder.SetEligibilityCount(1)
	builder = builder.SetPubKey(signing.PublicKey()).Sign(signing)

	return builder.Build()
}

func BuildStatusMsg(signing Signer, s *Set) *Msg {
	return buildStatusMsg(signing, s, -1)
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
		tracker.RecordStatus(context.TODO(), BuildPreRoundMsg(generateSigning(t), s))
		assert.False(t, tracker.IsSVPReady())
	}

	tracker.AnalyzeStatuses(validate)
	assert.True(t, tracker.IsSVPReady())
}

func TestStatusTracker_BuildUnionSet(t *testing.T) {
	tracker := newStatusTracker(lowThresh10, lowThresh10)

	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	tracker.RecordStatus(context.TODO(), BuildStatusMsg(generateSigning(t), s))
	s.Add(value2)
	tracker.RecordStatus(context.TODO(), BuildStatusMsg(generateSigning(t), s))
	s.Add(value3)
	tracker.RecordStatus(context.TODO(), BuildStatusMsg(generateSigning(t), s))

	g := tracker.buildUnionSet(defaultSetSize)
	assert.True(t, s.Equals(g))
}

func TestStatusTracker_IsSVPReady(t *testing.T) {
	tracker := newStatusTracker(1, 1)
	assert.False(t, tracker.IsSVPReady())
	s := NewSetFromValues(value1)
	tracker.RecordStatus(context.TODO(), BuildStatusMsg(generateSigning(t), s))
	tracker.AnalyzeStatuses(validate)
	assert.True(t, tracker.IsSVPReady())
}

func TestStatusTracker_BuildSVP(t *testing.T) {
	tracker := newStatusTracker(2, 1)
	s := NewSetFromValues(value1)
	tracker.RecordStatus(context.TODO(), BuildStatusMsg(generateSigning(t), s))
	tracker.RecordStatus(context.TODO(), BuildStatusMsg(generateSigning(t), s))
	tracker.AnalyzeStatuses(validate)
	svp := tracker.BuildSVP()
	assert.Equal(t, 2, len(svp.Messages))
}

func TestStatusTracker_ProposalSetTypeA(t *testing.T) {
	tracker := newStatusTracker(2, 1)
	s1 := NewSetFromValues(value1)
	s2 := NewSetFromValues(value1, value2)
	tracker.RecordStatus(context.TODO(), buildStatusMsg(generateSigning(t), s1, -1))
	tracker.RecordStatus(context.TODO(), buildStatusMsg(generateSigning(t), s2, -1))
	proposedSet := tracker.ProposalSet(2)
	assert.NotNil(t, proposedSet)
	assert.True(t, proposedSet.Equals(s1.Union(s2)))
}

func TestStatusTracker_ProposalSetTypeB(t *testing.T) {
	tracker := newStatusTracker(2, 1)
	s1 := NewSetFromValues(value1, value3)
	s2 := NewSetFromValues(value1, value2)
	tracker.RecordStatus(context.TODO(), buildStatusMsg(generateSigning(t), s1, 0))
	tracker.RecordStatus(context.TODO(), buildStatusMsg(generateSigning(t), s2, 2))
	tracker.AnalyzeStatuses(validate)
	proposedSet := tracker.ProposalSet(2)
	assert.NotNil(t, proposedSet)
	assert.True(t, proposedSet.Equals(s2))
}

func TestStatusTracker_AnalyzeStatuses(t *testing.T) {
	tracker := newStatusTracker(2, 1)
	s1 := NewSetFromValues(value1, value3)
	s2 := NewSetFromValues(value1, value2)
	tracker.RecordStatus(context.TODO(), buildStatusMsg(generateSigning(t), s1, 2))
	tracker.RecordStatus(context.TODO(), buildStatusMsg(generateSigning(t), s2, 1))
	tracker.RecordStatus(context.TODO(), buildStatusMsg(generateSigning(t), s2, 2))
	tracker.AnalyzeStatuses(validate)
	assert.Equal(t, 3, int(tracker.count))
}
