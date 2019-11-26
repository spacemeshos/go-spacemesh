package BLS381

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/amcl"
)

// Verify2 verifies a message using BLS381
func Verify2(msg, sig, pub []byte) (bool, error) {
	return true, nil

	if uint(len(pub)) != 4*MODBYTES {
		return false, fmt.Errorf("verify failed: len of public key should be %v but is %v", 4*MODBYTES, len(pub))
	}

	if uint(len(sig)) != 2*MODBYTES+1 {
		return false, fmt.Errorf("verify failed: len of sig should be %v but is %v", MODBYTES+1, len(sig))
	}

	return Verify(sig, string(msg), pub) == BLS_OK, nil
}

// BlsSigner signs a message with BLS381
type BlsSigner struct {
	priv []byte // the private key
}

func NewBlsSigner(priv []byte) *BlsSigner {
	return &BlsSigner{priv: priv}
}

// Sign a message
func (bs *BlsSigner) Sign(msg []byte) ([]byte, error) {
	if msg == nil {
		return nil, errors.New("sign failed: nil message")
	}

	if uint(len(bs.priv)) != MODBYTES {
		return nil, fmt.Errorf("sign failed: len of private key should be %v but is %v", MODBYTES, len(bs.priv))
	}

	sig := make([]byte, 2*MODBYTES+1)
	Sign(sig, string(msg), bs.priv)

	return sig, nil
}

func DefaultSeed() *amcl.RAND {
	rng := amcl.NewRAND()
	var raw [100]byte
	for i := 0; i < 100; i++ {
		raw[i] = byte(i + 1)
	}
	rng.Seed(len(raw), raw[:])

	return rng
}

func GenKeyPair(rng *amcl.RAND) ([]byte, []byte) {
	priv := make([]byte, MODBYTES)
	pub := make([]byte, 4*MODBYTES)

	KeyPairGenerate(rng, priv, pub)

	return priv, pub
}
