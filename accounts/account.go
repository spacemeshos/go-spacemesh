// Package account provide types for working with blockchain accounts
package accounts

import (
	"crypto/aes"
	"encoding/hex"
	"errors"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
)

type AccountsRegistry struct {
	All      map[string]*Account
	Unlocked map[string]*Account
}

type Account struct {
	PrivKey    crypto.PrivateKey
	PubKey     crypto.PublicKey
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

// Creates a new account using the provided passphrase
// Clients should persist newly created accounts - without this the account only lasts for one app session
func NewAccount(passphrase string) (*Account, error) {

	// account crypto data
	priv, pub, err := crypto.GenerateKeyPair()
	if err != nil {
		return nil, err
	}

	// derive key from passphrase
	kdfParams := crypto.DefaultCypherParams

	// add new salt to params
	saltData, err := crypto.GetRandomBytes(kdfParams.SaltLen)
	if err != nil {
		return nil, errors.New("Failed to generate random salt")
	}
	kdfParams.Salt = hex.EncodeToString(saltData)

	dk, err := crypto.DeriveKeyFromPassword(passphrase, kdfParams)
	if err != nil {
		return nil, err
	}

	// extract 16 bytes aes-128-ctr key from the derived key
	aesKey := dk[:16]

	// data to encrypt
	privKeyBytes := priv.Bytes()

	// compute nonce
	nonce, err := crypto.GetRandomBytes(aes.BlockSize)
	if err != nil {
		return nil, err
	}

	// aes encrypt data
	cipherText, err := crypto.AesCTRXOR(aesKey, privKeyBytes, nonce)

	if err != nil {
		log.Error("Failed to encrypt private key", err)
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
		N:       kdfParams.N,
		R:       kdfParams.R,
		P:       kdfParams.P,
		SaltLen: kdfParams.SaltLen,
		DKLen:   kdfParams.DKLen,
		Salt:    kdfParams.Salt,
	}

	// save all data in newly created account obj
	acct := &Account{priv,
		pub,
		cryptoData,
		kdParams}

	Accounts.All[acct.String()] = acct

	// newly created account are unlocked in the current app session
	Accounts.Unlocked[acct.String()] = acct

	return acct, nil
}
