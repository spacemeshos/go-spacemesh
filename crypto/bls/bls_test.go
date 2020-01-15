package bls

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestBLS(t *testing.T) {
	req := require.New(t)

	// Generate key pair.
	sec, pub := GenKeyPair()
	req.Len(sec, SecKeyLen)
	req.Len(pub, PubKeyLen)

	// Sign.
	signer, err := NewSigner(sec)
	req.NotNil(signer)
	req.NoError(err)
	msg := []byte("this is a message")
	sig, err := signer.Sign(msg)
	req.NoError(err)
	req.Len(sig, SigLen)

	// Verify signature.
	ok, err := Verify(msg, sig, pub)
	req.NoError(err)
	req.True(ok)
}

func TestBLS_Randomness(t *testing.T) {
	req := require.New(t)

	n := 100
	set := make(map[string]bool)

	// Verify n distinct secret keys.
	for i := 0; i < n; i++ {
		sec, _ := GenKeyPair()
		secHex := fmt.Sprintf("%x", sec)
		_, ok := set[secHex]
		req.False(ok, "random secret key was repeated")

		set[secHex] = true
	}
}

//
//func TestBLS(t *testing.T) {
//	fmt.Println("Begin BLS")
//	rng := amcl.NewRAND()
//	var raw [100]byte
//	for i := 0; i < 100; i++ {
//		raw[i] = byte(i + 1)
//	}
//	rng.Seed(100, raw[:])
//	bls_BN254(rng)
//}
//
//func printBinary(array []byte) {
//	for i := 0; i < len(array); i++ {
//		fmt.Printf("%02x", array[i])
//	}
//	fmt.Printf("\n")
//}
//
//func bls_BN254(rng *amcl.RAND) {
//
//	const BGS = BN254.BGS
//	const BFS = BN254.BFS
//	const G1S = BFS + 1 /* Group 1 Size */
//	const G2S = 4 * BFS /* Group 2 Size */
//
//	var S [BGS]byte
//	var W [G2S]byte
//	var SIG [G1S]byte
//
//	fmt.Printf("\nTesting Boneh-Lynn-Shacham BLS signature code\n")
//	mess := "This is a test message"
//
//	BN254.KeyPairGenerate(rng, S[:], W[:])
//	fmt.Printf("Private key : 0x")
//	printBinary(S[:])
//	fmt.Printf("Public  key : 0x")
//	printBinary(W[:])
//
//	BN254.Sign(SIG[:], mess, S[:])
//	fmt.Printf("Signature : 0x")
//	printBinary(SIG[:])
//
//	res := BN254.Verify(SIG[:], mess, W[:])
//
//	if res == 0 {
//		fmt.Printf("Signature is OK\n")
//	} else {
//		fmt.Printf("Signature is *NOT* OK\n")
//	}
//}
