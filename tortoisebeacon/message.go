package tortoisebeacon

import (
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

type ProposalMessage interface {
	Message
	Proposals() []types.ATXID
	Hash() types.Hash32
}

type proposal struct {
	epoch     types.EpochID
	proposals []types.ATXID
}

func NewProposalMessage(epoch types.EpochID, atxList []types.ATXID) ProposalMessage {
	return &proposal{
		epoch:     epoch,
		proposals: atxList,
	}
}

func (p proposal) Epoch() types.EpochID {
	return p.epoch
}

func (p proposal) Proposals() []types.ATXID {
	return p.proposals
}

func (p proposal) Hash() types.Hash32 {
	return hashATXList(p.proposals)
}

type VotingMessage interface {
	Message
	Round() int
	VotesFor() []types.Hash32
	VotesAgainst() []types.Hash32
}

type vote struct {
	epoch                types.EpochID
	round                int
	atxListHashesFor     []types.Hash32
	atxListHashesAgainst []types.Hash32
}

func NewVotingMessage(epoch types.EpochID, round int, atxListHashesFor, atxListHashesAgainst []types.Hash32) VotingMessage {
	return &vote{
		epoch:                epoch,
		round:                round,
		atxListHashesFor:     atxListHashesFor,
		atxListHashesAgainst: atxListHashesAgainst,
	}
}

func (v vote) Epoch() types.EpochID {
	return v.epoch
}

func (v vote) Round() int {
	return v.round
}

func (v vote) VotesFor() []types.Hash32 {
	return v.atxListHashesFor
}
func (v vote) VotesAgainst() []types.Hash32 {
	return v.atxListHashesAgainst
}
