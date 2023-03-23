package hare

import "github.com/spacemeshos/go-spacemesh/signing"

type msgsTracker struct {
	sigToPub map[[64]byte]*signing.PublicKey
}

func (mt *msgsTracker) Track(m *Msg) {
	mt.sigToPub[m.Signature] = m.PubKey
}

func (mt *msgsTracker) PublicKey(m *Message) *signing.PublicKey {
	return mt.sigToPub[m.Signature]
}

func newMsgsTracker() *msgsTracker {
	return &msgsTracker{sigToPub: make(map[[64]byte]*signing.PublicKey)}
}
