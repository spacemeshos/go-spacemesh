package types

import (
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/signing"
)

// EdSigningScheme is the classic Ed25519 signing scheme value
const EdSigningScheme = 0

// EdSigning is the classic Ed25519 signing scheme
const EdSigning = edSigningObject(EdSigningScheme)

// EdPlusSigningScheme is the extended Ed25519++ signing scheme value
const EdPlusSigningScheme = 1

// EdPlusSigning is the extended Ed25519++ signing scheme
const EdPlusSigning = edPlusSigningObject(EdPlusSigningScheme)

// SigningScheme defines the signing scheme
type SigningScheme interface {
	Sign(signer *signing.EdSigner, data []byte) TxSignature
	Verify(data []byte, pubKey TxPublicKey, signature TxSignature) bool
	Extract(data []byte, signature TxSignature) (TxPublicKey, bool, error)
	ExtractablePubKey() bool
	Identify() int
}

type edSigningObject int

// Sign signs data
func (edSigningObject) Sign(signer *signing.EdSigner, data []byte) TxSignature {
	return TxSignatureFromBytes(signer.Sign1(data))
}

// Verify verifies signature
func (edSigningObject) Verify(data []byte, pubKey TxPublicKey, signature TxSignature) bool {
	return ed25519.Verify(pubKey[:], data, signature[:])
}

// Extract returns public key if it's extractable
func (edSigningObject) Extract(data []byte, signature TxSignature) (TxPublicKey, bool, error) {
	return TxPublicKey{}, false, nil
}

// ExtractablePubKey returns true if public key is extractable
func (edSigningObject) ExtractablePubKey() bool {
	return false
}

// Identify returns scheme's constant value
func (e edSigningObject) Identify() int {
	return int(e)
}

type edPlusSigningObject int

// Sign signs data
func (edPlusSigningObject) Sign(signer *signing.EdSigner, data []byte) TxSignature {
	return TxSignatureFromBytes(signer.Sign2(data))
}

// Verify verifies signature
func (edPlusSigningObject) Verify(data []byte, pubKey TxPublicKey, signature TxSignature) bool {
	return ed25519.Verify2(pubKey[:], data, signature[:])
}

// Extract returns public key if it's extractable
func (edPlusSigningObject) Extract(data []byte, signature TxSignature) (pubKey TxPublicKey, ok bool, err error) {
	ok = true
	pk, err := ed25519.ExtractPublicKey(data, signature[:])
	if err != nil {
		return
	}
	pubKey = TxPublicKeyFromBytes(pk)
	return
}

// ExtractablePubKey returns true if public key is extractable
func (edPlusSigningObject) ExtractablePubKey() bool {
	return true
}

// Identify returns scheme's constant value
func (e edPlusSigningObject) Identify() int {
	return int(e)
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
