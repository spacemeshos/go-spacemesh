package tortoisebeacon

import (
	"encoding/json"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

// ProposalMessage is a message type which is used when sending proposals.
type ProposalMessage struct {
	EpochID      types.EpochID
	MinerPK      []byte
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

type proposalMessageWithReceiptData struct {
	message      ProposalMessage
	gossip       service.GossipMessage
	receivedTime time.Time
}

// FirstVotingMessageBody is FirstVotingMessage without a signature.
type FirstVotingMessageBody struct {
	ValidProposals            [][]byte
	PotentiallyValidProposals [][]byte
}

// FirstVotingMessage is a message type which is used when sending first voting messages.
type FirstVotingMessage struct {
	FirstVotingMessageBody
	Signature []byte
}

// String returns a string form of FirstVotingMessage.
func (v FirstVotingMessage) String() string {
	bytes, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}

	return string(bytes)
}

// FollowingVotingMessageBody is FollowingVotingMessage without a signature.
type FollowingVotingMessageBody struct {
	MinerID        types.NodeID
	RoundID        types.RoundID
	VotesBitVector []uint64
}

// FollowingVotingMessage is a message type which is used when sending following voting messages.
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
