package signing

import (
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

type edExtractorOption struct {
	prefix []byte
}

// ExtractorOptionFunc to modify verifier.
type ExtractorOptionFunc func(*edExtractorOption) error

// WithExtractorPrefix sets the prefix used by PubKeyExtractor. This usually is the Network ID.
func WithExtractorPrefix(prefix []byte) ExtractorOptionFunc {
	return func(opts *edExtractorOption) error {
		opts.prefix = prefix
		return nil
	}
}

// PubKeyExtractor extracts public keys from signatures.
type PubKeyExtractor struct {
	prefix []byte
}

// NewPubKeyExtractor returns a new PubKeyExtractor.
func NewPubKeyExtractor(opts ...ExtractorOptionFunc) (*PubKeyExtractor, error) {
	cfg := &edExtractorOption{}
	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return nil, err
		}
	}
	extractor := &PubKeyExtractor{
		prefix: cfg.prefix,
	}
	return extractor, nil
}

// Extract public key from a signature.
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
	return types.BytesToNodeID(pub), err
}
