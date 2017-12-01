package common

import (
	crypto "gx/ipfs/QmaPbCnUMBohSGo3KnxEa2bHqyJVVeEEcwtqJAYxerieBo/go-libp2p-crypto"
	peer "gx/ipfs/QmXYjuNuxVzXKJCfWasQk1RqkhVLDM9jtUKhqc2WPQmFSB/go-libp2p-peer"

)

type Account struct {
	peer.ID
	priKey *PrivateKey
	pubKey *PublicKey
}

func NewAccount () (*Account, error) {

	priv, pub, err := GenerateKeyPair(crypto.Secp256k1, 256)

	if err != nil {
		return nil, err
	}

	id, err := pub.IdFromPubKey()
	acct := &Account{id.ID, &priv, &pub}
	return acct, nil
}

func (acct *Account) String () (string) {
	return peer.IDB58Encode(acct.ID)
}


func (acct *Account) Bytes () []byte {
// IDs are stored as raw binary data (in an ascii string) so just convert to bytes
return []byte (acct.ID)
}