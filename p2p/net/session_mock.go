package net

import (
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
)

// SessionMock is a wonderful fluffy teddybear
type SessionMock struct {
	id        p2pcrypto.PublicKey
	decResult []byte
	decError  error
	encResult []byte
	encError  error

	pubkey p2pcrypto.PublicKey
	keyM   []byte
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
	out := message
	if sm.encResult != nil {
		out = sm.encResult
	}
	return out
}

// Decrypt is this
func (sm SessionMock) OpenMessage(boxedMessage []byte) ([]byte, error) {
	out := boxedMessage
	if sm.decResult != nil {
		out = sm.decResult
	}
	return out, sm.decError
}

// SetDecrypt is this
func (sm *SessionMock) SetDecrypt(res []byte, err error) {
	sm.decResult = res
	sm.decError = err
}

var _ NetworkSession = (*SessionMock)(nil)
