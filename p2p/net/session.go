package net

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
)

// NetworkSession is an authenticated network session between 2 peers.
// Sessions may be used between 'connections' until they expire.
// Session provides the encryptor/decryptor for all messages exchanged between 2 peers.
// enc/dec is using an ephemeral sym key exchanged securely between the peers via the handshake protocol
// The handshake protocol goal is to create an authenticated network session.
type NetworkSession interface {
	ID() p2pcrypto.PublicKey // Unique session id, currently the peer pubkey TODO: @noam use pubkey from conn and remove this

	OpenMessage(boxedMessage []byte) ([]byte, error) // decrypt data using session dec key
	SealMessage(message []byte) []byte               // encrypt data using session enc key
}

var _ NetworkSession = (*networkSessionImpl)(nil)

// TODO: add support for idle session expiration

// networkSessionImpl implements NetworkSession.
type networkSessionImpl struct {
	sharedSecret p2pcrypto.SharedSecret
	peerPubkey   p2pcrypto.PublicKey
}

// String returns the session's identifier string.
func (n networkSessionImpl) String() string {
	return n.peerPubkey.String()
}

// ID returns the session's unique id
func (n networkSessionImpl) ID() p2pcrypto.PublicKey {
	return n.peerPubkey
}

// SealMessage encrypts in binary data with the session's sym enc key.
func (n networkSessionImpl) SealMessage(message []byte) []byte {
	if n.sharedSecret == nil {
		log.Panic("tried to seal a message before initializing session with a shared secret")
	}
	return n.sharedSecret.Seal(message)
}

// OpenMessage decrypts in binary data that was encrypted with the session's sym enc key.
func (n networkSessionImpl) OpenMessage(boxedMessage []byte) (message []byte, err error) {
	if n.sharedSecret == nil {
		log.Panic("tried to open a message before initializing session with a shared secret")
	}
	return n.sharedSecret.Open(boxedMessage)
}

// NewNetworkSession creates a new network session based on provided data
func NewNetworkSession(sharedSecret p2pcrypto.SharedSecret, peerPubkey p2pcrypto.PublicKey) NetworkSession {
	return &networkSessionImpl{
		sharedSecret: sharedSecret,
		peerPubkey:   peerPubkey,
	}
}
