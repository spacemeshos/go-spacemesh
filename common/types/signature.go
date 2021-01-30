package types

import (
	"crypto/sha512"
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/signing"
	"hash"
)

// Signer is an alias to Ed signer
type Signer = *signing.EdSigner

// SigningScheme is an signing interface using to sign/verify transactions
type SigningScheme struct{ *SigningSchemeObject }

// TransactionHasher is the hash using in signing to calculate digest of transaction
type TransactionHasher struct{ hash.Hash }

// TransactionDigest is the transaction digest
type TransactionDigest [sha512.Size]byte

// Sum finishes digest calculation
func (h TransactionHasher) Sum() (digest TransactionDigest) {
	copy(digest[:], h.Hash.Sum(nil))
	return
}

// NewTransactionHasher creates new Hasher
func NewTransactionHasher() TransactionHasher {
	return TransactionHasher{sha512.New()}
}

// EdSigningScheme is an classic Ed25519 scheme
var EdSigningScheme = SigningSchemeObject{
	Value:        0,
	Extractable:  false,
	PubKeyLength: ed25519.PublicKeySize,

	Sign: func(signer Signer, data []byte) Signature {
		return SignatureFromBytes(signer.Sign1(data))
	},

	Verify: func(digest TransactionDigest, pubKey PublicKey, signature Signature) bool {
		return ed25519.Verify(pubKey.k[:], digest[:], signature[:])
	},

	ExtractPubKey: func(digest TransactionDigest, signature Signature) (PublicKey, bool, error) {
		return PublicKey{}, false, nil
	},
}.New()

// EdPlusSigningScheme is an classic Ed25519++ scheme
var EdPlusSigningScheme = SigningSchemeObject{
	Value:        1,
	Extractable:  true,
	PubKeyLength: ed25519.PublicKeySize,

	Sign: func(signer Signer, data []byte) Signature {
		return SignatureFromBytes(signer.Sign2(data))
	},

	Verify: func(digest TransactionDigest, pubKey PublicKey, signature Signature) bool {
		return ed25519.Verify2(pubKey.k[:], digest[:], signature[:])
	},

	ExtractPubKey: func(digest TransactionDigest, signature Signature) (pubKey PublicKey, ok bool, err error) {
		ok = true
		pk, err := ed25519.ExtractPublicKey(digest[:], signature[:])
		if err != nil {
			return
		}
		pubKey = PublicKeyFromBytes(pk)
		return
	},
}.New()

// SigningSchemeObject is an backand signing scheme object
type SigningSchemeObject struct {
	Value         int
	Extractable   bool
	PubKeyLength  int
	Sign          func(signer Signer, data []byte) Signature
	Verify        func(digest TransactionDigest, pubKey PublicKey, signature Signature) bool
	ExtractPubKey func(digest TransactionDigest, signature Signature) (PublicKey, bool, error)
}

// New creates new signing scheme
func (sso SigningSchemeObject) New() SigningScheme {
	return SigningScheme{&sso}
}

// SignatureLength defines signature length
// TODO: signing does not have signature length constant
const SignatureLength = ed25519.SignatureSize

// Signature is a signature of a transaction
type Signature [SignatureLength]byte

// SignatureFromBytes transforms bytes to the signature
func SignatureFromBytes(bs []byte) (sig Signature) {
	copy(sig[:], bs)
	return
}

// Bytes returns signature's bytes
func (sig Signature) Bytes() []byte {
	return sig[:]
}

// PublicKeyLength defines public key length
// TODO: signing does not have public key length constant
const PublicKeyLength = ed25519.PublicKeySize

// PublicKey is a public key of a transaction
type PublicKey struct {
	k [PublicKeyLength]byte
}

// PublicKeyFromBytes converts bytes to the public key
func PublicKeyFromBytes(bs []byte) (pk PublicKey) {
	copy(pk.k[:], bs)
	return
}

// Bytes returns public key's bytes
func (pk PublicKey) Bytes() []byte {
	return pk.k[:]
}
