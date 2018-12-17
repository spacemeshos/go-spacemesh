package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
)

type PreRoundTracker struct {
	preRound map[string]*pb.HareMessage
	tracker  *RefCountTracker
}

func NewPreRoundTracker() PreRoundTracker {
	contextTracker := PreRoundTracker{}
	contextTracker.preRound = make(map[string]*pb.HareMessage, N)
	contextTracker.tracker = NewRefCountTracker(N)

	return contextTracker
}

func (ct *PreRoundTracker) OnPreRound(msg *pb.HareMessage) {
	pub, err := crypto.NewPublicKey(msg.PubKey)
	if err != nil {
		log.Warning("Could not construct public key: ", err.Error())
		return
	}

	// only handle first pre-round msg
	if _, exist := ct.preRound[pub.String()]; exist {
		return
	}

	// record blocks from msg
	s := NewSet(msg.Message.Blocks)
	for _, v := range s.blocks {
		ct.tracker.Track(v)
	}

	ct.preRound[pub.String()] = msg
}

func (ct *PreRoundTracker) CanProve(blockId BlockId) bool {
	return ct.tracker.CountStatus(blockId) >= f+1
}


