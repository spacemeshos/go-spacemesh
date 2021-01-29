package types

import (
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/signing"
)

type Signer = *signing.EdSigner
type SigningScheme struct{ *SigningSchemeObject }

var EdSigningScheme = SigningSchemeObject{
	Value: func() int { return 0 },

	Extractable: func() bool { return false },

	Sign: func(signer Signer, data []byte) TxSignature {
		return TxSignatureFromBytes(signer.Sign1(data))
	},

	Verify: func(data []byte, pubKey TxPublicKey, signature TxSignature) bool {
		return ed25519.Verify(pubKey[:], data, signature[:])
	},

	ExtractPubKey: func(data []byte, signature TxSignature) (TxPublicKey, bool, error) {
		return TxPublicKey{}, false, nil
	},
}.New()

var EdPlusSigningScheme = SigningSchemeObject{
	Value: func() int { return 1 },

	Extractable: func() bool { return true },

	Sign: func(signer Signer, data []byte) TxSignature {
		return TxSignatureFromBytes(signer.Sign2(data))
	},

	Verify: func(data []byte, pubKey TxPublicKey, signature TxSignature) bool {
		return ed25519.Verify2(pubKey[:], data, signature[:])
	},

	ExtractPubKey: func(data []byte, signature TxSignature) (pubKey TxPublicKey, ok bool, err error) {
		ok = true
		pk, err := ed25519.ExtractPublicKey(data, signature[:])
		if err != nil {
			return
		}
		pubKey = TxPublicKeyFromBytes(pk)
		return
	},
}.New()

type SigningSchemeObject struct {
	Value         func() int
	Extractable   func() bool
	Sign          func(signer Signer, data []byte) TxSignature
	Verify        func(data []byte, pubKey TxPublicKey, signature TxSignature) bool
	ExtractPubKey func(data []byte, signature TxSignature) (TxPublicKey, bool, error)
}

func (sso SigningSchemeObject) New() SigningScheme {
	return SigningScheme{&sso}
}

// TxSignatureLength defines signature length
// TODO: signing does not have signature length constant
const TxSignatureLength = ed25519.SignatureSize

// TxSignature is a signature of a transaction
type TxSignature [TxSignatureLength]byte

// TxSignatureFromBytes transforms bytes to the signature
func TxSignatureFromBytes(bs []byte) (sig TxSignature) {
	copy(sig[:], bs)
	return
}

// Bytes returns signature's bytes
func (sig TxSignature) Bytes() []byte {
	return sig[:]
}

// TxPublicKeyLength defines public key length
// TODO: signing does not have public key length constant
const TxPublicKeyLength = ed25519.PublicKeySize

// TxPublicKey is a public key of a transaction
type TxPublicKey [TxPublicKeyLength]byte

// TxPublicKeyFromBytes converts bytes to the public key
func TxPublicKeyFromBytes(bs []byte) (pk TxPublicKey) {
	copy(pk[:], bs)
	return
}

// Bytes returns public key's bytes
func (pk TxPublicKey) Bytes() []byte {
	return pk[:]
}
