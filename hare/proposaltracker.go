package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
)

type Round2Tracker struct {
	proposals map[string]*pb.HareMessage
	isConflicting bool
}

func NewRound2Tracker() Round2Tracker {
	r2 := Round2Tracker{}
	r2.proposals = make(map[string]*pb.HareMessage, N)
	r2.isConflicting = false

	return r2
}

func (r2 *Round2Tracker) OnProposal(msg *pb.HareMessage) {
	pub, err := crypto.NewPublicKey(msg.PubKey)
	if err != nil {
		log.Warning("Could not construct public key: ", err.Error())
		return
	}

	s := NewSet(msg.Message.Blocks)

	p, exist := r2.proposals[pub.String()]
	if !exist { // should record
		r2.proposals[pub.String()] = msg
		return
	}

	// same pub key, verify same set
	g := NewSet(p.Message.Blocks)
	if !s.Equals(g) {
		r2.isConflicting = true
	}
}

func (r2 *Round2Tracker) HasValidProposal() bool {
	return r2.isConflicting
}

func (r2 *Round2Tracker) ProposedSet() Set {
	return Set{}
}