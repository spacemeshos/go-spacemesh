package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/stretchr/testify/assert"
	"testing"
)

func BuildStatusMsg(t *testing.T, pubKey crypto.PublicKey, s *Set) *pb.HareMessage {
	builder := NewMessageBuilder()
	builder.SetType(Status).SetLayer(*Layer1).SetIteration(k).SetKi(ki).SetBlocks(s)
	builder, err := builder.SetPubKey(pubKey).Sign(NewMockSigning())
	assert.Nil(t, err)

	return builder.Build()
}

func TestStatusTracker_RecordStatus(t *testing.T) {
	s := NewEmptySet()
	s.Add(blockId1)
	s.Add(blockId2)

	m1 := BuildStatusMsg(t, generatePubKey(t), s)
	tracker := NewStatusTracker(lowThresh10, lowThresh10)
	tracker.RecordStatus(m1)
	assert.False(t, tracker.IsSVPReady())

	for i := 0; i < lowThresh10; i++ {
		m := BuildPreRoundMsg(t, generatePubKey(t), s)
		tracker.RecordStatus(m)
	}

	assert.True(t, tracker.IsSVPReady())
}

func TestStatusTracker_BuildUnionSet(t *testing.T) {
	tracker := NewStatusTracker(lowThresh10, lowThresh10)

	s := NewEmptySet()
	s.Add(blockId1)
	s.Add(blockId2)
	m := BuildStatusMsg(t, generatePubKey(t), s)
	tracker.RecordStatus(m)
}
