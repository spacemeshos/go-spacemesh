package crypto

type VRFSigner struct {
	privateKey []byte
}

func (s *VRFSigner) Sign(message []byte) []byte {
	// TODO: implement
	return message
}

func NewVRFSigner(privateKey []byte) *VRFSigner {
	return &VRFSigner{privateKey: privateKey}
}
