package signing

import (
	"bytes"
	"errors"
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
)

const shortStringSize = 5

// PublicKey is the type describing a public key
type PublicKey struct {
	pub []byte
}

// NewPublicKey constructs a new public key instance from a byte array
func NewPublicKey(pub []byte) *PublicKey {
	return &PublicKey{pub}
}

// Field returns a log field. Implements the LoggableField interface.
func (p *PublicKey) Field() log.Field {
	return log.String("public_key", p.ShortString())
}

// Bytes returns the public key as byte array
func (p *PublicKey) Bytes() []byte {
	// Prevent segfault if unset
	if p != nil {
		return p.pub
	}
	return nil
}

// String returns the public key as a hex representation string
func (p *PublicKey) String() string {
	return util.Bytes2Hex(p.Bytes())
}

// ShortString returns a representative sub string
func (p *PublicKey) ShortString() string {
	s := p.String()
	if len(s) < shortStringSize {
		return s
	}

	return s[:shortStringSize]
}

// Equals returns true iff the public keys are equal
func (p *PublicKey) Equals(o *PublicKey) bool {
	return bytes.Equal(p.Bytes(), o.Bytes())
}

// EdSigner represents an ED25519 signer
type EdSigner struct {
	privKey ed25519.PrivateKey // the pub & private key
	pubKey  ed25519.PublicKey  // only the pub key part
}

// NewEdSignerFromBuffer builds a signer from a private key as byte buffer
func NewEdSignerFromBuffer(buff []byte) (*EdSigner, error) {
	if len(buff) != ed25519.PrivateKeySize {
		log.Error("Could not create EdSigner from the provided buffer: buffer too small")
		return nil, errors.New("buffer too small")
	}

	sgn := &EdSigner{privKey: buff, pubKey: buff[32:]}
	keyPair := ed25519.NewKeyFromSeed(sgn.privKey[:32])
	if !bytes.Equal(keyPair[32:], sgn.pubKey) {
		log.Error("Public key and private key does not match")
		return nil, errors.New("private and public does not match")
	}

	return sgn, nil
}

// NewEdSigner returns an auto-generated ed signer
func NewEdSigner() *EdSigner {
	pub, priv, err := ed25519.GenerateKey(nil)

	if err != nil {
		log.Panic("Could not generate key pair err=%v", err)
	}

	return &EdSigner{privKey: priv, pubKey: pub}
}

// Sign signs the provided message
func (es *EdSigner) Sign(m []byte) []byte {
	return ed25519.Sign2(es.privKey, m)
}

// Verify verifies the provided message
func Verify(pubkey *PublicKey, message []byte, sign []byte) bool {
	return ed25519.Verify2(ed25519.PublicKey(pubkey.Bytes()), message, sign)
}

// PublicKey returns the public key of the signer
func (es *EdSigner) PublicKey() *PublicKey {
	return NewPublicKey(es.pubKey)
}

// ToBuffer returns the private key as a byte buffer
func (es *EdSigner) ToBuffer() []byte {
	buff := make([]byte, len(es.privKey))
	copy(buff, es.privKey)

	return buff
}
