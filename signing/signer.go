package signing

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"

	"github.com/spacemeshos/ed25519"

	"github.com/spacemeshos/go-spacemesh/log"
)

// PrivateKey is an alias to spacemeshos/ed25519.PrivateKey.
type PrivateKey = ed25519.PrivateKey

const shortStringSize = 5

// Public returns public key part from ed25519 private key.
func Public(priv PrivateKey) ed25519.PublicKey {
	return ed25519.PublicKey(priv[ed25519.PrivateKeySize-ed25519.PublicKeySize:])
}

// PublicKey is the type describing a public key.
type PublicKey struct {
	pub []byte
}

// NewPublicKey constructs a new public key instance from a byte array.
func NewPublicKey(pub []byte) *PublicKey {
	return &PublicKey{pub}
}

// Field returns a log field. Implements the LoggableField interface.
func (p *PublicKey) Field() log.Field {
	return log.String("public_key", p.ShortString())
}

// Bytes returns the public key as byte array.
func (p *PublicKey) Bytes() []byte {
	// Prevent segfault if unset
	if p != nil {
		return p.pub
	}
	return nil
}

// String returns the public key as a hex representation string.
func (p *PublicKey) String() string {
	return hex.EncodeToString(p.Bytes())
}

// ShortString returns a representative sub string.
func (p *PublicKey) ShortString() string {
	s := p.String()
	if len(s) < shortStringSize {
		return s
	}

	return s[:shortStringSize]
}

// Equals returns true iff the public keys are equal.
func (p *PublicKey) Equals(o *PublicKey) bool {
	return bytes.Equal(p.Bytes(), o.Bytes())
}

var _ Signer = (*EdSigner)(nil)

// EdSigner represents an ED25519 signer.
type EdSigner struct {
	privKey ed25519.PrivateKey // the pub & private key
	pubKey  ed25519.PublicKey  // only the pub key part

	prefix []byte
}

// PrivateKeySize size of the private key in bytes.
const PrivateKeySize = ed25519.PrivateKeySize

// SignerOpt modifies EdSigner.
type SignerOpt func(*EdSigner)

// WithSignerPrefix sets used by EdSigner.
func WithSignerPrefix(prefix []byte) SignerOpt {
	return func(signer *EdSigner) {
		signer.prefix = prefix
	}
}

// NewEdSignerFromBuffer builds a signer from a private key as byte buffer.
func NewEdSignerFromBuffer(buff []byte, opts ...SignerOpt) (*EdSigner, error) {
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
	for _, opt := range opts {
		opt(sgn)
	}
	return sgn, nil
}

// NewEdSignerFromRand generate signer using predictable randomness source.
func NewEdSignerFromRand(rand io.Reader) *EdSigner {
	pub, priv, err := ed25519.GenerateKey(rand)
	if err != nil {
		log.Panic("Could not generate key pair err=%v", err)
	}
	return &EdSigner{privKey: priv, pubKey: pub}
}

// NewEdSigner returns an auto-generated ed signer.
func NewEdSigner(opts ...SignerOpt) *EdSigner {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		log.Panic("Could not generate key pair err=%v", err)
	}
	signer := &EdSigner{privKey: priv, pubKey: pub}
	for _, opt := range opts {
		opt(signer)
	}
	return signer
}

// Sign signs the provided message.
func (es *EdSigner) Sign(m []byte) []byte {
	if es.prefix != nil {
		m = append(es.prefix, m...)
	}
	return ed25519.Sign2(es.privKey, m)
}

// PublicKey returns the public key of the signer.
func (es *EdSigner) PublicKey() *PublicKey {
	return NewPublicKey(es.pubKey)
}

// PrivateKey returns private key.
func (es *EdSigner) PrivateKey() PrivateKey {
	return es.privKey
}

// LittleEndian indicates whether byte order in a signature is little-endian.
func (es *EdSigner) LittleEndian() bool {
	return true
}

// VRFSigner wraps same ed25519 key to provide ecvrf.
func (es *EdSigner) VRFSigner() *VRFSigner {
	return &VRFSigner{
		privateKey: es.privKey,
		pub:        es.PublicKey(),
	}
}

// ToBuffer returns the private key as a byte buffer.
func (es *EdSigner) ToBuffer() []byte {
	buff := make([]byte, len(es.privKey))
	copy(buff, es.privKey)

	return buff
}

// VerifierOpt to modify verifier.
type VerifierOpt func(*EDVerifier)

// WithVerifierPrefix ...
func WithVerifierPrefix(prefix []byte) VerifierOpt {
	return func(verifier *EDVerifier) {
		verifier.prefix = prefix
	}
}

// DefaultVerifier used by ExtractPublicKey.
var DefaultVerifier = EDVerifier{}

// ExtractPublicKey using DefaultVerifier Extract method.
func ExtractPublicKey(msg, sig []byte) (ed25519.PublicKey, error) {
	pub, err := DefaultVerifier.Extract(msg, sig)
	if err != nil {
		return nil, err
	}
	return pub.Bytes(), nil
}

// EDVerifier is a verifier for ED purposes.
type EDVerifier struct {
	prefix []byte
}

// NewEDVerifier returns a new EDVerifier.
func NewEDVerifier(opts ...VerifierOpt) EDVerifier {
	verifier := EDVerifier{}
	for _, opt := range opts {
		opt(&verifier)
	}
	return verifier
}

var _ VerifyExtractor = EDVerifier{}

// Extract public key from signature.
func (e EDVerifier) Extract(msg, sig []byte) (*PublicKey, error) {
	if e.prefix != nil {
		msg = append(e.prefix, msg...)
	}
	pub, err := ed25519.ExtractPublicKey(msg, sig)
	if err != nil {
		return nil, err
	}
	return &PublicKey{pub: pub}, nil
}
