package signing

import (
	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519/extra/ecvrf"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// VRFSigner is a signer for VRF purposes.
type VRFSigner struct {
	privateKey ed25519.PrivateKey
	nodeID     types.NodeID
}

// Sign signs a message for VRF purposes.
func (s VRFSigner) Sign(msg []byte) types.VrfSignature {
	return *(*[types.VrfSignatureSize]byte)(ecvrf.Prove(s.privateKey, msg))
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

type VRFVerifier func(types.NodeID, []byte, types.VrfSignature) bool

func NewVRFVerifier() VRFVerifier {
	return VRFVerify
}

// Verify verifies that a signature matches public key and message.
func (v VRFVerifier) Verify(nodeID types.NodeID, msg []byte, sig types.VrfSignature) bool {
	return v(nodeID, msg, sig)
}

// VRFVerify verifies that a signature matches public key and message.
func VRFVerify(nodeID types.NodeID, msg []byte, sig types.VrfSignature) bool {
	valid, _ := ecvrf.Verify(nodeID.Bytes(), sig[:], msg)
	return valid
}
