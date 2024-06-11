package wire

import (
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hash"
)

//go:generate scalegen

// NIPostChallengeV2 collects information needed to generate a PoET challenge.
// It's hash is the challenge registered in the PoET.
type NIPostChallengeV2 struct {
	PublishEpoch     types.EpochID
	PrevATXID        types.ATXID
	PositioningATXID types.ATXID
	InitialPost      *PostV1
}

// Hash serializes the NIPostChallenge and returns its hash.
// The serialized challenge is first prepended with a byte 0x00, and then hashed
// for second preimage resistance of poet membership merkle tree.
func (c *NIPostChallengeV2) Hash() types.Hash32 {
	ncBytes := codec.MustEncode(c)
	return hash.Sum([]byte{0x00}, ncBytes)
}

func (c *NIPostChallengeV2) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	if c == nil {
		return nil
	}
	encoder.AddString("Hash", c.Hash().String())
	encoder.AddUint32("PublishEpoch", c.PublishEpoch.Uint32())
	encoder.AddString("PrevATXID", c.PrevATXID.String())
	encoder.AddString("PositioningATX", c.PositioningATXID.String())
	encoder.AddObject("InitialPost", c.InitialPost)
	return nil
}

func NIPostChallengeToWireV2(c *types.NIPostChallenge) *NIPostChallengeV2 {
	return &NIPostChallengeV2{
		PublishEpoch:     c.PublishEpoch,
		PrevATXID:        c.PrevATXID,
		PositioningATXID: c.PositioningATX,
		InitialPost:      PostToWireV1(c.InitialPost),
	}
}
