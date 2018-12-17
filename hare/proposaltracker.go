package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
)

type ProposalTracker struct {
	proposals map[string]*pb.HareMessage
	isConflicting bool
}

func NewProposalTracker() ProposalTracker {
	r2 := ProposalTracker{}
	r2.proposals = make(map[string]*pb.HareMessage, N)
	r2.isConflicting = false

	return r2
}

func (pt *ProposalTracker) OnProposal(msg *pb.HareMessage) {
	pub, err := crypto.NewPublicKey(msg.PubKey)
	if err != nil {
		log.Warning("Could not construct public key: ", err.Error())
		return
	}

	s := NewSet(msg.Message.Blocks)

	p, exist := pt.proposals[pub.String()]
	if !exist { // should record
		pt.proposals[pub.String()] = msg
		return
	}

	// same pub key, verify same set
	g := NewSet(p.Message.Blocks)
	if !s.Equals(g) {
		pt.isConflicting = true
	}
}

func (pt *ProposalTracker) HasValidProposal() bool {
	return pt.isConflicting
}

func (pt *ProposalTracker) ProposedSet() Set {
	return Set{}
}