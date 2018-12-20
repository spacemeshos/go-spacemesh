package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
)

type CommitTracker struct {
	seenSenders map[string]bool              // tracks seen senders
	commits     map[uint32][]*pb.HareMessage // tracks Set->Commits
	maxSet      *Set                         // follows the set who has max number of commits
	threshold   int                          // the number of required commits
}

func NewCommitTracker(threshold int, expectedSize int) CommitTracker {
	ct := CommitTracker{}
	ct.seenSenders = make(map[string]bool, expectedSize)
	ct.commits = make(map[uint32][]*pb.HareMessage, expectedSize)
	ct.maxSet = nil
	ct.threshold = threshold

	return ct
}

func (ct *CommitTracker) getMaxCommits() int {
	if ct.maxSet == nil { // no max yet
		return 0
	}

	arr, exist := ct.commits[ct.maxSet.Id()]
	if !exist || arr == nil { // should seenSenders and non-nil
		panic("should be unreachable")
	}

	return len(arr)
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

	if ct.seenSenders[pub.String()] {
		return
	}

	ct.seenSenders[pub.String()] = true

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

	if len(arr) >= ct.getMaxCommits() { // update max
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
