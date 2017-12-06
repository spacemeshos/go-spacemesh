package accounts

import "errors"

// Persisted node data
type AccountData struct {
	Id         string     `json:"id"`
	PublicKey  string     `json:"publicKey"`
	CryptoData CryptoData `json:"crypto"`
	KDParams   KDParams   `json:"kd"`
}

type CryptoData struct {
	Cipher     string `json:"cipher"`
	CipherText string `json:"cipherText"` // encrypted private key
	CipherIv   string `json:"cipherIv"`
	Mac        string `json:"mac"`
}

type KDParams struct {
	N       int    `json:"n"`
	R       int    `json:"r"`
	P       int    `json:"p"`
	SaltLen int    `json:"saltLen"`
	DKLen   int    `json:"dkLen"`
	Salt    string `json:"salt"`
}

// Create a new account by id and stored data
// Account will be locked after creation as there's no persisted passphrase
func NewAccountFromDataFile(accountId string, dataFilePath string) (*Account, error) {
	return nil, nil
}

// Persist all account data to store
// Passphrases are never persisted to store
func (a *Account) Persist() error {
	if a.IsAccountLocked() {
		return errors.New("Can't persist a locked account")
	}

	return nil
}
