package accounts

import (
	"crypto/subtle"
	//"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/UnrulyOS/go-unruly/crypto"
	"github.com/UnrulyOS/go-unruly/log"
)

func (a *Account) Pretty() string {
	return fmt.Sprintf("Account %s", a.Identifier.Pretty())
}

func (a *Account) IsAccountLocked() bool {
	return a.PrivKey == nil
}

func (a *Account) IsAccountUnlocked() bool {
	return !a.IsAccountLocked()
}

func (a *Account) LockAccount(passphrase string) {
	a.PrivKey = nil
	delete(Accounts.Unlocked, a.String())
}

// Unlock account using the provided passphrase and account data
func (a *Account) UnlockAccount(passphrase string) error {
	if a.IsAccountUnlocked() {
		// account already unlocked
		return nil
	}

	// get derived key from params and passphrase
	dk, err := crypto.DeriveKeyFromPassword(passphrase, a.kdParams)
	if err != nil {
		log.Error("kdf failure: %v", err)
		return err
	}

	log.Info("Derived dk key: %s", hex.EncodeToString(dk))

	// extract 16 bytes aes-128-ctr key from the derived key
	aesKey := dk[:16]

	log.Info("aes key: %s", hex.EncodeToString(aesKey))

	cipherText, err := hex.DecodeString(a.cryptoData.CipherText)

	log.Info("cipherText: %s", a.cryptoData.CipherText)

	nonce, err := hex.DecodeString(a.cryptoData.CipherIv)

	log.Info("nonce: %s", a.cryptoData.CipherIv)

	// authenticate

	expectedMac := crypto.Sha256(dk[16:32], cipherText)

	mac, err := hex.DecodeString(a.cryptoData.Mac)
	if err != nil {
		return err
	}

	if subtle.ConstantTimeCompare(mac, expectedMac) != 1 {
		log.Info("Mac: %s, ExpectedMac: %s", hex.EncodeToString(mac),
			hex.EncodeToString(expectedMac))
		return errors.New("mac auth error")
	}

	// aes encrypt data
	privKeyData, err := crypto.AesCTRXOR(aesKey, cipherText, nonce)

	if err != nil {
		return err
	}

	privateKey, err := crypto.NewPrivateKey(privKeyData)
	if err != nil {
		return err
	}

	privKeyStr, err := privateKey.String()
	log.Info("Reconstructed private key: %s", privKeyStr)

	publicKey, err := privateKey.GetPublicKey()
	if err != nil {
		return err
	}

	publicKeyStr, err := publicKey.String()
	if err != nil {
		return err
	}

	accountPubKeyStr, err := a.PubKey.String()
	if err != nil {
		return err
	}

	if accountPubKeyStr != publicKeyStr {
		return errors.New("Invalid extracted private key")
	}

	// store decrypted key and update accounts
	a.PrivKey = privateKey

	Accounts.Unlocked[a.String()] = a

	return nil
}
