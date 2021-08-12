package signing

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interfaces.go Signer,Verifier

// Signer is a common interface for signature generation.
type Signer interface {
	Sign([]byte) []byte
	PublicKey() *PublicKey
}

// Verifier is a common interface for signature verification.
type Verifier interface {
	Verify(pub *PublicKey, msg, sig []byte) bool
}

// VerifyExtractor is a common interface for signature verification with support of public key extraction.
type VerifyExtractor interface {
	Verifier
	Extract(msg, sig []byte) (*PublicKey, error)
}
