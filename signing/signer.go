package signing

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

type Domain byte

const (
	ATX Domain = 0

	PROPOSAL = 1
	BALLOT   = 2
	HARE     = 3
	POET     = 4
	MARRIAGE = 5

	BEACON_FIRST_MSG    = 10
	BEACON_FOLLOWUP_MSG = 11
)

// String returns the string representation of a domain.
func (d Domain) String() string {
	switch d {
	case ATX:
		return "ATX"
	case PROPOSAL:
		return "PROPOSAL"
	case BALLOT:
		return "BALLOT"
	case HARE:
		return "HARE"
	case POET:
		return "POET"
	case BEACON_FIRST_MSG:
		return "BEACON_FIRST_MSG"
	case BEACON_FOLLOWUP_MSG:
		return "BEACON_FOLLOWUP_MSG"
	default:
		return "UNKNOWN"
	}
}

type edSignerOption struct {
	priv   PrivateKey
	file   string
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

// ToFile writes the private key to a file after creation.
func ToFile(path string) EdSignerOptionFunc {
	return func(opt *edSignerOption) error {
		if opt.file != "" {
			return errors.New("invalid option ToFile: file already set")
		}
		opt.file = path
		return nil
	}
}

// FromFile loads the private key from a file.
func FromFile(path string) EdSignerOptionFunc {
	return func(opt *edSignerOption) error {
		if opt.priv != nil {
			return errors.New("invalid option FromFile: private key already set")
		}

		if opt.file != "" {
			return errors.New("invalid option FromFile: file already set")
		}

		// read hex data from file
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to open identity file at %s: %w", path, err)
		}

		if n := hex.DecodedLen(len(data)); n != PrivateKeySize {
			return fmt.Errorf("invalid key size %d/%d for %s", n, PrivateKeySize, filepath.Base(path))
		}

		dst := make([]byte, PrivateKeySize)
		n, err := hex.Decode(dst, data)
		if err != nil || n != PrivateKeySize {
			return fmt.Errorf("decoding private key in %s: %w", filepath.Base(path), err)
		}

		priv := PrivateKey(dst)
		keyPair := ed25519.NewKeyFromSeed(priv[:32])
		if !bytes.Equal(keyPair[32:], priv.Public().(ed25519.PublicKey)) {
			return errors.New("private and public do not match")
		}

		opt.priv = priv
		opt.file = filepath.Base(path)
		return nil
	}
}

// WithPrivateKey sets the private key used by EdSigner.
func WithPrivateKey(priv PrivateKey) EdSignerOptionFunc {
	return func(opt *edSignerOption) error {
		if opt.priv != nil {
			return errors.New("invalid option WithPrivateKey: private key already set")
		}

		if len(priv) != ed25519.PrivateKeySize {
			return errors.New("could not create EdSigner: invalid key length")
		}

		keyPair := ed25519.NewKeyFromSeed(priv[:32])
		if !bytes.Equal(keyPair[32:], priv.Public().(ed25519.PublicKey)) {
			return errors.New("private and public do not match")
		}

		opt.priv = priv
		return nil
	}
}

// WithKeyFromRand sets the private key used by EdSigner using predictable randomness source.
func WithKeyFromRand(rand io.Reader) EdSignerOptionFunc {
	return func(opt *edSignerOption) error {
		_, priv, err := ed25519.GenerateKey(rand)
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
	file string

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
		_, priv, err := ed25519.GenerateKey(nil)
		if err != nil {
			return nil, fmt.Errorf("could not generate key pair: %w", err)
		}
		cfg.priv = priv

		if cfg.file != "" {
			_, err := os.Stat(cfg.file)
			switch {
			case errors.Is(err, fs.ErrNotExist):
			// continue
			case err != nil:
				return nil, fmt.Errorf("stat identity file %s: %w", filepath.Base(cfg.file), err)
			default: // err == nil
				return nil, fmt.Errorf("save identity file %s: %w", filepath.Base(cfg.file), fs.ErrExist)
			}

			dst := make([]byte, hex.EncodedLen(len(cfg.priv)))
			hex.Encode(dst, cfg.priv)
			err = os.WriteFile(cfg.file, dst, 0o600)
			if err != nil {
				return nil, fmt.Errorf("failed to write identity file: %w", err)
			}
		}
	}
	sig := &EdSigner{
		priv:   cfg.priv,
		prefix: cfg.prefix,
		file:   cfg.file,
	}
	return sig, nil
}

// Sign signs the provided message.
func (es *EdSigner) Sign(d Domain, m []byte) types.EdSignature {
	msg := make([]byte, 0, len(es.prefix)+1+len(m))
	msg = append(msg, es.prefix...)
	msg = append(msg, byte(d))
	msg = append(msg, m...)

	return *(*[types.EdSignatureSize]byte)(ed25519.Sign(es.priv, msg))
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

// Name returns the name of the signer. This is the filename of the identity file.
func (es *EdSigner) Name() string {
	if es.file == "" {
		return ""
	}
	return filepath.Base(es.file)
}

// VRFSigner wraps same ed25519 key to provide ecvrf.
func (es *EdSigner) VRFSigner() *VRFSigner {
	return &VRFSigner{
		privateKey: ed25519.PrivateKey(es.priv),
		nodeID:     es.NodeID(),
	}
}

func (es *EdSigner) Prefix() []byte {
	return es.prefix
}

// Matches implements the gomock.Matcher interface for testing.
func (es *EdSigner) Matches(x any) bool {
	if other, ok := x.(*EdSigner); ok {
		return bytes.Equal(es.priv, other.priv)
	}
	return false
}

func (es *EdSigner) String() string {
	return es.NodeID().ShortString()
}
