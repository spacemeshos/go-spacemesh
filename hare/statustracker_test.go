package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/stretchr/testify/assert"
	"testing"
)

func BuildStatusMsg(pubKey crypto.PublicKey, s *Set) *pb.HareMessage {
	builder := NewMessageBuilder()
	builder.SetType(Status).SetSetId(*setId1).SetIteration(k).SetKi(ki).SetValues(s)
	builder = builder.SetPubKey(pubKey).Sign(NewMockSigning())

	return builder.Build()
}

func TestStatusTracker_RecordStatus(t *testing.T) {
	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	s.Add(value2)

	m1 := BuildStatusMsg(generatePubKey(t), s)
	tracker := NewStatusTracker(lowThresh10, lowThresh10)
	tracker.RecordStatus(m1)
	assert.False(t, tracker.IsSVPReady())

	for i := 0; i < lowThresh10; i++ {
		m := BuildPreRoundMsg(generatePubKey(t), s)
		tracker.RecordStatus(m)
	}

	assert.True(t, tracker.IsSVPReady())
}

func TestStatusTracker_BuildUnionSet(t *testing.T) {
	tracker := NewStatusTracker(lowThresh10, lowThresh10)

	s := NewEmptySet(lowDefaultSize)
	s.Add(value1)
	s.Add(value2)
	m := BuildStatusMsg(generatePubKey(t), s)
	tracker.RecordStatus(m)
}
