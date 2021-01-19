package tortoisebeacon

import "github.com/spacemeshos/go-spacemesh/common/types"

type Message interface {
	Epoch() types.EpochID
	Round() int
	Payload() []types.ATXID
}

func NewMessage(epoch types.EpochID, round int, atxList []types.ATXID) Message {
	return message{
		epoch:   epoch,
		round:   round,
		atxList: atxList,
	}
}

type message struct {
	epoch   types.EpochID
	round   int
	atxList []types.ATXID
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
