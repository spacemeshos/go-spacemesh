package net

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
)

var _ NetworkSession = (*SessionMock)(nil)

// SessionMock mocks NetworkSession.
type SessionMock struct {
	id p2pcrypto.PublicKey

	SealMessageFunc func(message []byte) []byte
	OpenMessageFunc func(boxedMessage []byte) ([]byte, error)
}

// NewSessionMock creates a new mock for given public key.
func NewSessionMock(pubkey p2pcrypto.PublicKey) *SessionMock {
	return &SessionMock{id: pubkey}
}

// ID is a mock.
func (sm SessionMock) ID() p2pcrypto.PublicKey {
	return sm.id
}

// SealMessage is a mock.
func (sm SessionMock) SealMessage(message []byte) []byte {
	if sm.SealMessageFunc != nil {
		return sm.SealMessageFunc(message)
	}
	return nil
}

// OpenMessage is a mock.
func (sm SessionMock) OpenMessage(boxedMessage []byte) ([]byte, error) {
	if sm.OpenMessageFunc != nil {
		return sm.OpenMessageFunc(boxedMessage)
	}
	return nil, errors.New("not impl")
}

var _ NetworkSession = (*SessionMock)(nil)
