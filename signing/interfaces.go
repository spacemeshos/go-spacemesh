package signing

// Signer ...
type Signer interface {
	Sign([]byte) []byte
	PublicKey() *PublicKey
}

// Verifier ...
type Verifier interface {
	Verify(pub *PublicKey, msg, sig []byte) bool
	Extract(msg, sig []byte) (*PublicKey, error)
}
