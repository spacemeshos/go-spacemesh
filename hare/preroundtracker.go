package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/hare/pb"
	"github.com/spacemeshos/go-spacemesh/log"
)

type PreRoundTracker struct {
	preRound  map[string]*pb.HareMessage // maps PubKey->Pre-Round msg (unique)
	tracker   *RefCountTracker           // keeps track of seen blocks
	threshold uint32                     // the threshold to prove a single block
}

func NewPreRoundTracker(threshold uint32, expectedSize int) PreRoundTracker {
	pre := PreRoundTracker{}
	pre.preRound = make(map[string]*pb.HareMessage, expectedSize)
	pre.tracker = NewRefCountTracker(expectedSize)
	pre.threshold = threshold

	return pre
}

func (pre *PreRoundTracker) OnPreRound(msg *pb.HareMessage) {
	pub, err := crypto.NewPublicKey(msg.PubKey)
	if err != nil {
		log.Warning("Could not construct public key: ", err.Error())
		return
	}

	// only handle first pre-round msg
	if _, exist := pre.preRound[pub.String()]; exist {
		return
	}

	// record blocks from msg
	s := NewSet(msg.Message.Blocks)
	for _, v := range s.blocks {
		pre.tracker.Track(v)
	}

	pre.preRound[pub.String()] = msg
}

func (pre *PreRoundTracker) CanProveBlock(blockId BlockId) bool {
	// at least threshold occurrences of a given block
	return pre.tracker.CountStatus(blockId) >= pre.threshold
}

func (pre *PreRoundTracker) CanProveSet(set *Set) bool {
	// a set is provable iff all its blocks are provable
	for _, bid := range set.blocks {
		if !pre.CanProveBlock(bid) {
			return false
		}
	}

	return true
}
