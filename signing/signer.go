package signing

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/spacemeshos/ed25519"

	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
)

type GenesisBoundEdSigner struct {
	genesis_id [20]byte
	signer     EdSigner
}

type GenesisBoundEdVerifier struct {
	genesis_id [20]byte
	verifier   EDVerifier
}

func (s *GenesisBoundEdSigner) Sign(m []byte) []byte {
	msg := append(m, s.genesis_id[:]...)
	return s.signer.Sign(msg)
}

func (v *GenesisBoundEdVerifier) Verify(pubkey *PublicKey, message []byte, sign []byte) bool {
	if len(message) < 20 {
		return false
	}
	gen_id_extracted := message[len(message)-20:]
	if string(gen_id_extracted[:]) != string(v.genesis_id[:]) {
		return false
	}
	return v.verifier.Verify(pubkey, message, sign)

}

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
	return util.Bytes2Hex(p.Bytes())
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
}

// PrivateKeySize size of the private key in bytes.
const PrivateKeySize = ed25519.PrivateKeySize

// NewEdSignerFromBuffer builds a signer from a private key as byte buffer.
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

// NewEdSignerFromRand generate signer using predictable randomness source.
func NewEdSignerFromRand(rand io.Reader) *EdSigner {
	pub, priv, err := ed25519.GenerateKey(rand)
	if err != nil {
		log.Panic("Could not generate key pair err=%v", err)
	}
	return &EdSigner{privKey: priv, pubKey: pub}
}

// NewEdSigner returns an auto-generated ed signer.
func NewEdSigner() *EdSigner {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		log.Panic("Could not generate key pair err=%v", err)
	}

	return &EdSigner{privKey: priv, pubKey: pub}
}

// Sign signs the provided message.
func (es *EdSigner) Sign(m []byte) []byte {
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

// Verify verifies the provided message.
func Verify(pubkey *PublicKey, message []byte, sign []byte) bool {
	return ed25519.Verify2(pubkey.Bytes(), message, sign)
}

// EDVerifier is a verifier for ED purposes.
type EDVerifier struct{}

// NewEDVerifier returns a new EDVerifier.
func NewEDVerifier() EDVerifier {
	return EDVerifier{}
}

var _ VerifyExtractor = EDVerifier{}

// Verify that signature matches public key.
func (EDVerifier) Verify(pub *PublicKey, msg, sig []byte) bool {
	return Verify(pub, msg, sig)
}

// Extract public key from signature.
func (EDVerifier) Extract(msg, sig []byte) (*PublicKey, error) {
	pub, err := ed25519.ExtractPublicKey(msg, sig)
	if err != nil {
		return nil, fmt.Errorf("extract ed25519 pubkey: %w", err)
	}
	return &PublicKey{pub: pub}, nil
}
