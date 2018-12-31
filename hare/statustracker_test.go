package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/stretchr/testify/assert"
	"testing"
)

func BuildStatusMsg(pubKey crypto.PublicKey, s *Set) *pb.HareMessage {
	builder := NewMessageBuilder()
	builder.SetType(Status).SetInstanceId(*instanceId1).SetRoundCounter(k).SetKi(ki).SetValues(s)
	builder = builder.SetPubKey(pubKey).Sign(NewMockSigning())

	return builder.Build()
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
