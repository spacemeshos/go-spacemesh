package types

import (
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/signing"
)

// TODO: signing does not have signature length constant
const TxSignatureLength = ed25519.SignatureSize

type TxSignature [TxSignatureLength]byte

func TxSignatureFromBytes(bs []byte) (sig TxSignature) {
	copy(sig[:], bs)
	return
}

func (sig TxSignature) Bytes() []byte {
	return sig[:]
}

func (sig TxSignature) Verify(pubKey ed25519.PublicKey, data []byte) bool {
	return signing.Verify(signing.NewPublicKey(pubKey), data, sig[:])
}

// TODO: signing does not have public key length constant
const TxPublicKeyLength = ed25519.PublicKeySize

type TxPublicKey [TxPublicKeyLength]byte

func TxPublicKeyFromBytes(bs []byte) (pk TxPublicKey) {
	copy(pk[:], bs)
	return
}

func (pk TxPublicKey) Bytes() []byte {
	return pk[:]
}
