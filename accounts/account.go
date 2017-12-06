package accounts

import (
	"crypto/aes"
	"encoding/hex"
	"github.com/UnrulyOS/go-unruly/crypto"
	"github.com/UnrulyOS/go-unruly/log"
)

type Account struct {
	crypto.Identifier
	privKey  crypto.PrivateKeylike
	pubKey   crypto.PublicKeylike
	crypto   Crypto
	kdParams KDParams
}

func NewAccount(passPhrase string) (*Account, error) {

	priv, pub, err := crypto.GenerateKeyPair()
	if err != nil {
		return nil, err
	}

	id, err := pub.IdFromPubKey()
	kdfParams := crypto.DefaultCypherParams

	kdfData, err := crypto.DeriveKeyFromPassword(passPhrase, kdfParams)
	if err != nil {
		log.Error("kdf failure: %v", err)
		return nil, err
	}

	aesKey := kdfData.DK[:16]
	privKeyBytes, err := priv.Bytes()
	if err != nil {
		return nil, err
	}

	nonce, err := crypto.GetRandomBytes(aes.BlockSize)
	if err != nil {
		return nil, err
	}

	cipherText, err := crypto.AesCTREncrypt(aesKey, privKeyBytes, nonce)
	if err != nil {
		return nil, err
	}

	mac := crypto.Sha256(kdfData.DK[16:32], cipherText)

	cryptoData := Crypto{
		Cipher:     "AES-128", // 16 bytes key
		CipherText: hex.EncodeToString(cipherText),
		CipherIv:   hex.EncodeToString(nonce),
		Mac:        hex.EncodeToString(mac),
	}

	kdParams := KDParams{
		kdfParams.N,
		kdfParams.R,
		kdfParams.P,
		kdfParams.SaltLen,
		kdfParams.DKLen,
		hex.EncodeToString(kdfData.Salt),
	}

	acct := &Account{id,
		priv,
		pub,
		cryptoData,
		kdParams}

	// persist account data to store
	err = acct.Persist()
	if err != nil {
		return nil, err
	}

	return acct, nil
}



