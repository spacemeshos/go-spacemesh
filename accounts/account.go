package accounts

import (
	"crypto/aes"
	"encoding/hex"
	"errors"
	"github.com/UnrulyOS/go-unruly/crypto"
	"github.com/UnrulyOS/go-unruly/log"
)

type AccountsRegistry struct {
	All      map[string]*Account
	Unlocked map[string]*Account
	Coinbase *Account
}

type Account struct {
	crypto.Identifier
	PrivKey    crypto.PrivateKeylike
	PubKey     crypto.PublicKeylike
	cryptoData CryptoData
	kdParams   crypto.KDParams
}

var (
	Accounts *AccountsRegistry
)

func init() {
	Accounts = &AccountsRegistry{
		make(map[string]*Account),
		make(map[string]*Account),
		nil,
	}
}

func (a *Account) ToLog() {

	pubKey, _ := a.PubKey.String()

	log.Info("Account id: %s", a.Identifier.String())

	if a.PrivKey != nil {
		privKey, _ := a.PrivKey.String()
		log.Info("Private key: %s", privKey)
	}

	log.Info("Public key: %s", pubKey)
	log.Info("IsUnlocked: %t ", a.IsAccountUnlocked())
	log.Info("Crypto params: %+v", a.cryptoData)
	log.Info("kdParams: %+v", a.kdParams)
}

// Load all accounts from store
func LoadAllAccounts(accountsDataPath string) error {

	// todo: go over each file in the accounts folder and create a locked account for each
	// store loaded accounts in accounts.Accounts

	return nil
}

// Create a new account using passphrase
// Clients should persist account in accounts store to save the data - without this store account
// won't be accessible in future app sessions
func NewAccount(passphrase string) (*Account, error) {

	priv, pub, err := crypto.GenerateKeyPair()
	if err != nil {
		return nil, err
	}

	// derive key from passphrase

	id, err := pub.IdFromPubKey()

	kdfParams := crypto.DefaultCypherParams

	// add new salt to params
	saltData, err := crypto.GetRandomBytes(kdfParams.SaltLen)
	if err != nil {
		return nil, errors.New("Failed to generate random salt")
	}
	kdfParams.Salt = hex.EncodeToString(saltData)

	dk, err := crypto.DeriveKeyFromPassword(passphrase, kdfParams)
	if err != nil {
		log.Error("kdf failure: %v", err)
		return nil, err
	}

	log.Info("*** Derived key value (32 bytes) (insecure): %s", hex.EncodeToString(dk))

	// extract 16 bytes aes-128-ctr key from the derived key
	aesKey := dk[:16]

	log.Info("*** AES ency-key (16 bytes): %s", hex.EncodeToString(aesKey))

	// date to encrypt
	privKeyBytes, err := priv.Bytes()
	if err != nil {
		log.Error("Failed to get priv key bytes: %v", err)
		return nil, err
	}

	// compute nonce
	nonce, err := crypto.GetRandomBytes(aes.BlockSize)
	if err != nil {
		return nil, err
	}

	// aes encrypt data
	cipherText, err := crypto.AesCTRXOR(aesKey, privKeyBytes, nonce)

	if err != nil {
		log.Error("Failed to encrypt private key: %v", err)
		return nil, err
	}

	// use last 16 bytes from derived key and cipher text to create a mac
	mac := crypto.Sha256(dk[16:32], cipherText)

	// store aes cipher data
	cryptoData := CryptoData{
		Cipher:     "AES-128-CTR", // 16 bytes key
		CipherText: hex.EncodeToString(cipherText),
		CipherIv:   hex.EncodeToString(nonce),
		Mac:        hex.EncodeToString(mac),
	}

	// store kd data
	kdParams := crypto.KDParams{
		kdfParams.N,
		kdfParams.R,
		kdfParams.P,
		kdfParams.SaltLen,
		kdfParams.DKLen,
		kdfParams.Salt,
	}

	// save all date in newly created account obj
	acct := &Account{id,
		priv,
		pub,
		cryptoData,
		kdParams}

	Accounts.All[acct.String()] = acct
	Accounts.Unlocked[acct.String()] = acct

	return acct, nil
}
