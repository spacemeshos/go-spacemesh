package signing

import (
	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519/extra/ecvrf"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// VRFSigner is a signer for VRF purposes.
type VRFSigner struct {
	privateKey []byte
	nodeID     types.NodeID
}

// Sign signs a message for VRF purposes.
func (s VRFSigner) Sign(msg []byte) ([]byte, error) {
	return ecvrf.Prove(s.privateKey, msg), nil
}

// NodeID of the signer.
func (s VRFSigner) NodeID() types.NodeID {
	return s.nodeID
}

// PublicKey of the signer.
func (s VRFSigner) PublicKey() *PublicKey {
	return NewPublicKey(s.nodeID.Bytes())
}

// LittleEndian indicates whether byte order in a signature is little-endian.
func (s VRFSigner) LittleEndian() bool {
	return true
}

func VRFVerify(nodeID types.NodeID, msg, sig []byte) bool {
	valid, _ := ecvrf.Verify(nodeID.Bytes(), sig, msg)
	return valid
}
