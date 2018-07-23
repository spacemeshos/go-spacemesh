package net

type SessionMock struct {

	decResult []byte
	decError error
	encResult []byte
	encError error

	pubkey []byte
	keyM []byte
}

func (sm *SessionMock) ID() []byte {
	return []byte("SessionMock")
}

func (sm *SessionMock) PubKey() []byte {
	return sm.pubkey
}

func (sm *SessionMock) KeyM() []byte {
	return sm.keyM
}

func (sm *SessionMock) Encrypt(in []byte) ([]byte, error) {
	return sm.encResult, sm.encError
}

func (sm *SessionMock) Decrypt(in []byte) ([]byte, error) {
	return sm.decResult, sm.decError
}

func (sm *SessionMock) SetPubKey(x []byte) {
	sm.pubkey = x
}

func (sm *SessionMock) SetKeyM(x []byte) {
	sm.keyM = x
}

func (sm *SessionMock) SetEncrypt(res []byte, err error) {
	sm.encResult = res
	sm.encError = err
}

func (sm *SessionMock) SetDecrypt(res []byte, err error) {
	sm.decResult = res
	sm.decError = err
}
