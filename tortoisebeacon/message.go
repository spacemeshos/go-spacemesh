package tortoisebeacon

import (
	"encoding/json"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// ProposalMessage is a message type which is used when sending proposals.
type ProposalMessage struct {
	MinerID      types.NodeID
	VRFSignature []byte
}

// String returns a string form of ProposalMessage.
func (p ProposalMessage) String() string {
	bytes, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}

	return string(bytes)
}

type FirstVotingMessageBody struct {
	MinerID                   types.NodeID
	ValidProposals            [][]byte
	PotentiallyValidProposals [][]byte
}
type FirstVotingMessage struct {
	FirstVotingMessageBody
	Signature []byte // TODO: implement extracting pk from signature
}

// String returns a string form of FirstVotingMessage.
func (v FirstVotingMessage) String() string {
	bytes, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}

	return string(bytes)
}

type FollowingVotingMessageBody struct {
	MinerID        types.NodeID
	EpochID        types.EpochID
	RoundID        types.RoundID
	VotesBitVector []uint64
}

type FollowingVotingMessage struct {
	FollowingVotingMessageBody
	Signature []byte
}

// String returns a string form of FollowingVotingMessage.
func (v FollowingVotingMessage) String() string {
	bytes, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}

	return string(bytes)
}
