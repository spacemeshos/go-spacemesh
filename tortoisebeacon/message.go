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

type FirstVotingMessage struct {
	// TODO: miner ID
	MinerID                   types.NodeID
	ValidProposals            [][]byte
	PotentiallyValidProposals [][]byte
	Signature                 []byte // implement extracting pk from signature
}

// String returns a string form of FirstVotingMessage.
func (v FirstVotingMessage) String() string {
	bytes, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}

	return string(bytes)
}

type FollowingVotingMessage struct {
	// TODO: miner ID
	RoundID        types.RoundID
	VotesBitVector []uint64
	Signature      []byte
}

// String returns a string form of FollowingVotingMessage.
func (v FollowingVotingMessage) String() string {
	bytes, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}

	return string(bytes)
}
