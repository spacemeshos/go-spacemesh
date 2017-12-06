package accounts

import (
	"errors"
	"github.com/UnrulyOS/go-unruly/crypto"
)

// Persisted node data
type AccountData struct {
	Id         string          `json:"id"`
	PublicKey  string          `json:"publicKey"`
	CryptoData CryptoData      `json:"crypto"`
	KDParams   crypto.KDParams `json:"kd"`
}

type CryptoData struct {
	Cipher     string `json:"cipher"`
	CipherText string `json:"cipherText"` // encrypted private key
	CipherIv   string `json:"cipherIv"`
	Mac        string `json:"mac"`
}

// Create a new account by id and stored data
// Account will be locked after creation as there's no persisted passphrase
// accountsDataPath: os-specific full path to accounts data folder
func NewAccountFromStore(accountId string, accountsDataPath string) (*Account, error) {
	return nil, nil
}

// Persist all account data to store
// Passphrases are never persisted to store
// accountsDataPath: os-specific full path to accounts data folder
// Returns full path of persisted file (useful for testing)
func (a *Account) Persist(accountsDataPath string) (string, error) {
	if a.IsAccountLocked() {
		return "", errors.New("Can't persist a locked account")
	}

	return "", nil
}
