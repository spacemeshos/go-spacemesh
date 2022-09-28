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

// emptyGenesisID is a canonical empty GenesisID.
var emptyGenesisID = [20]byte{}

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
	privKey   ed25519.PrivateKey // the pub & private key
	pubKey    ed25519.PublicKey  // only the pub key part
	genesisID [20]byte
}

// PrivateKeySize size of the private key in bytes.
const PrivateKeySize = ed25519.PrivateKeySize

// NewEdSignerFromBuffer builds a signer from a private key as byte buffer.
func NewEdSignerFromBuffer(buff []byte, opts ...EdSignerOpt) (*EdSigner, error) {
	if len(buff) != ed25519.PrivateKeySize {
		log.Error("Could not create EdSigner from the provided buffer: buffer too small")
		return nil, errors.New("buffer too small")
	}
	edSigner := &EdSigner{privKey: buff, pubKey: buff[32:]}
	keyPair := ed25519.NewKeyFromSeed(edSigner.privKey[:32])
	if !bytes.Equal(keyPair[32:], edSigner.pubKey) {
		log.Error("Public key and private key does not match")
		return nil, errors.New("private and public does not match")
	}
	for _, opt := range opts {
		opt(edSigner)
	}
	return edSigner, nil
}

// NewEdSignerFromRand generate signer using predictable randomness source.
func NewEdSignerFromRand(rand io.Reader, opts ...EdSignerOpt) *EdSigner {
	pub, priv, err := ed25519.GenerateKey(rand)
	if err != nil {
		log.Panic("Could not generate key pair err=%v", err)
	}
	edSigner := &EdSigner{privKey: priv, pubKey: pub}
	for _, opt := range opts {
		opt(edSigner)
	}
	return edSigner
}

// EdSignerOpt is for configuring EdSigner.
type EdSignerOpt func(es *EdSigner)

// WithGenesisID sets genesisID for EdSigner.
func WithSignerGenesisID(genesisID [20]byte) EdSignerOpt {
	return func(es *EdSigner) {
		es.genesisID = genesisID
	}
}

// NewEdSigner returns an auto-generated ed signer.
func NewEdSigner(opts ...EdSignerOpt) *EdSigner {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		log.Panic("Could not generate key pair err=%v", err)
	}
	edSigner := &EdSigner{privKey: priv, pubKey: pub}
	for _, opt := range opts {
		opt(edSigner)
	}
	return edSigner
}

// Sign signs the provided message.
func (es *EdSigner) Sign(msg []byte) []byte {
	if es.genesisID != emptyGenesisID {
		msg = append(es.genesisID[:], msg...)
	}
	return ed25519.Sign2(es.privKey, msg)
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

// EdVerifier is a verifier for ED purposes.
type EdVerifier struct{ genesisID [20]byte }

// EdSignerOpt is for configuring EdSigner.
type EdVerifierOpt func(es *EdVerifier)

// WithGenesisID sets genesisID for EdSigner.
func WithVerifierGenesisID(genesisID [20]byte) EdVerifierOpt {
	return func(ev *EdVerifier) {
		ev.genesisID = genesisID
	}
}

// NewEdVerifier returns a new EDVerifier.
func NewEdVerifier(opts ...EdVerifierOpt) *EdVerifier {
	edVerifier := &EdVerifier{}
	for _, opt := range opts {
		opt(edVerifier)
	}
	return edVerifier
}

var _ VerifyExtractor = EdVerifier{}

// Verify that signature matches public key.
func (ev EdVerifier) Verify(pub *PublicKey, msg, sig []byte) bool {
	if ev.genesisID != emptyGenesisID {
		msg = append(ev.genesisID[:], msg...)
	}
	return Verify(pub, msg, sig)
}

// Extract public key from signature.
func (ev EdVerifier) Extract(msg, sig []byte) (*PublicKey, error) {
	if ev.genesisID != emptyGenesisID {
		msg = append(ev.genesisID[:], msg...)
	}
	pub, err := ed25519.ExtractPublicKey(msg, sig)
	if err != nil {
		return nil, fmt.Errorf("extract ed25519 pubkey: %w", err)
	}
	return &PublicKey{pub: pub}, nil
}
