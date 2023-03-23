package signing

import (
	"github.com/spacemeshos/ed25519-recovery"

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
func (e PubKeyExtractor) Extract(d domain, m []byte, sig [64]byte) (*PublicKey, error) {
	msg := make([]byte, 0, len(e.prefix)+1+len(m))
	msg = append(msg, e.prefix...)
	msg = append(msg, byte(d))
	msg = append(msg, m[:]...)
	pub, err := ed25519.ExtractPublicKey(msg, sig[:])
	if err != nil {
		return nil, err
	}
	return &PublicKey{PublicKey: pub}, nil
}

// ExtractNodeID from a signature.
func (e PubKeyExtractor) ExtractNodeID(d domain, m []byte, sig [64]byte) (types.NodeID, error) {
	pub, err := e.Extract(d, m, sig)
	if err != nil {
		return types.EmptyNodeID, err
	}
	return types.BytesToNodeID(pub.PublicKey), nil
}
