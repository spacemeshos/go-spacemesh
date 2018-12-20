package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
)

type CommitTracker struct {
	exist     map[string]bool
	commits   map[uint32][]*pb.HareMessage
	maxSet    *Set
	threshold int
}

func NewCommitTracker(threshold int) CommitTracker {
	ct := CommitTracker{}
	ct.exist = make(map[string]bool, threshold)
	ct.commits = make(map[uint32][]*pb.HareMessage, N)
	ct.maxSet = nil
	ct.threshold = threshold

	return ct
}

func (ct *CommitTracker) getMaxCommits() int {
	if ct.maxSet == nil {
		return 0
	}

	val, exist := ct.commits[ct.maxSet.Id()]
	if !exist || val == nil {
		panic("should be unreachable")
	}

	return len(val)
}

func (ct *CommitTracker) OnCommit(msg *pb.HareMessage) {
	if ct.HasEnoughCommits() {
		return
	}

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
		ct.commits[s.Id()] = make([]*pb.HareMessage, 0, ct.threshold)
	}

	// add msg
	arr, _ := ct.commits[s.Id()]
	arr = append(arr, msg)
	ct.commits[s.Id()] = arr

	if len(arr) >= ct.getMaxCommits() {
		ct.maxSet = s
	}
}

func (ct *CommitTracker) HasEnoughCommits() bool {
	if ct.maxSet == nil {
		return false
	}

	return ct.getMaxCommits() >= ct.threshold
}

func (ct *CommitTracker) BuildCertificate() *pb.Certificate {
	c := &pb.Certificate{}

	c.AggMsgs.Messages = ct.commits[ct.maxSet.Id()]
	c.Blocks = ct.commits[ct.maxSet.Id()][0].Message.Blocks
	// TODO: set c.AggMsgs.AggSig
	// TODO: optimize msg size by setting blocks to nil

	return c
}
