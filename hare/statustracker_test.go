package hare

import (
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/stretchr/testify/assert"
	"testing"
)

func buildStatusMsg(signing Signing, s *Set, ki int32) *pb.HareMessage {
	builder := NewMessageBuilder()
	builder.SetType(Status).SetInstanceId(instanceId1).SetRoundCounter(Round1).SetKi(ki).SetValues(s)
	builder = builder.SetPubKey(signing.Verifier().Bytes()).Sign(signing)

	return builder.Build()
}

func BuildStatusMsg(signing Signing, s *Set) *pb.HareMessage {
	return buildStatusMsg(signing, s, -1)
}

func validate(m *pb.HareMessage) bool {
	return true
}

func TestStatusTracker_RecordStatus(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	s.Add(value2)

	tracker := NewStatusTracker(lowThresh10, lowThresh10)
	assert.False(t, tracker.IsSVPReady())

	for i := 0; i < lowThresh10; i++ {
		tracker.RecordStatus(BuildPreRoundMsg(generateSigning(t), s))
		assert.False(t, tracker.IsSVPReady())
	}

	tracker.AnalyzeStatuses(validate)
	assert.True(t, tracker.IsSVPReady())
}

func TestStatusTracker_BuildUnionSet(t *testing.T) {
	tracker := NewStatusTracker(lowThresh10, lowThresh10)

	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	tracker.RecordStatus(BuildStatusMsg(generateSigning(t), s))
	s.Add(value2)
	tracker.RecordStatus(BuildStatusMsg(generateSigning(t), s))
	s.Add(value3)
	tracker.RecordStatus(BuildStatusMsg(generateSigning(t), s))

	g := tracker.buildUnionSet(defaultSetSize)
	assert.True(t, s.Equals(g))
}

func TestStatusTracker_IsSVPReady(t *testing.T) {
	tracker := NewStatusTracker(1, 1)
	assert.False(t, tracker.IsSVPReady())
	s := NewSetFromValues(value1)
	tracker.RecordStatus(BuildStatusMsg(generateSigning(t), s))
	tracker.AnalyzeStatuses(validate)
	assert.True(t, tracker.IsSVPReady())
}

func TestStatusTracker_BuildSVP(t *testing.T) {
	tracker := NewStatusTracker(2, 1)
	s := NewSetFromValues(value1)
	tracker.RecordStatus(BuildStatusMsg(generateSigning(t), s))
	tracker.RecordStatus(BuildStatusMsg(generateSigning(t), s))
	tracker.AnalyzeStatuses(validate)
	svp := tracker.BuildSVP()
	assert.Equal(t, 2, len(svp.Messages))
}

func TestStatusTracker_ProposalSetTypeA(t *testing.T) {
	tracker := NewStatusTracker(2, 1)
	s1 := NewSetFromValues(value1)
	s2 := NewSetFromValues(value1, value2)
	tracker.RecordStatus(buildStatusMsg(generateSigning(t), s1, -1))
	tracker.RecordStatus(buildStatusMsg(generateSigning(t), s2, -1))
	proposedSet := tracker.ProposalSet(2)
	assert.NotNil(t, proposedSet)
	assert.True(t, proposedSet.Equals(s1.Union(s2)))
}

func TestStatusTracker_ProposalSetTypeB(t *testing.T) {
	tracker := NewStatusTracker(2, 1)
	s1 := NewSetFromValues(value1, value3)
	s2 := NewSetFromValues(value1, value2)
	tracker.RecordStatus(buildStatusMsg(generateSigning(t), s1, 0))
	tracker.RecordStatus(buildStatusMsg(generateSigning(t), s2, 2))
	tracker.AnalyzeStatuses(validate)
	proposedSet := tracker.ProposalSet(2)
	assert.NotNil(t, proposedSet)
	assert.True(t, proposedSet.Equals(s2))
}

func TestStatusTracker_AnalyzeStatuses(t *testing.T) {
	tracker := NewStatusTracker(2, 1)
	s1 := NewSetFromValues(value1, value3)
	s2 := NewSetFromValues(value1, value2)
	tracker.RecordStatus(buildStatusMsg(generateSigning(t), s1, 2))
	tracker.RecordStatus(buildStatusMsg(generateSigning(t), s2, 1))
	tracker.RecordStatus(buildStatusMsg(generateSigning(t), s2, 2))
	tracker.AnalyzeStatuses(validate)
	assert.Equal(t, 2, len(tracker.statuses))
}
