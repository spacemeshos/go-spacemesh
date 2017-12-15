package swarm

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"errors"
	"github.com/UnrulyOS/go-unruly/crypto"
	"github.com/UnrulyOS/go-unruly/log"
	"time"
)

// A runtime network session auth between 2 peers
// Sessions may be used between 'connections' until they expire
// Session provides the encryptor/decryptor for all messages sent between peers
type NetworkSession interface {
	Id() []byte         // Unique session id
	String() string     // globally unique session id for p2p debugging and key store purposes
	KeyE() []byte       // session shared sym key for enc - 32 bytes
	KeyM() []byte       // session shared sym key for mac - 32 bytes
	PubKey() []byte     // 65 bytes session-only pub key uncompressed
	Created() time.Time // time when session was established

	// TODO: add session expiration support

	LocalNodeId() string  // string encoded session local node id
	RemoteNodeId() string // string encoded session remote node id

	IsAuthenticated() bool
	SetAuthenticated(val bool)

	Decrypt(in []byte) ([]byte, error) // decrypt data using session dec key
	Encrypt(in []byte) ([]byte, error) // encrypt data using session enc key
}

type NetworkSessionImpl struct {
	id            []byte
	keyE          []byte
	keyM          []byte
	pubKey        []byte
	created       time.Time
	authenticated bool

	localNodeId  string
	remoteNodeId string

	blockEncrypter cipher.BlockMode
	blockDecrypter cipher.BlockMode
}

func (n *NetworkSessionImpl) LocalNodeId() string {
	return n.localNodeId
}

func (n *NetworkSessionImpl) RemoteNodeId() string {
	return n.remoteNodeId
}

func (n *NetworkSessionImpl) String() string {
	return hex.EncodeToString(n.id)
}

func (n *NetworkSessionImpl) Id() []byte {
	return n.id
}

func (n *NetworkSessionImpl) KeyE() []byte {
	return n.keyE
}

func (n *NetworkSessionImpl) KeyM() []byte {
	return n.keyM
}

func (n *NetworkSessionImpl) PubKey() []byte {
	return n.pubKey
}

func (n *NetworkSessionImpl) IsAuthenticated() bool {
	return n.authenticated
}

func (n *NetworkSessionImpl) SetAuthenticated(val bool) {
	n.authenticated = val
}

func (n *NetworkSessionImpl) Created() time.Time {
	return n.created
}

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

func (n *NetworkSessionImpl) Decrypt(in []byte) ([]byte, error) {
	l := len(in)
	if l == 0 {
		return nil, errors.New("Invalid input buffer - 0 len")
	}
	//out := make([]byte, l)
	n.blockDecrypter.CryptBlocks(in, in)
	clearText, err := crypto.RemovePKCSPadding(in)
	if err != nil {
		return nil, err
	}

	return clearText, nil
}

func NewNetworkSession(id, keyE, keyM, pubKey []byte, localNodeId, remoteNodeId string) (NetworkSession, error) {
	s := &NetworkSessionImpl{
		id:            id,
		keyE:          keyE,
		keyM:          keyM,
		pubKey:        pubKey,
		created:       time.Now(),
		authenticated: false,
		localNodeId:   localNodeId,
		remoteNodeId:  remoteNodeId,
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
