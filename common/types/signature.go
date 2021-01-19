package types

import (
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/signing"
)

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

// Verify does signature verification
func (sig TxSignature) Verify(pubKey ed25519.PublicKey, data []byte) bool {
	return signing.Verify(signing.NewPublicKey(pubKey), data, sig[:])
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
