package signing

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"

	oasis "github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	sm_ed25519 "github.com/spacemeshos/ed25519-recovery"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

type domain byte

const (
	ATX domain = iota
	BEACON
	BALLOT
	HARE
	POET
)

// String returns the string representation of a domain.
func (d domain) String() string {
	switch d {
	case ATX:
		return "ATX"
	case BEACON:
		return "BEACON"
	case BALLOT:
		return "BALLOT"
	case HARE:
		return "HARE"
	case POET:
		return "POET"
	default:
		return "UNKNOWN"
	}
}

type edSignerOption struct {
	priv   PrivateKey
	prefix []byte
}

// EdSignerOptionFunc modifies EdSigner.
type EdSignerOptionFunc func(*edSignerOption) error

// WithPrefix sets the prefix used by EdSigner. This usually is the Network ID.
func WithPrefix(prefix []byte) EdSignerOptionFunc {
	return func(opt *edSignerOption) error {
		opt.prefix = prefix
		return nil
	}
}

// WithPrivateKey sets the private key used by EdSigner.
func WithPrivateKey(priv PrivateKey) EdSignerOptionFunc {
	return func(opt *edSignerOption) error {
		if len(priv) != sm_ed25519.PrivateKeySize {
			return errors.New("could not create EdSigner from the provided key: too small")
		}

		keyPair := sm_ed25519.NewKeyFromSeed(priv[:32])
		if !bytes.Equal(keyPair[32:], priv.Public().(sm_ed25519.PublicKey)) {
			log.Error("Public key and private key do not match")
			return errors.New("private and public do not match")
		}

		opt.priv = priv
		return nil
	}
}

// WithKeyFromRand sets the private key used by EdSigner using predictable randomness source.
func WithKeyFromRand(rand io.Reader) EdSignerOptionFunc {
	return func(opt *edSignerOption) error {
		_, priv, err := sm_ed25519.GenerateKey(rand)
		if err != nil {
			return fmt.Errorf("could not generate key pair: %w", err)
		}

		opt.priv = priv
		return nil
	}
}

// EdSigner represents an ED25519 signer.
type EdSigner struct {
	priv PrivateKey

	prefix []byte
}

// NewEdSigner returns an auto-generated ed signer.
func NewEdSigner(opts ...EdSignerOptionFunc) (*EdSigner, error) {
	cfg := &edSignerOption{}

	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return nil, err
		}
	}

	if cfg.priv == nil {
		_, priv, err := sm_ed25519.GenerateKey(nil)
		if err != nil {
			return nil, fmt.Errorf("could not generate key pair: %w", err)
		}
		cfg.priv = priv
	}
	sig := &EdSigner{
		priv:   cfg.priv,
		prefix: cfg.prefix,
	}
	return sig, nil
}

// Sign signs the provided message.
func (es *EdSigner) Sign(d domain, m []byte) types.EdSignature {
	msg := make([]byte, 0, len(es.prefix)+1+len(m))
	msg = append(msg, es.prefix...)
	msg = append(msg, byte(d))
	msg = append(msg, m...)

	switch d {
	case ATX, BEACON:
		return *(*[types.EdSignatureSize]byte)(ed25519.Sign(es.priv, msg))
	default:
		return *(*[types.EdSignatureSize]byte)(sm_ed25519.Sign(es.priv, msg))
	}
}

// NodeID returns the node ID of the signer.
func (es *EdSigner) NodeID() types.NodeID {
	return types.BytesToNodeID(es.PublicKey().Bytes())
}

// PublicKey returns the public key of the signer.
func (es *EdSigner) PublicKey() *PublicKey {
	return NewPublicKey(es.priv.Public().(sm_ed25519.PublicKey))
}

// PrivateKey returns private key.
func (es *EdSigner) PrivateKey() PrivateKey {
	return es.priv
}

// VRFSigner wraps same ed25519 key to provide ecvrf.
func (es *EdSigner) VRFSigner() (*VRFSigner, error) {
	return &VRFSigner{
		privateKey: oasis.PrivateKey(es.priv),
		nodeID:     es.NodeID(),
	}, nil
}

func (es *EdSigner) Prefix() []byte {
	return es.prefix
}
