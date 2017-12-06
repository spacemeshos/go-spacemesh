package accounts

import (
	"crypto/aes"
	"encoding/hex"
	"github.com/UnrulyOS/go-unruly/crypto"
	"github.com/UnrulyOS/go-unruly/log"
)

type Account struct {
	crypto.Identifier
	privKey    crypto.PrivateKeylike
	pubKey     crypto.PublicKeylike
	cryptoData CryptoData
	kdParams   KDParams
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
	kdfData, err := crypto.DeriveKeyFromPassword(passphrase, kdfParams)
	if err != nil {
		log.Error("kdf failure: %v", err)
		return nil, err
	}

	// extract 16 bytes aes-128-ctr key from the derived key
	aesKey := kdfData.DK[:16]

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
	cipherText, err := crypto.AesCTREncrypt(aesKey, privKeyBytes, nonce)
	if err != nil {
		return nil, err
	}

	// use last 16 bytes from derived key and cipher text to create a mac
	mac := crypto.Sha256(kdfData.DK[16:32], cipherText)

	// store aes cipher data
	cryptoData := CryptoData{
		Cipher:     "AES-128-CTR", // 16 bytes key
		CipherText: hex.EncodeToString(cipherText),
		CipherIv:   hex.EncodeToString(nonce),
		Mac:        hex.EncodeToString(mac),
	}

	// store kd data
	kdParams := KDParams{
		kdfParams.N,
		kdfParams.R,
		kdfParams.P,
		kdfParams.SaltLen,
		kdfParams.DKLen,
		hex.EncodeToString(kdfData.Salt),
	}

	// save all date in newly created account obj
	acct := &Account{id,
		priv,
		pub,
		cryptoData,
		kdParams}

	return acct, nil
}
