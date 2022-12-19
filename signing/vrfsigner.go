package signing

import (
	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519/extra/ecvrf"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// VRFSigner is a signer for VRF purposes.
type VRFSigner struct {
	// cdb *datastore.CachedDB

	privateKey []byte
	nodeID     types.NodeID
}

// func (s VRFSigner) getVRFNonce(nodeId types.NodeID) (*types.VRFPostIndex, error) {
// 	atxId, err := atxs.GetFirstIDByNodeID(s.cdb, nodeId)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to get initial atx for miner %s: %w", nodeId.String(), err)
// 	}
// 	atx, err := s.cdb.GetAtxHeader(atxId)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to get initial atx for miner %s: %w", nodeId.String(), err)
// 	}
// 	return atx.VRFNonce, nil
// }

// Sign signs a message for VRF purposes.
func (s VRFSigner) Sign(msg []byte) []byte {
	return ecvrf.Prove(ed25519.PrivateKey(s.privateKey), msg)
}

// PublicKey of the signer.
func (s VRFSigner) PublicKey() *PublicKey {
	return NewPublicKey(s.nodeID.Bytes())
}

// LittleEndian indicates whether byte order in a signature is little-endian.
func (s VRFSigner) LittleEndian() bool {
	return true
}

// VRFVerifier is a verifier for VRF purposes.
type VRFVerifier struct{}

// Verify that signature matches public key.
func (VRFVerifier) Verify(pub *PublicKey, msg, sig []byte) bool {
	valid, _ := ecvrf.Verify(pub.Bytes(), sig, msg)
	return valid
}
