package tortoisebeacon

import (
	"encoding/json"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// MessageType defines Tortoise Beacon message type.
type MessageType int

// Tortoise Beacon message types.
const (
	TimelyMessage MessageType = iota
	DelayedMessage
	LateMessage
)

func (m MessageType) String() string {
	switch m {
	case TimelyMessage:
		return "timely"
	case DelayedMessage:
		return "delayed"
	case LateMessage:
		return "late"
	default:
		return "unknown"
	}
}

type message interface {
	Epoch() types.EpochID
	String() string
}

// ProposalMessage is a message type which is used when sending proposals.
type ProposalMessage struct {
	EpochID      types.EpochID `json:"epoch_id"`
	ProposalList ATXIDList     `json:"proposal_list"`
}

// NewProposalMessage returns a new ProposalMessage.
func NewProposalMessage(epoch types.EpochID, atxList ATXIDList) *ProposalMessage {
	// TODO(nkryuchkov): Sign message.
	return &ProposalMessage{
		EpochID:      epoch,
		ProposalList: atxList,
	}
}

// Epoch returns epoch.
func (p ProposalMessage) Epoch() types.EpochID {
	return p.EpochID
}

// Proposals returns proposals.
func (p ProposalMessage) Proposals() ATXIDList {
	return p.ProposalList
}

// String returns a string form of ProposalMessage.
func (p ProposalMessage) String() string {
	bytes, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}

	return string(bytes)
}

// VotingMessage is a message type which is used when sending votes.
type VotingMessage struct {
	EpochID      types.EpochID  `json:"epoch_id"`
	RoundID      types.RoundID  `json:"round_id"`
	VotesFor     []types.Hash32 `json:"votes_for"`
	VotesAgainst []types.Hash32 `json:"votes_against"`
}

// NewVotingMessage returns a new VotingMessage.
func NewVotingMessage(epoch types.EpochID, round types.RoundID, votesFor, votesAgainst []types.Hash32) *VotingMessage {
	// TODO(nkryuchkov): Sign message.
	return &VotingMessage{
		EpochID:      epoch,
		RoundID:      round,
		VotesFor:     votesFor,
		VotesAgainst: votesAgainst,
	}
}

// Epoch returns epoch.
func (v VotingMessage) Epoch() types.EpochID {
	return v.EpochID
}

// Round returns round.
func (v VotingMessage) Round() types.RoundID {
	return v.RoundID
}

// For returns a list of ATX hashes which are votes for.
func (v VotingMessage) For() []types.Hash32 {
	return v.VotesFor
}

// Against returns a list of ATX hashes which are votes against.
func (v VotingMessage) Against() []types.Hash32 {
	return v.VotesAgainst
}

// String returns a string form of VotingMessage.
func (v VotingMessage) String() string {
	bytes, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}

	return string(bytes)
}
