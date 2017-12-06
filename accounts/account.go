package accounts

import (
	"fmt"
	crypto "github.com/UnrulyOS/go-unruly/crypto"
)

type Account struct {
	crypto.Identifier
	privKey    crypto.PrivateKeylike
	pubKey     crypto.PublicKeylike
	Passphrase string
}

func NewAccount(passphrase string) (*Account, error) {
	priv, pub, err := crypto.GenerateKeyPair()
	if err != nil {
		return nil, err
	}

	id, err := pub.IdFromPubKey()
	acct := &Account{id, priv, pub, passphrase}

	return acct, nil
}

func (a *Account) Pretty() string {
	return fmt.Sprintf("Account %s", a.Identifier.Pretty())
}
