package signing

import "github.com/spacemeshos/ed25519"

// VerifierOptionFunc to modify verifier.
type VerifierOptionFunc func(*EDVerifier)

// WithVerifierPrefix ...
func WithVerifierPrefix(prefix []byte) VerifierOptionFunc {
	return func(verifier *EDVerifier) {
		verifier.prefix = prefix
	}
}

// EDVerifier is a verifier for ED purposes.
type EDVerifier struct {
	prefix []byte
}

// NewEDVerifier returns a new EDVerifier.
func NewEDVerifier(opts ...VerifierOptionFunc) *EDVerifier {
	verifier := EDVerifier{}
	for _, opt := range opts {
		opt(&verifier)
	}
	return &verifier
}

// Extract public key from signature.
func (e EDVerifier) Extract(msg, sig []byte) (*PublicKey, error) {
	if e.prefix != nil {
		msg = append(e.prefix, msg...)
	}
	pub, err := ed25519.ExtractPublicKey(msg, sig)
	if err != nil {
		return nil, err
	}
	return &PublicKey{PublicKey: pub}, nil
}
