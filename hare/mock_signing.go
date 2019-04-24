package hare

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
)

type MockSigning struct {
	key crypto.PrivateKey
}

func NewMockSigning() *MockSigning {
	ms := new(MockSigning)

	priv, _, err := crypto.GenerateKeyPair()
	if err != nil {
		log.Error("Could not create private key ", err)
		log.Panic("MockSigning construction failed")
	}
	ms.key = priv

	return ms
}

func (ms *MockSigning) Sign(m []byte) []byte {
	sig, err := ms.key.Sign(m)
	if err != nil {
		log.Error("Error signing InnerMsg: ", err)
		log.Panic("Could not sign InnerMsg")
	}

	return sig
}

func (ms *MockSigning) Verifier() *PubVerifier {
	v, err := NewVerifier(ms.key.GetPublicKey().Bytes())
	if err != nil {
		log.Panic("Error getting public key", err.Error())
	}

	return v
}

type PubVerifier struct {
	pub crypto.PublicKey
}

func NewVerifier(bytes []byte) (*PubVerifier, error) {
	mv := new(PubVerifier)
	pub, err := crypto.NewPublicKey(bytes)
	if err != nil {
		return nil, err
	}
	mv.pub = pub

	return mv, nil
}

// Returns true if validation succeeds and false otherwise
func (mv *PubVerifier) Verify(data []byte, sig []byte) (bool, error) {
	result, err := mv.pub.Verify(data, sig)
	if err != nil {
		log.Error("Fatal: verification returned an error: ", err)
		return false, err
	}

	return result, nil
}

func (mv *PubVerifier) Bytes() []byte {
	return mv.pub.Bytes()
}

func (mv *PubVerifier) String() string {
	return mv.pub.String()
}
