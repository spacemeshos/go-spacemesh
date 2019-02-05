package accounts

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/log"
)

// AccountData is used to persist an account.
type AccountData struct {
	PublicKey  string          `json:"publicKey"`
	CryptoData CryptoData      `json:"crypto"`
	KDParams   crypto.KDParams `json:"kd"`
	NetworkID  int8            `json:"networkId"`
}

// CryptoData is the data use to encrypt/decrypt locally stored account data.
type CryptoData struct {
	Cipher     string `json:"cipher"`
	CipherText string `json:"cipherText"` // encrypted private key
	CipherIv   string `json:"cipherIv"`
	Mac        string `json:"mac"`
}

// LoadAllAccounts loads all account persisted to store
func LoadAllAccounts() error {

	accountsDataFolder, err := filesystem.GetAccountsDataDirectoryPath()
	if err != nil {
		return err
	}

	files, err := ioutil.ReadDir(accountsDataFolder)
	if err != nil {
		log.Error("Failed to read account directory files", err)
		return nil
	}

	for _, f := range files {
		fileName := f.Name()
		if !f.IsDir() && strings.HasSuffix(fileName, ".json") {
			accountID := fileName[:strings.LastIndex(fileName, ".")]
			_, err := NewAccountFromStore(accountID, accountsDataFolder)
			if err != nil {
				log.Error(fmt.Sprintf("failed to load account %s", accountID), err)
			}
		}
	}

	return nil

}

// NewAccountFromStore creates a new account by id and stored data.
// Account will be locked after creation as there's no persisted passphrase.
// accountsDataPath: os-specific full path to accounts data folder.
func NewAccountFromStore(accountID string, accountsDataPath string) (*Account, error) {

	log.Debug("Loading account from store. ID: %s ...", accountID)

	fileName := accountID + ".json"
	dataFilePath := filepath.Join(accountsDataPath, fileName)

	data, err := ioutil.ReadFile(dataFilePath)
	if err != nil {
		return nil, err
	}

	var accountData AccountData
	err = json.Unmarshal(data, &accountData)
	if err != nil {
		return nil, err
	}

	pubKey, err := crypto.NewPublicKeyFromString(accountData.PublicKey)
	if err != nil {
		return nil, err
	}

	acct := &Account{nil,
		pubKey,
		accountData.CryptoData,
		accountData.KDParams,
		accountData.NetworkID}

	log.Debug("Loaded account from store: %s", pubKey.String())

	Accounts.All[acct.String()] = acct

	return acct, nil
}

// Persist all account data to store
// Passphrases are never persisted to store
// accountsDataPath: os-specific full path to accounts data folder
// Returns full path of persisted file (useful for testing)
func (a *Account) Persist(accountsDataPath string) (string, error) {

	pubKeyStr := a.PubKey.String()

	data := &AccountData{
		pubKeyStr,
		a.cryptoData,
		a.kdParams,
		a.NetworkID,
	}

	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Error("Failed to marshal node data to json", err)
		return "", err
	}

	fileName := a.String() + ".json"
	dataFilePath := filepath.Join(accountsDataPath, fileName)
	err = ioutil.WriteFile(dataFilePath, bytes, filesystem.OwnerReadWrite)
	if err != nil {
		log.Error("Failed to write account to file", err)
		return "", err
	}

	log.Debug("Persisted account to store. ID: %s", a.String())

	return dataFilePath, nil
}
