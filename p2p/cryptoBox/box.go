package cryptoBox

import (
	crypto_rand "crypto/rand"
	"errors"
	"fmt"
	"github.com/btcsuite/btcutil/base58"
	"github.com/spacemeshos/go-spacemesh/log"
	"golang.org/x/crypto/nacl/box"
	"io"
)

const (
	keySize = 32 // non-configurable, expected by NaCl
	nonceSize = 24 // non-configurable, expected by NaCl
)

type Key interface {
	Bytes() []byte
	Raw() *[keySize]byte
	String() string
	Pretty() string
}

type PrivateKey interface {
	Key
}

type PublicKey interface {
	Key
}

type SharedSecret interface {
	Key
	Seal(message []byte) (out []byte)
	Open(encryptedMessage []byte) (out []byte, err error)
}

type key struct {
	bytes *[keySize]byte
}

var _ PrivateKey = &key{}
var _ PublicKey = &key{}
var _ SharedSecret = &key{}

func (k *key) Raw() *[keySize]byte {
	return k.bytes
}

func (k *key) Bytes() []byte {
	if k == nil {
		return []byte{}
	}
	return k.bytes[:]
}

func (k *key) String() string {
	if k == nil {
		return ""
	}
	return base58.Encode(k.bytes[:])
}

func (k *key) Pretty() string {
	pstr := k.String()
	maxRunes := 6
	if len(pstr) < maxRunes {
		maxRunes = len(pstr)
	}
	return fmt.Sprintf("<Key %s>", pstr[:maxRunes])
}

func getRandomNonce() *[nonceSize]byte {
	nonce := &[nonceSize]byte{}
	if _, err := io.ReadFull(crypto_rand.Reader, nonce[:]); err != nil {
		panic(err)
	}
	return nonce
}

func (k *key) Seal(message []byte) (out []byte) {
	nonce := getRandomNonce() // TODO: @noam replace with counter to prevent replays
	return box.SealAfterPrecomputation(nonce[:], message, nonce, k.bytes)
}

func (k *key) Open(encryptedMessage []byte) (out []byte, err error) {
	nonce := &[nonceSize]byte{}
	copy(nonce[:], encryptedMessage[:nonceSize])
	message, ok := box.OpenAfterPrecomputation(nil, encryptedMessage[nonceSize:], nonce, k.bytes)
	if !ok {
		return nil, errors.New("opening boxed message failed")
	}
	return message, nil
}

func GenerateKeyPair() (PrivateKey, PublicKey, error) {
	public, private, err := box.GenerateKey(crypto_rand.Reader)
	if err != nil {
		log.Error("failed to generate key pair\n")
		return nil, nil, err
	}

	return &key{private}, &key{public}, nil
}

func GenerateSharedSecret(privkey PrivateKey, peerPubkey PublicKey) SharedSecret {
	sharedSecret := newKey()
	box.Precompute(sharedSecret.bytes, peerPubkey.Raw(), privkey.Raw())
	return sharedSecret
}

func PrependPubkey(message []byte, pubkey PublicKey) []byte {
	return append(pubkey.Bytes(), message...)
}

func ExtractPubkey(message []byte) ([]byte, PublicKey, error) {
	if mSize := len(message); mSize <= keySize {
		return nil, nil, fmt.Errorf("cannot extract pubkey of size %d from message of size %d", keySize, mSize)
	}
	pubkey, err := NewPubkeyFromBytes(message[:keySize])
	if err != nil {
		panic(err) // this should never happen as we control the key size
	}
	return message[keySize:], pubkey, nil
}

func newKey() *key {
	return &key{&[keySize]byte{}}
}

func newKeyFromBytes(bytes []byte) (*key, error) {
	if l := len(bytes); l != keySize {
		return nil, fmt.Errorf("invalid key size (got %v instead of %v bytes)", l, keySize)
	}
	k := newKey()
	copy(k.bytes[:], bytes)
	return k, nil
}

func NewPubkeyFromBytes(bytes []byte) (PublicKey, error) {
	return newKeyFromBytes(bytes)
}

func newKeyFromBase58(s string) (*key, error) {
	bytes := base58.Decode(s)
	if len(bytes) == 0 {
		return nil, errors.New("unable to decode key")
	}
	return newKeyFromBytes(bytes)
}

func NewPrivateKeyFromBase58(s string) (PrivateKey, error) {
	return newKeyFromBase58(s)
}

func NewPublicKeyFromBase58(s string) (PublicKey, error) {
	return newKeyFromBase58(s)
}
