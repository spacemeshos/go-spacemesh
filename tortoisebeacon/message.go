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
	MinerPK      []byte `ssz-max:"256"`
	VRFSignature []byte `ssz-max:"256"`
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
	ValidProposals            [][]byte `ssz-size:"?,?" ssz-max:"4096,4096"`
	PotentiallyValidProposals [][]byte `ssz-size:"?,?" ssz-max:"4096,4096"`
}

// FirstVotingMessage is a message type which is used when sending first voting messages.
type FirstVotingMessage struct {
	FirstVotingMessageBody
	Signature []byte `ssz-max:"256"`
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
	VotesBitVector []uint64 `ssz-max:"4096"`
}

// FollowingVotingMessage is a message type which is used when sending following voting messages.
type FollowingVotingMessage struct {
	FollowingVotingMessageBody
	Signature []byte `ssz-max:"256"`
}

// String returns a string form of FollowingVotingMessage.
func (v FollowingVotingMessage) String() string {
	bytes, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}

	return string(bytes)
}

type vrfMessage struct {
	Prefix string `ssz-max:"64"`
	Epoch  uint32
}
