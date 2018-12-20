package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/stretchr/testify/assert"
	"testing"
)

const (
	k           = 1
	ki          = -1
	lowThresh10 = 10
)

var blockId1 = BlockId{Bytes32{1}}
var blockId2 = BlockId{Bytes32{2}}
var blockId3 = BlockId{Bytes32{3}}

func BuildPreRoundMsg(t *testing.T, pubKey crypto.PublicKey, s *Set) *pb.HareMessage {
	builder := NewMessageBuilder()
	builder.SetType(PreRound).SetLayer(*Layer1).SetIteration(k).SetKi(ki).SetBlocks(s)
	builder, err := builder.SetPubKey(pubKey).Sign(NewMockSigning())
	assert.Nil(t, err)

	return builder.Build()
}

func TestPreRoundTracker_OnPreRound(t *testing.T) {
	s := NewEmptySet()
	s.Add(blockId1)
	s.Add(blockId2)
	pubKey := generatePubKey(t)

	m1 := BuildPreRoundMsg(t, pubKey, s)
	tracker := NewPreRoundTracker(lowThresh10, lowThresh10)
	tracker.OnPreRound(m1)
	assert.Equal(t, 1, len(tracker.preRound))      // one msg
	assert.Equal(t, 2, len(tracker.tracker.table)) // two blocks
	m1 = tracker.preRound[pubKey.String()]
	m2 := BuildPreRoundMsg(t, pubKey, s)
	tracker.OnPreRound(m2)
	m2 = tracker.preRound[pubKey.String()]
	assert.Equal(t, m1, m2) // same pub --> same msg
}

func TestPreRoundTracker_CanProveBlockAndSet(t *testing.T) {
	s := NewEmptySet()
	s.Add(blockId1)
	s.Add(blockId2)
	tracker := NewPreRoundTracker(lowThresh10, lowThresh10)

	for i := 0; i < lowThresh10; i++ {
		assert.False(t, tracker.CanProveSet(s))
		m1 := BuildPreRoundMsg(t, generatePubKey(t), s)
		tracker.OnPreRound(m1)
	}

	assert.True(t, tracker.CanProveBlock(blockId1))
	assert.True(t, tracker.CanProveBlock(blockId2))
	assert.True(t, tracker.CanProveSet(s))
}
