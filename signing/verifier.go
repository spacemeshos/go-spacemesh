package signing

import (
	"crypto/ed25519"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type edVerifierOption struct {
	prefix []byte
}

// VerifierOptionFunc to modify verifier.
type VerifierOptionFunc func(*edVerifierOption) error

// WithVerifierPrefix sets the prefix used by PubKeyVerifier. This usually is the Network ID.
func WithVerifierPrefix(prefix []byte) VerifierOptionFunc {
	return func(opts *edVerifierOption) error {
		opts.prefix = prefix
		return nil
	}
}

// EdVerifier extracts public keys from signatures.
type EdVerifier struct {
	prefix []byte
}

func NewEdVerifier(opts ...VerifierOptionFunc) (*EdVerifier, error) {
	cfg := &edVerifierOption{}
	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return nil, err
		}
	}
	Verifier := &EdVerifier{
		prefix: cfg.prefix,
	}
	return Verifier, nil
}

// Verify verifies that a signature matches public key and message.
func (es *EdVerifier) Verify(d Domain, nodeID types.NodeID, m []byte, sig types.EdSignature) bool {
	msg := make([]byte, 0, len(es.prefix)+1+len(m))
	msg = append(msg, es.prefix...)
	msg = append(msg, byte(d))
	msg = append(msg, m...)
	return ed25519.Verify(nodeID[:], msg, sig[:])
}
