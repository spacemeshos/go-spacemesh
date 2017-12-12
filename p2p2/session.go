package p2p2

import "time"

// Session auth between 2 peers
// Sessions may be used between 'connections' until they expire
type NetworkSession interface {
	Iv() []byte // session iv
	Key() []byte // session shared sym key
	Created() time.Time // time when session was established

	// TODO: add expiration support

	IsAuthenticated() bool
	SetAuthenticated(val bool)
}

type NetworkSessionImpl struct {
	iv []byte
	key []byte
	created time.Time
	authenticated bool

	// todo: this type might include a decryptor and an encryptor for fast enc/dec of data to/from a remote node
	// when we have an active session - it might be expensive to create these for each outgoing / incoming message
	// there should only be 1 session per remote node
}

func (n* NetworkSessionImpl) Iv() []byte {
	return n.iv
}

func (n* NetworkSessionImpl) Key() []byte {
	return n.Key()
}

func (n* NetworkSessionImpl) IsAuthenticated() bool {
	return n.authenticated
}

func (n* NetworkSessionImpl) SetAuthenticated(val bool) {
	n.authenticated = val
}

func (n* NetworkSessionImpl) Created() time.Time {
	return n.created
}

func NewNetworkSession(iv []byte, key []byte) NetworkSession {
	s := &NetworkSessionImpl{
		iv: iv,
		key: key,
		created: time.Now(),
		authenticated: false,
	}

	// todo: create dec/enc here
	return s
}