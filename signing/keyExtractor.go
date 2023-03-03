package signing

import (
	"github.com/spacemeshos/ed25519-recovery"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type edExtractorOption struct {
	prefix []byte
	domain Domain
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

// WithExtractorDomain sets the domain of this key extractor.
func WithExtractorDomain(d Domain) ExtractorOptionFunc {
	return func(opts *edExtractorOption) error {
		opts.domain = d
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
	prefix := cfg.prefix
	if cfg.domain != Default {
		prefix = make([]byte, len(cfg.prefix)+1)
		copy(prefix, cfg.prefix)
		prefix[len(cfg.prefix)] = byte(cfg.domain)
	}
	extractor := &PubKeyExtractor{
		prefix: prefix,
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

// ExtractNodeID from a signature.
func (e PubKeyExtractor) ExtractNodeID(m, sig []byte) (types.NodeID, error) {
	pub, err := e.Extract(m, sig)
	if err != nil {
		return types.EmptyNodeID, err
	}
	return types.BytesToNodeID(pub.PublicKey), nil
}
