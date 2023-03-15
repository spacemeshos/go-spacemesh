package beacon

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate scalegen

// ProposalVrfMessage is a message for buildProposal below.
type ProposalVrfMessage struct {
	Type  types.EligibilityType
	Nonce types.VRFPostIndex
	Epoch types.EpochID
}

// ProposalMessage is a message type which is used when sending proposals.
type ProposalMessage struct {
	EpochID      types.EpochID
	NodeID       types.NodeID
	VRFSignature []byte `scale:"max=32"`
}

type Proposal struct {
	Value []byte `scale:"max=32"` // TODO(mafa): check if max. value is correct
}

// FirstVotingMessageBody is FirstVotingMessage without a signature.
type FirstVotingMessageBody struct {
	EpochID                   types.EpochID
	ValidProposals            []Proposal `scale:"max=32"` // TODO(mafa): check if max. value is correct
	PotentiallyValidProposals []Proposal `scale:"max=32"` // TODO(mafa): check if max. value is correct
}

// FirstVotingMessage is a message type which is used when sending first voting messages.
type FirstVotingMessage struct {
	FirstVotingMessageBody
	Signature []byte `scale:"max=32"`
}

// FollowingVotingMessageBody is FollowingVotingMessage without a signature.
type FollowingVotingMessageBody struct {
	EpochID        types.EpochID
	RoundID        types.RoundID
	VotesBitVector []byte `scale:"max=32"` // TODO(mafa): check if max. value is correct
}

// FollowingVotingMessage is a message type which is used when sending following voting messages.
type FollowingVotingMessage struct {
	FollowingVotingMessageBody
	Signature []byte `scale:"max=32"`
}
