package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
)

type Round3Tracker struct {
	exist   map[string]bool
	commits map[uint32][]*pb.HareMessage
	maxSet *Set
}

func NewRound3Tracker() Round3Tracker {
	r3 := Round3Tracker{}
	r3.exist = make(map[string]bool, f+1)
	r3.commits = make(map[uint32][]*pb.HareMessage, N)
	r3.maxSet = nil

	return r3
}

func (r3 *Round3Tracker) OnCommit(msg *pb.HareMessage) {
	pub, err := crypto.NewPublicKey(msg.PubKey)
	if err != nil {
		log.Warning("Could not construct public key: ", err.Error())
		return
	}

	if r3.exist[pub.String()] {
		return
	}

	r3.exist[pub.String()] = true

	s := NewSet(msg.Message.Blocks)

	// add to msgs array (create if necessary)
	arr, exist := r3.commits[s.Id()]
	if !exist {
		r3.commits[s.Id()] = make([]*pb.HareMessage, 0, f+1)
		r3.commits[s.Id()] = append(r3.commits[s.Id()], msg)
	} else {
		arr = append(arr, msg)
	}

	if len(r3.commits[s.Id()]) > len(r3.commits[r3.maxSet.Id()]) {
		r3.maxSet = s
	}
}

func (r3 *Round3Tracker) HasEnoughCommits() bool {
	if r3.maxSet == nil {
		return false
	}

	return len(r3.commits[r3.maxSet.Id()]) >= f+1
}

func (r3 *Round3Tracker) BuildCertificate() *pb.Certificate {
	c := &pb.Certificate{}

	return c
}

