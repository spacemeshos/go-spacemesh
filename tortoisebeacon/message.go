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

// FirstVotingMessageBody is FirstVotingMessage without a signature.
type FirstVotingMessageBody struct {
	MinerID                   types.NodeID
	ValidProposals            [][]byte
	PotentiallyValidProposals [][]byte
}

// FirstVotingMessage is a message type which is used when sending first voting messages.
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

// FollowingVotingMessageBody is FollowingVotingMessage without a signature.
type FollowingVotingMessageBody struct {
	MinerID        types.NodeID
	EpochID        types.EpochID
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

// BeaconSyncMessageBody is BeaconSyncMessage without a signature.
type BeaconSyncMessageBody struct {
	MinerID types.NodeID
	EpochID types.EpochID
	Beacon  types.Hash32
}

// BeaconSyncMessage is a message type which is used for synchronizing tortoise beacon values.
type BeaconSyncMessage struct {
	BeaconSyncMessageBody
	Signature []byte // TODO: implement extracting pk from signature
}

// String returns a string form of BeaconSyncMessage.
func (v BeaconSyncMessage) String() string {
	bytes, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}

	return string(bytes)
}
