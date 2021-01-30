package types

import (
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/signing"
)

// Signer is an alias to Ed signer
type Signer = *signing.EdSigner

// SigningScheme is an signing interface using to sign/verify transactions
type SigningScheme struct{ *SigningSchemeObject }

// EdSigningScheme is an classic Ed25519 scheme
var EdSigningScheme = SigningSchemeObject{
	Value:        0,
	Extractable:  false,
	PubKeyLength: ed25519.PublicKeySize,

	Sign: func(signer Signer, data []byte) TxSignature {
		return TxSignatureFromBytes(signer.Sign1(data))
	},

	Verify: func(data []byte, pubKey TxPublicKey, signature TxSignature) bool {
		return ed25519.Verify(pubKey.k[:], data, signature[:])
	},

	ExtractPubKey: func(data []byte, signature TxSignature) (TxPublicKey, bool, error) {
		return TxPublicKey{}, false, nil
	},
}.New()

// EdPlusSigningScheme is an classic Ed25519++ scheme
var EdPlusSigningScheme = SigningSchemeObject{
	Value:        1,
	Extractable:  true,
	PubKeyLength: ed25519.PublicKeySize,

	Sign: func(signer Signer, data []byte) TxSignature {
		return TxSignatureFromBytes(signer.Sign2(data))
	},

	Verify: func(data []byte, pubKey TxPublicKey, signature TxSignature) bool {
		return ed25519.Verify2(pubKey.k[:], data, signature[:])
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

// SigningSchemeObject is an backand signing scheme object
type SigningSchemeObject struct {
	Value         int
	Extractable   bool
	PubKeyLength  int
	Sign          func(signer Signer, data []byte) TxSignature
	Verify        func(data []byte, pubKey TxPublicKey, signature TxSignature) bool
	ExtractPubKey func(data []byte, signature TxSignature) (TxPublicKey, bool, error)
}

// New creates new signing scheme
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
type TxPublicKey struct {
	k [TxPublicKeyLength]byte
}

// TxPublicKeyFromBytes converts bytes to the public key
func TxPublicKeyFromBytes(bs []byte) (pk TxPublicKey) {
	copy(pk.k[:], bs)
	return
}

// Bytes returns public key's bytes
func (pk TxPublicKey) Bytes() []byte {
	return pk.k[:]
}
