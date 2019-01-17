package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"math/rand"
)

type Signing interface {
	Sign(m []byte) []byte
	Verifier() Verifier
}

type MockSigning struct {
	key crypto.PrivateKey
}

func NewMockSigning() *MockSigning {
	ms := new(MockSigning)

	token := make([]byte, 32)
	rand.Read(token)

	priv, _, err := crypto.GenerateKeyPair()
	if err != nil {
		panic("MockSigning construction failed")
	}
	ms.key = priv

	return ms
}

func (ms *MockSigning) Sign(m []byte) []byte {
	sig, err := ms.key.Sign(m)
	if err != nil {
		panic("Could not sign message")
	}

	return sig
}

func (ms *MockSigning) Verifier() Verifier {
	return NewVerifier(ms.key.GetPublicKey().Bytes())
}

type Verifier interface {
	Verify(data []byte, sig []byte) bool
	Bytes() []byte
	String() string
}

type PubVerifier struct {
	pub crypto.PublicKey
}

func NewVerifier(bytes []byte) Verifier {
	mv := new(PubVerifier)
	pub, err := crypto.NewPublicKey(bytes)
	if err != nil {
		panic("PubVerifier construction failed")
	}
	mv.pub = pub

	return mv
}

// Returns true if validation succeeds and false otherwise
func (mv *PubVerifier) Verify(data []byte, sig []byte) bool {
	result, err := mv.pub.Verify(data, sig)
	if err != nil {
		log.Error("Fatal: verification returned an error: ", err)
		return false
	}

	return result
}

func (mv *PubVerifier) Bytes() []byte {
	return mv.pub.Bytes()
}

func (mv *PubVerifier) String() string {
	return mv.pub.String()
}