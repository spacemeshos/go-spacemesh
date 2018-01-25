package p2p

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"errors"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"time"
)

// NetworkSession is an authenticated network session between 2 peers.
// Sessions may be used between 'connections' until they expire.
// Session provides the encryptor/decryptor for all messages exchanged between 2 peers.
// enc/dec is using an ephemeral sym key exchanged securely between the peers via the handshake protocol
// The handshake protocol goal is to create an authenticated network session.
type NetworkSession interface {
	ID() []byte         // Unique session id
	String() string     // globally unique session id for p2p debugging and key store purposes
	KeyE() []byte       // session shared sym key for enc - 32 bytes
	KeyM() []byte       // session shared sym key for mac - 32 bytes
	PubKey() []byte     // 65 bytes session-only pub key uncompressed
	Created() time.Time // time when session was established

	// TODO: add support for idle session expiration

	LocalNodeID() string  // string encoded session local node id
	RemoteNodeID() string // string encoded session remote node id

	IsAuthenticated() bool
	SetAuthenticated(val bool)

	Decrypt(in []byte) ([]byte, error) // decrypt data using session dec key
	Encrypt(in []byte) ([]byte, error) // encrypt data using session enc key
}

// NetworkSessionImpl implements NetworkSession.
type NetworkSessionImpl struct {
	id            []byte
	keyE          []byte
	keyM          []byte
	pubKey        []byte
	created       time.Time
	authenticated bool

	localNodeID  string
	remoteNodeID string

	blockEncrypter cipher.BlockMode
	blockDecrypter cipher.BlockMode
}

//LocalNodeID returns the session's local node id.
func (n *NetworkSessionImpl) LocalNodeID() string {
	return n.localNodeID
}

//RemoteNodeID returns the session's remote node id.
func (n *NetworkSessionImpl) RemoteNodeID() string {
	return n.remoteNodeID
}

// String returns the session's identifier string.
func (n *NetworkSessionImpl) String() string {
	return hex.EncodeToString(n.id)
}

// ID returns the session's unique id
func (n *NetworkSessionImpl) ID() []byte {
	return n.id
}

// KeyE returns the sessions sym encryption key.
func (n *NetworkSessionImpl) KeyE() []byte {
	return n.keyE
}


// KeyM returns the session's MAC encryption key.
func (n *NetworkSessionImpl) KeyM() []byte {
	return n.keyM
}

// PubKey returns the session's public key.
func (n *NetworkSessionImpl) PubKey() []byte {
	return n.pubKey
}

// IsAuthenticated returns true iff the session is authenticated.
func (n *NetworkSessionImpl) IsAuthenticated() bool {
	return n.authenticated
}

// SetAuthenticated updates the session's authentication state.
func (n *NetworkSessionImpl) SetAuthenticated(val bool) {
	n.authenticated = val
}

// Created returns the session creation time.
func (n *NetworkSessionImpl) Created() time.Time {
	return n.created
}

// Encrypt encrypts in binary data with the session's sym enc key.
func (n *NetworkSessionImpl) Encrypt(in []byte) ([]byte, error) {
	l := len(in)
	if l == 0 {
		return nil, errors.New("Invalid input buffer - 0 len")
	}
	paddedIn := crypto.AddPKCSPadding(in)
	out := make([]byte, len(paddedIn))
	n.blockEncrypter.CryptBlocks(out, paddedIn)
	return out, nil
}

// Decrypt decrypts in binary data that was encrypted with the session's sym enc key.
func (n *NetworkSessionImpl) Decrypt(in []byte) ([]byte, error) {
	l := len(in)
	if l == 0 {
		return nil, errors.New("Invalid input buffer - 0 len")
	}

	n.blockDecrypter.CryptBlocks(in, in)
	clearText, err := crypto.RemovePKCSPadding(in)
	if err != nil {
		return nil, err
	}

	return clearText, nil
}

// NewNetworkSession creates a new network session based on provided data
func NewNetworkSession(id, keyE, keyM, pubKey []byte, localNodeID, remoteNodeID string) (NetworkSession, error) {
	s := &NetworkSessionImpl{
		id:            id,
		keyE:          keyE,
		keyM:          keyM,
		pubKey:        pubKey,
		created:       time.Now(),
		authenticated: false,
		localNodeID:   localNodeID,
		remoteNodeID:  remoteNodeID,
	}

	// create and store block enc/dec
	blockCipher, err := aes.NewCipher(keyE)
	if err != nil {
		log.Error("Failed to create block cipher")
		return nil, err
	}

	s.blockEncrypter = cipher.NewCBCEncrypter(blockCipher, s.id)
	s.blockDecrypter = cipher.NewCBCDecrypter(blockCipher, s.id)

	return s, nil
}
