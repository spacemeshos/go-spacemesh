package common

import (
	crypto "gx/ipfs/QmaPbCnUMBohSGo3KnxEa2bHqyJVVeEEcwtqJAYxerieBo/go-libp2p-crypto"
	peer "gx/ipfs/QmXYjuNuxVzXKJCfWasQk1RqkhVLDM9jtUKhqc2WPQmFSB/go-libp2p-peer"
	b58 "gx/ipfs/QmT8rehPR3F6bmwL6zjUN8XpiDBFFpMP2myPdC6ApsWfJf/go-base58"
)

// Fundamental common data types
// Wrap as many 3rd party types as possible with our own extendable types such as PublicKey, PrivateKey, NodeId, etc...
type Key struct {
	crypto.Key
}

func GenerateKeyPair(typ, bits int) (PrivateKey, PublicKey, error) {
	priv, pub, err := crypto.GenerateKeyPair(typ, bits)
	return PrivateKey{priv}, PublicKey{ pub}, err
}

type PublicKey struct {
	crypto.PubKey
}

func (pubKey *PublicKey) Bytes () ([]byte, error) {
	return pubKey.PubKey.Bytes()
}

func NewPublicKey(data []byte) (*PublicKey, error) {
	key, err := crypto.UnmarshalPublicKey(data)
	return &PublicKey{(key)}, err
}

func (pubKey *PublicKey) String () (string, error) {
	bytes, err := pubKey.Bytes()
	if err != nil {
		return "", err
	}

	return b58.Encode(bytes), nil
}

func (pubKey *PublicKey) IdFromPubKey () (NodeId, error) {
	id, err := peer.IDFromPublicKey(pubKey)
	return NodeId{id},err
}

type PrivateKey struct {
	crypto.PrivKey
}

func (privKey *PrivateKey) Bytes () ([]byte, error) {
	return privKey.PrivKey.Bytes()
}

func (privKey *PrivateKey) String () (string, error) {
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

type NodeId struct {
	peer.ID
}

func (nodeId *NodeId) String () (string) {
	return peer.IDB58Encode(nodeId.ID)
}

func NewNodeId(b58 string) (*NodeId, error) {
	id, err := peer.IDB58Decode(b58)
	return &NodeId{id}, err
}


