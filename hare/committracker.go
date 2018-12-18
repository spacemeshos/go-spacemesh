package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
)

type CommitTracker struct {
	exist   map[string]bool
	commits map[uint32][]*pb.HareMessage
	maxSet *Set
}

func NewCommitTracker() CommitTracker {
	r3 := CommitTracker{}
	r3.exist = make(map[string]bool, f+1)
	r3.commits = make(map[uint32][]*pb.HareMessage, N)
	r3.maxSet = nil

	return r3
}

func (ct *CommitTracker) OnCommit(msg *pb.HareMessage) {
	pub, err := crypto.NewPublicKey(msg.PubKey)
	if err != nil {
		log.Warning("Could not construct public key: ", err.Error())
		return
	}

	if ct.exist[pub.String()] {
		return
	}

	ct.exist[pub.String()] = true

	s := NewSet(msg.Message.Blocks)

	// create array if necessary
	_, exist := ct.commits[s.Id()]
	if !exist {
		ct.commits[s.Id()] = make([]*pb.HareMessage, 0, f+1)
	}

	arr, _ := ct.commits[s.Id()]
	if len(arr) == f+1 { // done, we already have f+1 commits
		return
	}

	// add msg
	arr = append(arr, msg)

	if len(ct.commits[s.Id()]) > len(ct.commits[ct.maxSet.Id()]) {
		ct.maxSet = s
	}
}

func (ct *CommitTracker) HasEnoughCommits() bool {
	if ct.maxSet == nil {
		return false
	}

	return len(ct.commits[ct.maxSet.Id()]) >= f+1
}

func (ct *CommitTracker) BuildCertificate() *pb.Certificate {
	c := &pb.Certificate{}

	c.AggMsgs.Messages = ct.commits[ct.maxSet.Id()]
	c.Blocks = ct.commits[ct.maxSet.Id()][0].Message.Blocks
	// TODO: set c.AggMsgs.AggSig
	// TODO: optimize msg size by setting blocks to nil

	return c
}

