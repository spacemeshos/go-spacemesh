package signing

import "math/rand"

// RandomProposalID generates a random ProposalID for testing.
func RandomEdSignature() EdSignature {
	var b [EdSignatureSize]byte
	_, err := rand.Read(b[:])
	if err != nil {
		return EdSignature{}
	}
	return EdSignature(b)
}
