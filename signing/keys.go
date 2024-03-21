package signing

import (
	"encoding/hex"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"

	"github.com/spacemeshos/go-spacemesh/log"
)

// PrivateKey is an alias to ed25519.PrivateKey.
type PrivateKey = ed25519.PrivateKey

// PrivateKeySize size of the private key in bytes.
const PrivateKeySize = ed25519.PrivateKeySize

// PublicKey is the type describing a public key.
type PublicKey struct {
	ed25519.PublicKey
}

func Public(priv PrivateKey) ed25519.PublicKey {
	return priv.Public().(ed25519.PublicKey)
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
		return p.PublicKey
	}
	return nil
}

// String returns the public key as a hex representation string.
func (p *PublicKey) String() string {
	return hex.EncodeToString(p.Bytes())
}

const shortStringSize = 5

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
	return p.PublicKey.Equal(o.PublicKey)
}
