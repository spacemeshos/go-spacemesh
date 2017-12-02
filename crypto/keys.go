package crypto

import (
	b58 "gx/ipfs/QmT8rehPR3F6bmwL6zjUN8XpiDBFFpMP2myPdC6ApsWfJf/go-base58"
	peer "gx/ipfs/QmXYjuNuxVzXKJCfWasQk1RqkhVLDM9jtUKhqc2WPQmFSB/go-libp2p-peer"
	libp2pcrypto "gx/ipfs/QmaPbCnUMBohSGo3KnxEa2bHqyJVVeEEcwtqJAYxerieBo/go-libp2p-crypto"
)

type Keylike interface {
	String() (string, error)
	Bytes() ([]byte, error)
}

type PrivateKeylike interface {
	Keylike
	PrivatePeerKey() libp2pcrypto.PrivKey
}

type PublicKeylike interface {
	Keylike
	IdFromPubKey() (Identifiable, error)
	Verify(data []byte, sig []byte) (bool, error)
	PublicPeerKey() libp2pcrypto.PubKey
}


// Fundamental common data types
// Wrap as many 3rd party types as possible with our own extendable types such as PublicKey, PrivateKey, NodeId, etc...
type Key struct {
	libp2pcrypto.Key
}

func GenerateKeyPair(typ, bits int) (PrivateKeylike, PublicKeylike, error) {
	priv, pub, err := libp2pcrypto.GenerateKeyPair(typ, bits)
	return &PrivateKey{priv}, &PublicKey{pub}, err
}

type PublicKey struct {
	libp2pcrypto.PubKey
}

func NewPublicKey(data []byte) (PublicKeylike, error) {
	key, err := libp2pcrypto.UnmarshalPublicKey(data)
	return &PublicKey{(key)}, err
}


func (p *PublicKey) Bytes() ([]byte, error) {
	return p.PubKey.Bytes()
}

func (p *PublicKey) String() (string, error) {
	bytes, err := p.Bytes()
	if err != nil {
		return "", err
	}

	return b58.Encode(bytes), nil
}

// create an Id which is derived from a public key
// used for both accounts and nodes
func (p *PublicKey) IdFromPubKey() (Identifiable, error) {
	id, err := peer.IDFromPublicKey(p)
	return &Id{id}, err
}

func (p *PublicKey) PublicPeerKey() (libp2pcrypto.PubKey) {
	return p.PubKey
}

///////////////////

type PrivateKey struct {
	libp2pcrypto.PrivKey
}

func NewPrivateKey(data []byte) (PrivateKeylike, error) {
	key, err := libp2pcrypto.UnmarshalPrivateKey(data)
	return &PrivateKey{(key)}, err
}

func (p *PrivateKey) Bytes() ([]byte, error) {
	return p.PrivKey.Bytes()
}

func (p *PrivateKey) String() (string, error) {
	bytes, err := p.Bytes()
	if err != nil {
		return "", err
	}

	return b58.Encode(bytes), nil
}

func (p *PrivateKey) PrivatePeerKey() (libp2pcrypto.PrivKey) {
	return p.PrivKey
}

