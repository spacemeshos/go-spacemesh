package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/stretchr/testify/assert"
	"testing"
)

func buildStatusMsg(pub crypto.PublicKey, s *Set, ki int32) *pb.HareMessage {
	builder := NewMessageBuilder()
	builder.SetType(Status).SetInstanceId(*instanceId1).SetRoundCounter(Round1).SetKi(ki).SetValues(s)
	builder = builder.SetPubKey(pub).Sign(NewMockSigning())

	return builder.Build()
}

func BuildStatusMsg(pubKey crypto.PublicKey, s *Set) *pb.HareMessage {
	return buildStatusMsg(pubKey, s, -1)
}

func TestStatusTracker_RecordStatus(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	s.Add(value2)

	tracker := NewStatusTracker(lowThresh10+1, lowThresh10)
	assert.False(t, tracker.IsSVPReady())

	for i := 0; i < lowThresh10; i++ {
		tracker.RecordStatus(BuildPreRoundMsg(generatePubKey(t), s))
		assert.False(t, tracker.IsSVPReady())
	}

	tracker.RecordStatus(BuildPreRoundMsg(generatePubKey(t), s))
	assert.True(t, tracker.IsSVPReady())
}

func TestStatusTracker_BuildUnionSet(t *testing.T) {
	tracker := NewStatusTracker(lowThresh10, lowThresh10)

	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	tracker.RecordStatus(BuildStatusMsg(generatePubKey(t), s))
	s.Add(value2)
	tracker.RecordStatus(BuildStatusMsg(generatePubKey(t), s))
	s.Add(value3)
	tracker.RecordStatus(BuildStatusMsg(generatePubKey(t), s))

	g := tracker.buildUnionSet(cfg.SetSize)
	assert.True(t, s.Equals(g))
}

func TestStatusTracker_IsSVPReady(t *testing.T) {
	tracker := NewStatusTracker(1, 1)
	assert.False(t, tracker.IsSVPReady())
	s := NewSetFromValues(value1)
	tracker.RecordStatus(BuildStatusMsg(generatePubKey(t), s))
	assert.True(t, tracker.IsSVPReady())
}

func TestStatusTracker_BuildSVP(t *testing.T) {
	tracker := NewStatusTracker(2, 1)
	s := NewSetFromValues(value1)
	tracker.RecordStatus(BuildStatusMsg(generatePubKey(t), s))
	tracker.RecordStatus(BuildStatusMsg(generatePubKey(t), s))
	svp := tracker.BuildSVP()
	assert.Equal(t, 2, len(svp.Messages))
}

func TestStatusTracker_ProposalSetTypeA(t *testing.T) {
	tracker := NewStatusTracker(2, 1)
	s1 := NewSetFromValues(value1)
	s2 := NewSetFromValues(value1, value2)
	tracker.RecordStatus(buildStatusMsg(generatePubKey(t), s1, -1))
	tracker.RecordStatus(buildStatusMsg(generatePubKey(t), s2, -1))
	proposedSet := tracker.ProposalSet(2)
	assert.NotNil(t, proposedSet)
	assert.True(t, proposedSet.Equals(s1.Union(s2)))
}

func TestStatusTracker_ProposalSetTypeB(t *testing.T) {
	tracker := NewStatusTracker(2, 1)
	s1 := NewSetFromValues(value1, value3)
	s2 := NewSetFromValues(value1, value2)
	tracker.RecordStatus(buildStatusMsg(generatePubKey(t), s1, 0))
	tracker.RecordStatus(buildStatusMsg(generatePubKey(t), s2, 2))
	proposedSet := tracker.ProposalSet(2)
	assert.NotNil(t, proposedSet)
	assert.True(t, proposedSet.Equals(s2))
}