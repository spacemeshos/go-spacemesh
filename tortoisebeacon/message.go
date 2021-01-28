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
	Hash() types.Hash32
}

type vote struct {
	epoch       types.EpochID
	round       int
	atxListHash types.Hash32
}

// TODO(nkryuchkov): remove this type
func NewVotingMessage(epoch types.EpochID, round int, atxListHash types.Hash32) VotingMessage {
	return &vote{
		epoch:       epoch,
		round:       round,
		atxListHash: atxListHash,
	}
}

func (v vote) Epoch() types.EpochID {
	return v.epoch
}

func (v vote) Round() int {
	return v.round
}

func (v vote) Hash() types.Hash32 {
	return v.atxListHash
}

type BatchVotingMessage interface {
	Message
	Round() int
	HashList() []types.Hash32
}

type batchVote struct {
	epoch         types.EpochID
	round         int
	atxListHashes []types.Hash32
}

// TODO(nkryuchkov): rename to NewVotingMessage
func NewBatchVotingMessage(epoch types.EpochID, round int, atxListHashes []types.Hash32) BatchVotingMessage {
	return &batchVote{
		epoch:         epoch,
		round:         round,
		atxListHashes: atxListHashes,
	}
}

func (v batchVote) Epoch() types.EpochID {
	return v.epoch
}

func (v batchVote) Round() int {
	return v.round
}

func (v batchVote) HashList() []types.Hash32 {
	return v.atxListHashes
}
