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
	}
}

// Create a new account using provided passphrase
// Clients should persist newly created accounts - without this the account only last for one app session
func NewAccount(passphrase string) (*Account, error) {

	// account crypto data
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

	// extract 16 bytes aes-128-ctr key from the derived key
	aesKey := dk[:16]

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

	// newly created account are unlocked in the current app session
	Accounts.Unlocked[acct.String()] = acct

	return acct, nil
}
