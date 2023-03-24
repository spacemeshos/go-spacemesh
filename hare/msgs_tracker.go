package hare

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

type msgsTracker struct {
	sigToPub map[types.EdSignature]*signing.PublicKey
}

func (mt *msgsTracker) Track(m *Msg) {
	mt.sigToPub[m.Signature] = m.PubKey
}

func (mt *msgsTracker) PublicKey(m *Message) *signing.PublicKey {
	return mt.sigToPub[m.Signature]
}

func newMsgsTracker() *msgsTracker {
	return &msgsTracker{sigToPub: make(map[types.EdSignature]*signing.PublicKey)}
}
