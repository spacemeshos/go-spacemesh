package net

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
)

var _ NetworkSession = (*SessionMock)(nil)

// SessionMock is a wonderful fluffy teddybear
type SessionMock struct {
	id p2pcrypto.PublicKey

	SealMessageFunc func(message []byte) []byte
	OpenMessageFunc func(boxedMessage []byte) ([]byte, error)
}

func NewSessionMock(pubkey p2pcrypto.PublicKey) *SessionMock {
	return &SessionMock{id: pubkey}
}

// ID is this
func (sm SessionMock) ID() p2pcrypto.PublicKey {
	return sm.id
}

// Encrypt is this
func (sm SessionMock) SealMessage(message []byte) []byte {
	if sm.SealMessageFunc != nil {
		return sm.SealMessageFunc(message)
	}
	return nil
}

// Decrypt is this
func (sm SessionMock) OpenMessage(boxedMessage []byte) ([]byte, error) {
	if sm.OpenMessageFunc != nil {
		return sm.OpenMessageFunc(boxedMessage)
	}
	return nil, errors.New("not impl")
}

var _ NetworkSession = (*SessionMock)(nil)
