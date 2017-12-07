package accounts

import (
	"encoding/json"
	"errors"
	"github.com/UnrulyOS/go-unruly/crypto"
	"github.com/UnrulyOS/go-unruly/filesystem"
	"github.com/UnrulyOS/go-unruly/log"
	"io/ioutil"
	"path/filepath"
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

	log.Info("Loading persistent node data for account id: %s", accountId)

	fileName := accountId + ".json"
	dataFilePath := filepath.Join(accountsDataPath, fileName)

	data, err := ioutil.ReadFile(dataFilePath)
	if err != nil {
		log.Error("Failed to read node data from file: %v", err)
		return nil, err
	}

	var accountData AccountData
	err = json.Unmarshal(data, &accountData)
	if err != nil {
		log.Error("Failed to unmarshal account data. %v", err)
		return nil, err
	}

	Id, err := crypto.NewIdentifier(accountData.Id)
	if err != nil {
		log.Error("Invalid account id: %v", err)
		return nil, err
	}

	log.Info("Loaded id: %s", Id.String())

	if Id.String() != accountId {
		log.Error("Account ids mismatch")
		return nil, errors.New("Account ids mismtach")
	}

	pubKey, err := crypto.NewPublicKeyFromString(accountData.PublicKey)
	if err != nil {
		log.Error("Invalid account public key: %v", err)
		return nil, err
	}

	log.Info("Account public key data from file: %s", accountData.PublicKey)

	if !pubKey.VerifyId(Id) {
		err := errors.New("Public key and id mismtach")
		log.Error("%v", err)
		return nil, err
	}

	acct := &Account{Id,
		nil,
		pubKey,
		accountData.CryptoData,
		accountData.KDParams}

	Accounts.All[acct.String()] = acct

	return acct, nil
}

// Persist all account data to store
// Passphrases are never persisted to store
// accountsDataPath: os-specific full path to accounts data folder
// Returns full path of persisted file (useful for testing)
func (a *Account) Persist(accountsDataPath string) (string, error) {

	pubKeyStr, err := a.PubKey.String()
	if err != nil {
		log.Error("Invalid public key: %v", err)
		return "", err
	}

	data := &AccountData{
		a.Identifier.String(),
		pubKeyStr,
		a.cryptoData,
		a.kdParams,
	}

	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Error("Failed to marshal node data to json: %v", err)
		return "", err
	}

	fileName := a.Identifier.String() + ".json"
	dataFilePath := filepath.Join(accountsDataPath, fileName)
	err = ioutil.WriteFile(dataFilePath, bytes, filesystem.OwnerReadWrite)

	if err != nil {
		log.Error("Failed to write account to file: %v", err)
		return "", err
	}

	return dataFilePath, nil
}
