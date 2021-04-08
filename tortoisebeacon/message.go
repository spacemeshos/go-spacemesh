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
	ProposalList []types.ATXID `json:"proposal_list"`
}

// NewProposalMessage returns a new ProposalMessage.
func NewProposalMessage(epoch types.EpochID, atxList []types.ATXID) *ProposalMessage {
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
func (p ProposalMessage) Proposals() []types.ATXID {
	return p.ProposalList
}

// Hash returns hash.
func (p ProposalMessage) Hash() types.Hash32 {
	return hashATXList(p.ProposalList)
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
	EpochID       types.EpochID  `json:"epoch_id"`
	RoundID       uint64         `json:"round_id"`
	HashesFor     []types.Hash32 `json:"hashes_for"`
	HashesAgainst []types.Hash32 `json:"hashes_against"`
}

// NewVotingMessage returns a new VotingMessage.
func NewVotingMessage(epoch types.EpochID, round uint64, hashesFor, hashesAgainst []types.Hash32) *VotingMessage {
	return &VotingMessage{
		EpochID:       epoch,
		RoundID:       round,
		HashesFor:     hashesFor,
		HashesAgainst: hashesAgainst,
	}
}

// Epoch returns epoch.
func (v VotingMessage) Epoch() types.EpochID {
	return v.EpochID
}

// Round returns round.
func (v VotingMessage) Round() uint64 {
	return v.RoundID
}

// VotesFor returns a list of ATX hashes which are votes for.
func (v VotingMessage) VotesFor() []types.Hash32 {
	return v.HashesFor
}

// VotesAgainst returns a list of ATX hashes which are votes against.
func (v VotingMessage) VotesAgainst() []types.Hash32 {
	return v.HashesAgainst
}

// String returns a string form of VotingMessage.
func (v VotingMessage) String() string {
	bytes, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}

	return string(bytes)
}
