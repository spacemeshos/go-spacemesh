package hare

import "github.com/spacemeshos/go-spacemesh/signing"

type msgsTracker struct {
	sigToPub map[string]*signing.PublicKey
}

func (mt *msgsTracker) Track(m *Msg) {
	mt.sigToPub[string(m.Sig)] = m.PubKey
}

func (mt *msgsTracker) PublicKey(m *Message) *signing.PublicKey {
	return mt.sigToPub[string(m.Sig)]
}

func newMsgsTracker() *msgsTracker {
	return &msgsTracker{sigToPub: make(map[string]*signing.PublicKey)}
}
