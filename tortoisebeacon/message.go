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
	Payload() []byte
}

func NewInitialMessage(epoch types.EpochID) Message {
	return message{
		kind:    InitialMessage,
		epoch:   epoch,
		round:   0,
		payload: nil,
	}
}

func NewVotingMessage(epoch types.EpochID, round int) Message {
	return message{
		kind:    VotingMessage,
		epoch:   epoch,
		round:   round,
		payload: nil,
	}
}

type message struct {
	kind    Kind
	epoch   types.EpochID
	round   int
	payload []byte
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
func (m message) Payload() []byte {
	return m.payload
}
