package signing

import (
	"github.com/spacemeshos/ed25519"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// VerifierOptionFunc to modify verifier.
type VerifierOptionFunc func(*PubKeyExtractor)

// WithVerifierPrefix ...
func WithVerifierPrefix(prefix []byte) VerifierOptionFunc {
	return func(verifier *PubKeyExtractor) {
		verifier.prefix = prefix
	}
}

// PubKeyExtractor is a verifier for ED purposes.
type PubKeyExtractor struct {
	prefix []byte
}

// NewPubKeyExtractor returns a new EDVerifier.
func NewPubKeyExtractor(opts ...VerifierOptionFunc) *PubKeyExtractor {
	extractor := PubKeyExtractor{}
	for _, opt := range opts {
		opt(&extractor)
	}
	return &extractor
}

// Extract public key from signature.
func (e PubKeyExtractor) Extract(m, sig []byte) (*PublicKey, error) {
	msg := make([]byte, len(m)+len(e.prefix))
	copy(msg, e.prefix)
	copy(msg[len(e.prefix):], m)
	pub, err := ed25519.ExtractPublicKey(msg, sig)
	if err != nil {
		return nil, err
	}
	return &PublicKey{PublicKey: pub}, nil
}

func (e PubKeyExtractor) ExtractNodeID(m, sig []byte) (types.NodeID, error) {
	msg := make([]byte, len(m)+len(e.prefix))
	copy(msg, e.prefix)
	copy(msg[len(e.prefix):], m)
	pub, err := ed25519.ExtractPublicKey(msg, sig)
	if err != nil {
		return *types.EmptyNodeID, err
	}
	return types.BytesToNodeID(pub), nil
}
