package hare

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
)

type msgsTracker struct {
	sigToPub map[types.EdSignature]types.NodeID
}

func (mt *msgsTracker) Track(m *Message) {
	mt.sigToPub[m.Signature] = m.SmesherID
}

func (mt *msgsTracker) NodeID(m *Message) types.NodeID {
	return mt.sigToPub[m.Signature]
}

func newMsgsTracker() *msgsTracker {
	return &msgsTracker{sigToPub: make(map[types.EdSignature]types.NodeID)}
}
