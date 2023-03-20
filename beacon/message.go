package beacon

import (
	"github.com/spacemeshos/go-scale"
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
	VRFSignature []byte `scale:"max=80"`
}

type Proposal [4]byte

func ProposalFromString(s string) Proposal {
	var p Proposal
	copy(p[:], []byte(s))
	return p
}

func (p Proposal) String() string {
	return string(p[:])
}

// EncodeScale implements scale codec interface.
func (p *Proposal) EncodeScale(e *scale.Encoder) (int, error) {
	return scale.EncodeByteArray(e, p[:])
}

// DecodeScale implements scale codec interface.
func (p *Proposal) DecodeScale(d *scale.Decoder) (int, error) {
	return scale.DecodeByteArray(d, p[:])
}

// FirstVotingMessageBody is FirstVotingMessage without a signature.
type FirstVotingMessageBody struct {
	EpochID                   types.EpochID
	ValidProposals            []Proposal `scale:"max=200"`
	PotentiallyValidProposals []Proposal `scale:"max=200"`
}

// FirstVotingMessage is a message type which is used when sending first voting messages.
type FirstVotingMessage struct {
	FirstVotingMessageBody
	Signature []byte `scale:"max=64"`
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
	Signature []byte `scale:"max=64"`
}
