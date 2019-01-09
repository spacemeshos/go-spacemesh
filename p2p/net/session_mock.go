package net

import (
	"github.com/spacemeshos/go-spacemesh/p2p/cryptoBox"
)

// SessionMock is a wonderful fluffy teddybear
type SessionMock struct {
	id        cryptoBox.PublicKey
	decResult []byte
	decError  error
	encResult []byte
	encError  error

	pubkey cryptoBox.PublicKey
	keyM   []byte
}

func NewSessionMockWithPubkey(pubkey cryptoBox.PublicKey) *SessionMock {
	return &SessionMock{id: pubkey}
}

func NewSessionMock(ID []byte) *SessionMock {
	publicKey, err := cryptoBox.NewPubkeyFromBytes(ID)
	if err != nil {
		panic(err)
	}
	return &SessionMock{id: publicKey}
}

// ID is this
func (sm SessionMock) ID() cryptoBox.PublicKey {
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
