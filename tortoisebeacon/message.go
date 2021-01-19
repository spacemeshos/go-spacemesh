package tortoisebeacon

import "github.com/spacemeshos/go-spacemesh/common/types"

type Kind int

const (
	InitialMessage Kind = iota
	VotingMessage
)

type Message interface {
	Kind() Kind
	Epoch() types.EpochID
	Round() int
	Payload() []types.ATXID
}

// TODO: have one message type
func NewInitialMessage(epoch types.EpochID, atxList []types.ATXID) Message {
	return message{
		kind:    InitialMessage,
		epoch:   epoch,
		round:   0,
		atxList: atxList,
	}
}

func NewVotingMessage(epoch types.EpochID, round int, atxList []types.ATXID) Message {
	return message{
		kind:    VotingMessage,
		epoch:   epoch,
		round:   round,
		atxList: atxList,
	}
}

type message struct {
	kind    Kind
	epoch   types.EpochID
	round   int
	atxList []types.ATXID
}

func (m message) Kind() Kind {
	return m.kind
}

func (m message) Epoch() types.EpochID {
	return m.epoch
}

func (m message) Round() int {
	return m.round
}
func (m message) Payload() []types.ATXID {
	return m.atxList
}
