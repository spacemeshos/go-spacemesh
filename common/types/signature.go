package types

import (
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/signing"
)

// EdSigning is the classic Ed25519 signing scheme
const EdSigning = EdSigningScheme(0)

// EdPlusSigning is the extended Ed25519++ signing scheme
const EdPlusSigning = EdPlusSigningScheme(1)

// SigningScheme defines the signing scheme
type SigningScheme interface {
	Sign(signer *signing.EdSigner, data []byte) TxSignature
	Verify(data []byte, pubKey TxPublicKey, signature TxSignature) bool
	Extract(data []byte, signature TxSignature) (TxPublicKey, bool, error)
	ExtractablePubKey() bool
}

type EdSigningScheme int

func (EdSigningScheme) Sign(signer *signing.EdSigner, data []byte) TxSignature {
	return TxSignatureFromBytes(signer.Sign1(data))
}

func (EdSigningScheme) Verify(data []byte, pubKey TxPublicKey, signature TxSignature) bool {
	return ed25519.Verify(pubKey[:], data, signature[:])
}

func (EdSigningScheme) Extract(data []byte, signature TxSignature) (TxPublicKey, bool, error) {
	return TxPublicKey{}, false, nil
}

func (EdSigningScheme) ExtractablePubKey() bool {
	return false
}

type EdPlusSigningScheme int

func (EdPlusSigningScheme) Sign(signer *signing.EdSigner, data []byte) TxSignature {
	return TxSignatureFromBytes(signer.Sign2(data))
}

func (EdPlusSigningScheme) Verify(data []byte, pubKey TxPublicKey, signature TxSignature) bool {
	return ed25519.Verify2(pubKey[:], data, signature[:])
}

func (EdPlusSigningScheme) Extract(data []byte, signature TxSignature) (pubKey TxPublicKey, ok bool, err error) {
	ok = true
	pk, err := ed25519.ExtractPublicKey(data, signature[:])
	if err != nil {
		return
	}
	pubKey = TxPublicKeyFromBytes(pk)
	return
}

func (EdPlusSigningScheme) ExtractablePubKey() bool {
	return true
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
