package wire

import (
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hash"
)

//go:generate scalegen

type NIPostChallengeV1 struct {
	PublishEpoch types.EpochID
	// Sequence number counts the number of ancestors of the ATX. It sequentially increases for each ATX in the chain.
	// Two ATXs with the same sequence number from the same miner can be used as the proof of malfeasance against
	// that miner.
	Sequence uint64
	// the previous ATX's ID (for all but the first in the sequence)
	PrevATXID        types.ATXID
	PositioningATXID types.ATXID

	// CommitmentATXID is the ATX used in the commitment for initializing the PoST of the node.
	CommitmentATXID *types.ATXID
	InitialPost     *PostV1
}

// Hash serializes the NIPostChallenge and returns its hash.
// The serialized challenge is first prepended with a byte 0x00, and then hashed
// for second preimage resistance of poet membership merkle tree.
func (c *NIPostChallengeV1) Hash() types.Hash32 {
	ncBytes := codec.MustEncode(c)
	return hash.Sum([]byte{0x00}, ncBytes)
}

func (c *NIPostChallengeV1) Publish() types.EpochID {
	return c.PublishEpoch
}

func (c *NIPostChallengeV1) PrevATX() types.ATXID {
	return c.PrevATXID
}

func (c *NIPostChallengeV1) PositioningATX() types.ATXID {
	return c.PositioningATXID
}

func (c *NIPostChallengeV1) CommitmentATX() *types.ATXID {
	return c.CommitmentATXID
}

func (c *NIPostChallengeV1) MaybeSequence() *uint64 {
	return &c.Sequence
}
