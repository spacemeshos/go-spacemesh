package tortoisebeacon

import (
	"encoding/json"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// ProposalMessage is a message type which is used when sending proposals.
type ProposalMessage struct {
	VRFSignature []byte `json:"vrf_signature"`
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
	ValidVotes            [][]byte `json:"valid_votes"`
	PotentiallyValidVotes [][]byte `json:"potentially_valid_votes"`
	Signature             []byte   `json:"signature"`
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
	RoundID        types.RoundID `json:"round_id"`
	VotesBitVector []uint64      `json:"votes_bit_vector"`
	Signature      []byte        `json:"signature"`
}

// String returns a string form of FollowingVotingMessage.
func (v FollowingVotingMessage) String() string {
	bytes, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}

	return string(bytes)
}
