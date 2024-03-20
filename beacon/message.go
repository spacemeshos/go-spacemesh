package beacon

import (
	"encoding/hex"

	"github.com/spacemeshos/go-scale"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate scalegen

// ProposalVrfMessage is the payload for the VRF Signature in `ProposalMessage`.
type ProposalVrfMessage struct {
	Type  types.EligibilityType // always types.EligibilityBeacon
	Nonce types.VRFPostIndex
	Epoch types.EpochID
}

// ProposalMessage is a message type which is used when sending proposals.
type ProposalMessage struct {
	EpochID      types.EpochID
	NodeID       types.NodeID
	VRFSignature types.VrfSignature
}

type Proposal [types.BeaconSize]byte

// EncodeScale implements scale codec interface.
func (p *Proposal) EncodeScale(e *scale.Encoder) (int, error) {
	return scale.EncodeByteArray(e, p[:])
}

// DecodeScale implements scale codec interface.
func (p *Proposal) DecodeScale(d *scale.Decoder) (int, error) {
	return scale.DecodeByteArray(d, p[:])
}

func (p Proposal) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("proposal", hex.EncodeToString(p[:]))
	return nil
}

func ProposalFromVrf(vrf types.VrfSignature) Proposal {
	var p Proposal
	copy(p[:], vrf[:])
	return p
}

// FirstVotingMessageBody is FirstVotingMessage without a signature.
type FirstVotingMessageBody struct {
	EpochID types.EpochID
	// number of proposals is expected to be under 100, 1000 is a safe upper bound
	// This expectation holds true for any number of smeshers on the network
	ValidProposals []Proposal `scale:"max=1000"`
	// number of proposals is expected to be under 100, 1000 is a safe upper bound
	// This expectation holds true for any number of smeshers on the network
	PotentiallyValidProposals []Proposal `scale:"max=1000"`
}

// FirstVotingMessage is a message type which is used when sending first voting messages.
type FirstVotingMessage struct {
	FirstVotingMessageBody

	SmesherID types.NodeID
	Signature types.EdSignature
}

// FollowingVotingMessageBody is FollowingVotingMessage without a signature.
type FollowingVotingMessageBody struct {
	EpochID        types.EpochID
	RoundID        types.RoundID
	VotesBitVector []byte `scale:"max=128"` // 128 bytes = 1024 bits and we limit the number of proposals to 1000
}

// FollowingVotingMessage is a message type which is used when sending following voting messages.
type FollowingVotingMessage struct {
	FollowingVotingMessageBody

	SmesherID types.NodeID
	Signature types.EdSignature
}
