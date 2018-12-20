package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
)

type ProposalTracker struct {
	proposals     map[string]*pb.HareMessage // maps PubKey->Proposal
	isConflicting map[string]bool            // maps PubKey->ConflictStatus
}

func NewProposalTracker(expectedSize int) ProposalTracker {
	pt := ProposalTracker{}
	pt.proposals = make(map[string]*pb.HareMessage, expectedSize)
	pt.isConflicting = make(map[string]bool, expectedSize)

	return pt
}

func (pt *ProposalTracker) OnProposal(msg *pb.HareMessage) {
	pub, err := crypto.NewPublicKey(msg.PubKey)
	if err != nil {
		log.Warning("Could not construct public key: ", err.Error())
	}

	s := NewSet(msg.Message.Blocks)

	p, exist := pt.proposals[pub.String()]
	if !exist { // first msg from this pub
		pt.proposals[pub.String()] = msg
		return
	}

	// same pub key, verify same set
	g := NewSet(p.Message.Blocks)
	if !s.Equals(g) {
		pt.isConflicting[pub.String()] = true
	}
}

func (pt *ProposalTracker) getLeader() (string, bool) {
	// TODO: later it should actually get the minimum over role proofs
	for pubKey := range pt.proposals {
		return pubKey, true
	}

	return "", false
}

func (pt *ProposalTracker) IsConflicting() bool {
	leader, found := pt.getLeader()
	if !found {
		return false
	}

	return pt.isConflicting[leader]
}

func (pt *ProposalTracker) ProposedSet() (*Set, bool) {
	leader, found := pt.getLeader()
	if found {
		return nil, false
	}

	if pt.isConflicting[leader] {
		return nil, false
	}

	return NewSet(pt.proposals[leader].Message.Blocks), true
}
