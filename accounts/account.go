package accounts

import (
	crypto "github.com/UnrulyOS/go-unruly/crypto"
	peer "gx/ipfs/QmXYjuNuxVzXKJCfWasQk1RqkhVLDM9jtUKhqc2WPQmFSB/go-libp2p-peer"
	libp2pcrypto "gx/ipfs/QmaPbCnUMBohSGo3KnxEa2bHqyJVVeEEcwtqJAYxerieBo/go-libp2p-crypto"
)

type Account struct {
	peer.ID
	priKey *crypto.PrivateKeylike
	pubKey *crypto.PublicKeylike
}

func NewAccount() (*Account, error) {
	priv, pub, err := crypto.GenerateKeyPair(libp2pcrypto.Secp256k1, 256)

	if err != nil {
		return nil, err
	}

	id, err := pub.IdFromPubKey()
	acct := &Account{id.PeerId(), &priv, &pub}
	return acct, nil
}

func (acct *Account) String() string {
	return peer.IDB58Encode(acct.ID)
}

func (acct *Account) Bytes() []byte {
	// IDs are stored as raw binary data (in an ascii string) so just convert to bytes
	return []byte(acct.ID)
}
