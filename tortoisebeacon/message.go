package tortoisebeacon

import (
	"encoding/json"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type MessageType int

const (
	TimelyMessage MessageType = iota
	DelayedMessage
	LateMessage
)

type Message interface {
	Epoch() types.EpochID
}

type ProposalMessage struct {
	EpochID      types.EpochID `json:"epoch_id"`
	ProposalList []types.ATXID `json:"proposal_list"`
}

func NewProposalMessage(epoch types.EpochID, atxList []types.ATXID) *ProposalMessage {
	return &ProposalMessage{
		EpochID:      epoch,
		ProposalList: atxList,
	}
}

func (p ProposalMessage) Epoch() types.EpochID {
	return p.EpochID
}

func (p ProposalMessage) Proposals() []types.ATXID {
	return p.ProposalList
}

func (p ProposalMessage) Hash() types.Hash32 {
	return hashATXList(p.ProposalList)
}

func (p ProposalMessage) String() string {
	bytes, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}

	return string(bytes)
}

type VotingMessage struct {
	EpochID              types.EpochID  `json:"epoch_id"`
	RoundID              uint64         `json:"round_id"`
	ATXListHashesFor     []types.Hash32 `json:"atx_list_hashes_for"`
	ATXListHashesAgainst []types.Hash32 `json:"atx_list_hashes_against"`
}

func NewVotingMessage(epoch types.EpochID, round uint64, atxListHashesFor, atxListHashesAgainst []types.Hash32) *VotingMessage {
	return &VotingMessage{
		EpochID:              epoch,
		RoundID:              round,
		ATXListHashesFor:     atxListHashesFor,
		ATXListHashesAgainst: atxListHashesAgainst,
	}
}

func (v VotingMessage) Epoch() types.EpochID {
	return v.EpochID
}

func (v VotingMessage) Round() uint64 {
	return v.RoundID
}

func (v VotingMessage) VotesFor() []types.Hash32 {
	return v.ATXListHashesFor
}

func (v VotingMessage) VotesAgainst() []types.Hash32 {
	return v.ATXListHashesAgainst
}

func (v VotingMessage) String() string {
	bytes, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}

	return string(bytes)
}
