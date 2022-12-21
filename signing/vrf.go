package signing

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519/extra/ecvrf"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
)

// VRFSigner is a signer for VRF purposes.
type VRFSigner struct {
	privateKey []byte
	fetcher    nonceFetcher

	nodeID types.NodeID
}

// Sign signs a message for VRF purposes.
func (s VRFSigner) Sign(msg []byte) ([]byte, error) {
	nonce, err := s.fetcher.NonceForNode(s.nodeID)
	if err != nil {
		return nil, err
	}

	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, uint64(nonce))
	return ecvrf.Prove(ed25519.PrivateKey(s.privateKey), append(buf, msg...)), nil
}

// PublicKey of the signer.
func (s VRFSigner) PublicKey() *PublicKey {
	return NewPublicKey(s.nodeID.Bytes())
}

// LittleEndian indicates whether byte order in a signature is little-endian.
func (s VRFSigner) LittleEndian() bool {
	return true
}

type VRFOptionFunc func(*vrfOption) error

type vrfOption struct {
	nonceMap map[types.NodeID]types.VRFPostIndex
	db       *datastore.CachedDB
}

func (opt *vrfOption) validate() error {
	if opt.nonceMap == nil && opt.db == nil {
		return errors.New("no source for VRF nonces provided")
	}
	return nil
}

func (opt *vrfOption) getFetcher() nonceFetcher {
	if opt.nonceMap != nil {
		return mapFetcher(opt.nonceMap)
	}
	return dbFetcher{opt.db}
}

// WithNonceForNode sets the nonce for a specific node. This is intended primarily for testing.
// This option can be used multiple times to set different nonces for different nodes.
// Using this option will cause the verifier to ignore the database.
func WithNonceForNode(nonce types.VRFPostIndex, node types.NodeID) VRFOptionFunc {
	return func(opts *vrfOption) error {
		if opts.nonceMap == nil {
			opts.nonceMap = make(map[types.NodeID]types.VRFPostIndex)
		}
		opts.nonceMap[node] = nonce
		return nil
	}
}

// WithNonceFromDB sets the database to use for retrieving nonces.
// Use this option to verify VRF signatures in production.
func WithNonceFromDB(db *datastore.CachedDB) VRFOptionFunc {
	return func(opts *vrfOption) error {
		opts.db = db
		return nil
	}
}

type VRFVerifier struct {
	fetcher nonceFetcher
}

type dbFetcher struct {
	*datastore.CachedDB
}

func (f dbFetcher) NonceForNode(nodeID types.NodeID) (types.VRFPostIndex, error) {
	atxId, err := atxs.GetFirstIDByNodeID(f, nodeID)
	if err != nil {
		return 0, fmt.Errorf("failed to get initial atx for smesher %s: %w", nodeID.String(), err)
	}
	atx, err := f.GetAtxHeader(atxId)
	if err != nil {
		return 0, fmt.Errorf("failed to get initial atx for smesher %s: %w", nodeID.String(), err)
	}
	return *atx.VRFNonce, nil
}

// mapFetcher is used as a source for nodeid -> nonce mappings when WithNonceForNode is used.
type mapFetcher map[types.NodeID]types.VRFPostIndex

func (m mapFetcher) NonceForNode(nodeID types.NodeID) (types.VRFPostIndex, error) {
	nonce, ok := m[nodeID]
	if !ok {
		return 0, fmt.Errorf("no nonce for node %s", nodeID.String())
	}
	return nonce, nil
}

func NewVRFVerifier(opts ...VRFOptionFunc) (*VRFVerifier, error) {
	cfg := &vrfOption{}
	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return nil, err
		}
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &VRFVerifier{
		fetcher: cfg.getFetcher(),
	}, nil
}

// Verify that signature matches public key.
func (v VRFVerifier) Verify(nodeID types.NodeID, msg, sig []byte) bool {
	nonce, err := v.fetcher.NonceForNode(nodeID)
	if err != nil {
		return false
	}

	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, uint64(nonce))
	valid, _ := ecvrf.Verify(nodeID.Bytes(), sig, append(buf, msg...))
	return valid
}
