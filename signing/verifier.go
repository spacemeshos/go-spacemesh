package signing

import "github.com/spacemeshos/ed25519"

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
func (e PubKeyExtractor) Extract(msg, sig []byte) (*PublicKey, error) {
	if e.prefix != nil {
		msg = append(e.prefix, msg...)
	}
	pub, err := ed25519.ExtractPublicKey(msg, sig)
	if err != nil {
		return nil, err
	}
	return &PublicKey{PublicKey: pub}, nil
}
