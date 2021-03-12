// Package p2pcrypto defines the cryptographic primitives used to communicate and identify in the p2p network,
// it uses go stdlib's NaCL box implementation.
package p2pcrypto

import (
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/btcsuite/btcutil/base58"
	"github.com/spacemeshos/go-spacemesh/log"
	"golang.org/x/crypto/nacl/box"
	"io"
)

const (
	keySize   = 32 // non-configurable, expected by NaCl
	nonceSize = 24 // non-configurable, expected by NaCl
)

// Key represents a 32 bit cryptographic key, public or private.
type Key interface {
	Bytes() []byte
	raw() *[keySize]byte
	Array() [32]byte
	String() string
	Field() log.Field
}

// PrivateKey is a private key of 32 byte.
type PrivateKey interface {
	Key
}

// PublicKey is a public key of 32 byte.
type PublicKey interface {
	Key
}

// SharedSecret is created using two participants key to enable Sealing and Opening of messages (NaCL).
type SharedSecret interface {
	Key
	Seal(message []byte) (out []byte)
	Open(encryptedMessage []byte) (out []byte, err error)
}

type key struct {
	bytes [keySize]byte
}

var _ PrivateKey = (*key)(nil)
var _ PublicKey = (*key)(nil)
var _ SharedSecret = (*key)(nil)

func (k key) raw() *[keySize]byte {
	return &k.bytes
}

// Array returns a fixed size array representation of the key.
func (k key) Array() [32]byte {
	return k.bytes
}

// Bytes returns the key represented in bytes.
func (k key) Bytes() []byte {
	return k.bytes[:]
}

// String returns a base58 encoded string from the key.
func (k key) String() string {
	return base58.Encode(k.Bytes())
}

// Field returns a log field. Implements the LoggableField interface.
func (k key) Field() log.Field {
	return log.String("key", k.String())
}

func getRandomNonce() [nonceSize]byte {
	nonce := [nonceSize]byte{}
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		log.Panic(fmt.Sprint(err))
	}
	return nonce
}

// Seal encrypts and signs a message.
func (k key) Seal(message []byte) (out []byte) {
	nonce := getRandomNonce() // TODO: @noam replace with counter to prevent replays
	return box.SealAfterPrecomputation(nonce[:], message, &nonce, k.raw())
}

// Open decrypts and authenticates a signed and encrypted message.
func (k key) Open(encryptedMessage []byte) (out []byte, err error) {
	if len(encryptedMessage) <= nonceSize {
		return nil, errors.New("message was too small")
	}
	nonce := &[nonceSize]byte{}
	copy(nonce[:], encryptedMessage[:nonceSize])
	message, ok := box.OpenAfterPrecomputation(nil, encryptedMessage[nonceSize:], nonce, k.raw())
	if !ok {
		return nil, errors.New("opening boxed message failed")
	}
	return message, nil
}

// GenerateKeyPair generates ed25519 private key and a public key derived from it.
func GenerateKeyPair() (PrivateKey, PublicKey, error) {
	public, private, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	return key{*private}, key{*public}, nil
}

// GenerateSharedSecret creates a key derived from a private and a public key.
func GenerateSharedSecret(privkey PrivateKey, peerPubkey PublicKey) SharedSecret {
	sharedSecret := newKey()
	box.Precompute(sharedSecret.raw(), peerPubkey.raw(), privkey.raw())
	return sharedSecret
}

// PrependPubkey adds a public key at the beginning on a byte array.
func PrependPubkey(message []byte, pubkey PublicKey) []byte {
	return append(pubkey.Bytes(), message...)
}

// ExtractPubkey extracts a public key from the beginning of a byte array.
func ExtractPubkey(message []byte) ([]byte, PublicKey, error) {
	if mSize := len(message); mSize <= keySize {
		return nil, nil, fmt.Errorf("cannot extract pubkey of size %d from message of size %d", keySize, mSize)
	}
	pubkey, err := NewPubkeyFromBytes(message[:keySize])
	if err != nil {
		log.Panic(fmt.Sprint(err)) // this should never happen as we control the key size
	}
	return message[keySize:], pubkey, nil
}

func newKey() key {
	return key{[keySize]byte{}}
}

var nilKey key

func newKeyFromBytes(bytes []byte) (key, error) {
	if l := len(bytes); l != keySize {
		return nilKey, fmt.Errorf("invalid key size (got %v instead of %v bytes)", l, keySize)
	}
	k := newKey()
	copy(k.bytes[:], bytes)
	return k, nil
}

// NewPubkeyFromBytes creates a public key from a byte array.
func NewPubkeyFromBytes(bytes []byte) (PublicKey, error) {
	return newKeyFromBytes(bytes)
}

func newKeyFromBase58(s string) (key, error) {
	bytes := base58.Decode(s)
	if len(bytes) == 0 {
		return nilKey, errors.New("unable to decode key")
	}
	return newKeyFromBytes(bytes)
}

// NewPrivateKeyFromBase58 creates a private key from a base58 string.
func NewPrivateKeyFromBase58(s string) (PrivateKey, error) {
	return newKeyFromBase58(s)
}

// NewPublicKeyFromBase58 creates a public key from a base58 string.
func NewPublicKeyFromBase58(s string) (PublicKey, error) {
	return newKeyFromBase58(s)
}

// NewRandomPubkey reads random bytes and creates a public key from them. used for testing
func NewRandomPubkey() PublicKey {
	k := newKey()
	if _, err := io.ReadFull(rand.Reader, k.bytes[:]); err != nil {
		log.Panic(fmt.Sprint(err))
	}
	return k
}

// PublicKeyFromArray creates a public key using a fixed sized byte array.
func PublicKeyFromArray(ke [32]byte) PublicKey {
	return key{ke}
}
