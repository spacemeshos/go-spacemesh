package signing

import (
	"bytes"
	"errors"
	"io"

	"github.com/spacemeshos/ed25519"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// EdSigner represents an ED25519 signer.
type EdSigner struct {
	priv PrivateKey

	prefix []byte
}

// SignerOptionFunc modifies EdSigner.
type SignerOptionFunc func(*EdSigner)

// WithPrefix sets used by EdSigner.
func WithPrefix(prefix []byte) SignerOptionFunc {
	return func(signer *EdSigner) {
		signer.prefix = prefix
	}
}

// NewEdSignerFromKey builds a signer from a private key as byte buffer.
func NewEdSignerFromKey(key PrivateKey, opts ...SignerOptionFunc) (*EdSigner, error) {
	if len(key) != ed25519.PrivateKeySize {
		log.Error("Could not create EdSigner from the provided buffer: buffer too small")
		return nil, errors.New("buffer too small")
	}

	sgn := &EdSigner{priv: key}
	keyPair := ed25519.NewKeyFromSeed(sgn.priv[:32])
	if !bytes.Equal(keyPair[32:], sgn.priv.Public().(ed25519.PublicKey)) {
		log.Error("Public key and private key do not match")
		return nil, errors.New("private and public do not match")
	}
	for _, opt := range opts {
		opt(sgn)
	}
	return sgn, nil
}

// NewEdSignerFromRand generate signer using predictable randomness source.
func NewEdSignerFromRand(rand io.Reader) *EdSigner {
	_, priv, err := ed25519.GenerateKey(rand)
	if err != nil {
		log.Panic("Could not generate key pair err=%v", err)
	}
	return &EdSigner{priv: priv}
}

// NewEdSigner returns an auto-generated ed signer.
func NewEdSigner(opts ...SignerOptionFunc) *EdSigner {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		log.Panic("Could not generate key pair err=%v", err)
	}
	signer := &EdSigner{priv: priv}
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
	return ed25519.Sign2(es.priv, m)
}

// NodeID returns the node ID of the signer.
func (es *EdSigner) NodeID() types.NodeID {
	return types.BytesToNodeID(es.PublicKey().Bytes())
}

// PublicKey returns the public key of the signer.
func (es *EdSigner) PublicKey() *PublicKey {
	return NewPublicKey(es.priv.Public().(ed25519.PublicKey))
}

// PrivateKey returns private key.
func (es *EdSigner) PrivateKey() PrivateKey {
	return es.priv
}

// LittleEndian indicates whether byte order in a signature is little-endian.
func (es *EdSigner) LittleEndian() bool {
	return true
}

// VRFSigner wraps same ed25519 key to provide ecvrf.
func (es *EdSigner) VRFSigner() *VRFSigner {
	return &VRFSigner{
		privateKey: es.priv,
		nodeID:     es.NodeID(),
	}
}
