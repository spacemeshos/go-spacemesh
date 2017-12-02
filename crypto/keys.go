package crypto

import (
	b58 "gx/ipfs/QmT8rehPR3F6bmwL6zjUN8XpiDBFFpMP2myPdC6ApsWfJf/go-base58"
	peer "gx/ipfs/QmXYjuNuxVzXKJCfWasQk1RqkhVLDM9jtUKhqc2WPQmFSB/go-libp2p-peer"
	crypto "gx/ipfs/QmaPbCnUMBohSGo3KnxEa2bHqyJVVeEEcwtqJAYxerieBo/go-libp2p-crypto"
)

// Fundamental common data types
// Wrap as many 3rd party types as possible with our own extendable types such as PublicKey, PrivateKey, NodeId, etc...
type Key struct {
	crypto.Key
}

func GenerateKeyPair(typ, bits int) (PrivateKey, PublicKey, error) {
	priv, pub, err := crypto.GenerateKeyPair(typ, bits)
	return PrivateKey{priv}, PublicKey{pub}, err
}

type PublicKey struct {
	crypto.PubKey
}

func (pubKey *PublicKey) Bytes() ([]byte, error) {
	return pubKey.PubKey.Bytes()
}

func NewPublicKey(data []byte) (*PublicKey, error) {
	key, err := crypto.UnmarshalPublicKey(data)
	return &PublicKey{(key)}, err
}

func (pubKey *PublicKey) String() (string, error) {
	bytes, err := pubKey.Bytes()
	if err != nil {
		return "", err
	}

	return b58.Encode(bytes), nil
}

// create an Id which is derived from a public key
// used for both accounts and nodes
func (pubKey *PublicKey) IdFromPubKey() (Id, error) {
	id, err := peer.IDFromPublicKey(pubKey)
	return Id{id}, err
}

type PrivateKey struct {
	crypto.PrivKey
}

func (privKey *PrivateKey) Bytes() ([]byte, error) {
	return privKey.PrivKey.Bytes()
}

func (privKey *PrivateKey) String() (string, error) {
	bytes, err := privKey.Bytes()
	if err != nil {
		return "", err
	}

	return b58.Encode(bytes), nil
}

func NewPrivateKey(data []byte) (*PrivateKey, error) {
	key, err := crypto.UnmarshalPrivateKey(data)
	return &PrivateKey{(key)}, err
}
