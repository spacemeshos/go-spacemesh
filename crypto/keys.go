package crypto

import (
	"fmt"
	b58 "gx/ipfs/QmT8rehPR3F6bmwL6zjUN8XpiDBFFpMP2myPdC6ApsWfJf/go-base58"
	peer "gx/ipfs/QmXYjuNuxVzXKJCfWasQk1RqkhVLDM9jtUKhqc2WPQmFSB/go-libp2p-peer"
	libp2pcrypto "gx/ipfs/QmaPbCnUMBohSGo3KnxEa2bHqyJVVeEEcwtqJAYxerieBo/go-libp2p-crypto"
)

// todo: is Keyer better?
type Keylike interface {
	String() (string, error)
	Bytes() ([]byte, error)
	Pretty() string
}

// todo: is there a better name for this interface? PrivateKeyer?
type PrivateKeylike interface {
	Keylike
	PrivatePeerKey() libp2pcrypto.PrivKey
	GetPublicKey() (PublicKeylike, error)
}

// todo: find a better name for this interface
type PublicKeylike interface {
	Keylike
	IdFromPubKey() (Identifier, error)
	Verify(data []byte, sig []byte) (bool, error)
	PublicPeerKey() libp2pcrypto.PubKey
}

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

func NewPublicKeyFromString(s string) (PublicKeylike, error) {
	data := b58.Decode(s)
	return NewPublicKey(data)
}

func (p PublicKey) Bytes() ([]byte, error) {
	return p.PubKey.Bytes()
}

func (p PublicKey) String() (string, error) {
	bytes, err := p.Bytes()
	if err != nil {
		return "", err
	}

	return b58.Encode(bytes), nil
}

func (p PublicKey) Pretty() string {

	pstr, err := p.String()
	if err != nil {
		return fmt.Sprintf("Invalid public key: %v", err)
	}

	maxRunes := 6
	if len(pstr) < maxRunes {
		maxRunes = len(pstr)
	}

	return fmt.Sprintf("<PubKey %s>", pstr[:maxRunes])
}

// create an Id which is derived from a public key
// used for both accounts and nodes
func (p PublicKey) IdFromPubKey() (Identifier, error) {
	id, err := peer.IDFromPublicKey(p)
	return &Id{id}, err
}

func (p PublicKey) PublicPeerKey() libp2pcrypto.PubKey {
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

func NewPrivateKeyFromString(s string) (PrivateKeylike, error) {
	data := b58.Decode(s)
	return NewPrivateKey(data)
}

func (p PrivateKey) Bytes() ([]byte, error) {
	return p.PrivKey.Bytes()
}

func (p PrivateKey) String() (string, error) {
	bytes, err := p.Bytes()
	if err != nil {
		return "", err
	}

	return b58.Encode(bytes), nil
}

func (p PrivateKey) GetPublicKey() (PublicKeylike, error) {
	pubFromPrivData, _ := p.PrivatePeerKey().GetPublic().Bytes()
	pubFromPriv, err := NewPublicKey(pubFromPrivData)
	return pubFromPriv, err
}

func (p PrivateKey) PrivatePeerKey() libp2pcrypto.PrivKey {
	return p.PrivKey
}

func (p PrivateKey) Pretty() string {

	pstr, err := p.String()
	if err != nil {
		return fmt.Sprintf("Invalid private key: %v", err)
	}

	maxRunes := 6
	if len(pstr) < maxRunes {
		maxRunes = len(pstr)
	}

	return fmt.Sprintf("<PrivKey %s>", pstr[:maxRunes])
}
