package signing

import (
	"errors"
	"github.com/btcsuite/btcutil/base58"
	"github.com/spacemeshos/ed25519"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/rand"
)

type PublicKey struct {
	pub []byte
}

func NewPublicKey(pub []byte) *PublicKey {
	return &PublicKey{pub}
}

func (p *PublicKey) Bytes() []byte {
	return p.pub
}

func (p *PublicKey) String() string {
	return base58.Encode(p.Bytes())
}

type EdSigner struct {
	privKey ed25519.PrivateKey // the pub & private key
	pubKey  ed25519.PublicKey  // only the pub key part
}

func NewEdSignerFromBuffer(buff []byte) (*EdSigner, error) {
	if len(buff) < 32 {
		log.Error("Could not create EdSigner from the provided buffer: buffer too small")
		return nil, errors.New("buffer too small")
	}

	sgn := &EdSigner{privKey: buff, pubKey: buff[:32]}
	m := make([]byte, 4)
	rand.Read(m)
	sig := ed25519.Sign2(sgn.privKey, m)
	if !ed25519.Verify2(sgn.pubKey, m, sig) {
		log.Error("Public key and private key does not match. Could not verify the signed message")
		return nil, errors.New("private and public does not match")
	}

	return sgn, nil
}

func NewEdSigner() *EdSigner {
	pub, priv, err := ed25519.GenerateKey(nil)

	if err != nil {
		log.Panic("Could not generate key pair err=%v", err)
	}

	return &EdSigner{privKey: priv, pubKey: pub}
}

func (es *EdSigner) Sign(m []byte) []byte {
	return ed25519.Sign2(es.privKey, m)
}

func (es *EdSigner) PublicKey() *PublicKey {
	return NewPublicKey(es.pubKey)
}

func (es *EdSigner) ToBuffer() []byte {
	buff := make([]byte, len(es.privKey))
	copy(buff, es.privKey)

	return buff
}
