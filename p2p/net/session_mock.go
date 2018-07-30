package net

// SessionMock is a wonderful fluffy teddybear
type SessionMock struct {
	decResult []byte
	decError  error
	encResult []byte
	encError  error

	pubkey []byte
	keyM   []byte
}

// ID is this
func (sm *SessionMock) ID() []byte {
	return []byte("SessionMock")
}

// PubKey is this
func (sm *SessionMock) PubKey() []byte {
	return sm.pubkey
}

// KeyM is this
func (sm *SessionMock) KeyM() []byte {
	return sm.keyM
}

// Encrypt is this
func (sm *SessionMock) Encrypt(in []byte) ([]byte, error) {
	return sm.encResult, sm.encError
}

// Decrypt is this
func (sm *SessionMock) Decrypt(in []byte) ([]byte, error) {
	return sm.decResult, sm.decError
}

// SetPubKey is this
func (sm *SessionMock) SetPubKey(x []byte) {
	sm.pubkey = x
}

// SetKeyM is this
func (sm *SessionMock) SetKeyM(x []byte) {
	sm.keyM = x
}

// SetEncrypt is this
func (sm *SessionMock) SetEncrypt(res []byte, err error) {
	sm.encResult = res
	sm.encError = err
}

// SetDecrypt is this
func (sm *SessionMock) SetDecrypt(res []byte, err error) {
	sm.decResult = res
	sm.decError = err
}
