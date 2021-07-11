package signing

import (
	"bytes"
	"fmt"

	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
)

// VRFSigner is a signer for VRF purposes
type VRFSigner struct {
	privateKey []byte
}

// Sign signs a message for VRF purposes
func (s VRFSigner) Sign(msg []byte) []byte {
	signature := ed25519.Sign(s.privateKey, msg)

	log.With().Info("Signed message",
		log.String("pk", util.Bytes2Hex(s.privateKey)),
		log.String("msg", util.Bytes2Hex(msg)),
		log.String("sig", util.Bytes2Hex(signature)),
	)

	return signature
}

// NewVRFSigner creates a new VRFSigner from a 32-byte seed
func NewVRFSigner(seed []byte) (*VRFSigner, []byte, error) {
	if len(seed) < ed25519.SeedSize {
		return nil, nil, fmt.Errorf("seed must be >=%d bytes (len(seed)=%d)", ed25519.SeedSize, len(seed))
	}
	vrfPub, vrfPriv, err := ed25519.GenerateKey(bytes.NewReader(seed))
	if err != nil {
		return nil, nil, err
	}

	log.With().Info("New VRF signer",
		log.String("pub", util.Bytes2Hex(vrfPub)),
		log.String("priv", util.Bytes2Hex(vrfPriv)),
	)

	return &VRFSigner{privateKey: vrfPriv}, vrfPub, nil
}

// VRFVerify verifies a message and signature, given a public key
func VRFVerify(pub, msg, sig []byte) bool {
	return ed25519.Verify(pub, msg, sig)
}
