package crypto

import (
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcutil/base58"
	"github.com/spacemeshos/go-spacemesh/log"
)

// Key defines basic key caps.
type Key interface {
	String() string // this is a base58 encoded of Bytes()
	Bytes() []byte  // raw key binary data
	Pretty() string // pretty print key id
}

// PrivateKey defines a private encryption key.
type PrivateKey interface {
	Key

	GetPublicKey() PublicKey // get the pub key corresponding to this priv key
	Sign([]byte) ([]byte, error)

	// Decrypt binary data encrypted with the public key of this private key
	Decrypt(in []byte) ([]byte, error)

	InternalKey() *btcec.PrivateKey
}

// PublicKey defines a public encryption key.
type PublicKey interface { // 33 bytes
	Key
	Verify(data []byte, sig []byte) (bool, error)
	VerifyString(data []byte, sig string) (bool, error)

	// encrypt data so it is only decryptable w the private key of this key
	Encrypt(in []byte) ([]byte, error)

	InternalKey() *btcec.PublicKey
}

type publicKeyImpl struct {
	k *btcec.PublicKey
}

type privateKeyImpl struct {
	k *btcec.PrivateKey
}

// GenerateKeyPair generates a private and public key pair.
func GenerateKeyPair() (PrivateKey, PublicKey, error) {
	privKey, err := btcec.NewPrivateKey(btcec.S256())
	if err != nil {
		return nil, nil, err
	}

	return &privateKeyImpl{privKey}, &publicKeyImpl{privKey.PubKey()}, nil
}

// NewPrivateKey creates a new private key from data
func NewPrivateKey(data []byte) (PrivateKey, error) {

	if len(data) != 32 {
		return nil, errors.New("expected 32 bytes input")
	}

	privk, _ := btcec.PrivKeyFromBytes(btcec.S256(), data)
	return &privateKeyImpl{privk}, nil
}

// NewPrivateKeyFromString creates a new private key a base58 encoded string.
func NewPrivateKeyFromString(s string) (PrivateKey, error) {
	data := base58.Decode(s)
	return NewPrivateKey(data)
}

// InternalKey gets the internal key associated with a private key
func (p *privateKeyImpl) InternalKey() *btcec.PrivateKey {
	return p.k
}

// Bytes returns the private key binary data
func (p *privateKeyImpl) Bytes() []byte {
	return p.k.Serialize()
}

// String returns a base58 encoded string of the private key binary data
func (p *privateKeyImpl) String() string {
	bytes := p.Bytes()
	return base58.Encode(bytes)
}

// GetPublicKey generates and returns the public key associated with a private key
func (p *privateKeyImpl) GetPublicKey() PublicKey {
	pubKey := p.k.PubKey()
	return &publicKeyImpl{k: pubKey}
}

// Pretty returns a readable string of the private key data.
func (p *privateKeyImpl) Pretty() string {
	pstr := p.String()
	maxRunes := 6
	if len(pstr) < maxRunes {
		maxRunes = len(pstr)
	}
	return fmt.Sprintf("<PrivKey %s>", pstr[:maxRunes])
}

// Sign signs binary data with the private key.
func (p *privateKeyImpl) Sign(in []byte) ([]byte, error) {
	signature, err := p.k.Sign(in)
	if err != nil {
		return nil, err
	}
	return signature.Serialize(), nil
}

// Decrypt decrypts data encrypted with a public key using its matching private key
func (p *privateKeyImpl) Decrypt(in []byte) ([]byte, error) {
	return btcec.Decrypt(p.k, in)
}

// NewPublicKey creates a new public key from provided binary key data.
func NewPublicKey(data []byte) (PublicKey, error) {
	k, err := btcec.ParsePubKey(data, btcec.S256())
	if err != nil {
		log.Error("Failed to parse public key from binay data", err)
		return nil, err
	}

	return &publicKeyImpl{k}, nil
}

// NewPublicKeyFromString creates a new public key from a base58 encoded string data.
func NewPublicKeyFromString(s string) (PublicKey, error) {
	data := base58.Decode(s)
	return NewPublicKey(data)
}

// InternalKey returns the internal public key data.
func (p *publicKeyImpl) InternalKey() *btcec.PublicKey {
	return p.k
}

// Bytes returns the raw public key data (33 bytes compressed format).
func (p *publicKeyImpl) Bytes() []byte {
	return p.k.SerializeCompressed()
}

// String returns a base58 encoded string of the key binary data.
func (p *publicKeyImpl) String() string {
	return base58.Encode(p.Bytes())
}

// Pretty returns a readable short string of the public key.
func (p *publicKeyImpl) Pretty() string {
	pstr := p.String()
	maxRunes := 6
	if len(pstr) < maxRunes {
		maxRunes = len(pstr)
	}
	return fmt.Sprintf("<PUBK %s>", pstr[:maxRunes])
}

// VerifyString verifies data signed with the private key matching this public key.
// sig: hex encoded binary data string
func (p *publicKeyImpl) VerifyString(data []byte, sig string) (bool, error) {
	bin, err := hex.DecodeString(sig)
	if err != nil {
		return false, err
	}
	return p.Verify(data, bin)
}

// Verify verifies data was signed by provided signature.
// It returns true iff data was signed using the private key matching this public key.
/// sig: signature binary data
func (p *publicKeyImpl) Verify(data []byte, sig []byte) (bool, error) {
	signature, err := btcec.ParseSignature(sig, btcec.S256())
	if err != nil {
		return false, err
	}

	verified := signature.Verify(data, p.k)
	return verified, nil
}

// Encrypt encrypts data that can only be decrypted using the private key matching this public key.
func (p *publicKeyImpl) Encrypt(in []byte) ([]byte, error) {
	return btcec.Encrypt(p.k, in)
}
