package accounts

import (
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
)

// Pretty returns an account logging string.
func (a *Account) Pretty() string {
	return fmt.Sprintf("Account %s", a.PubKey.Pretty())
}

// String returns the stringed data of the account public key.
func (a *Account) String() string {
	return a.PubKey.String()
}

// Log account info.
func (a *Account) Log() {

	pubKey := a.PubKey.String()
	log.Info("Account id: %s", a.String())

	if a.PrivKey != nil {
		privKey := a.PrivKey.String()
		log.Info(" Private key: %s", privKey)
	}

	log.Info(" Public key: %s", pubKey)
	log.Info(" IsUnlocked: %t ", a.IsAccountUnlocked())
	log.Info(" Crypto params: %+v", a.cryptoData)
	log.Info(" kdParams: %+v", a.kdParams)
}

// IsAccountLocked returns true if account is locked.
func (a *Account) IsAccountLocked() bool {
	return a.PrivKey == nil
}

// IsAccountUnlocked returns true if account is unlocked.
func (a *Account) IsAccountUnlocked() bool {
	return !a.IsAccountLocked()
}

// LockAccount locks an account with user provided passphrase.
func (a *Account) LockAccount(passphrase string) {
	a.PrivKey = nil
	delete(Accounts.Unlocked, a.String())
}

// UnlockAccount unlocks an account using the user provided passphrase.
func (a *Account) UnlockAccount(passphrase string) error {

	if a.IsAccountUnlocked() {
		// account already unlocked
		return nil
	}

	// get derived key from params and pass-phrase
	dk, err := crypto.DeriveKeyFromPassword(passphrase, a.kdParams)
	if err != nil {
		return err
	}

	// extract 16 bytes aes-128-ctr key from the derived key
	aesKey := dk[:16]
	cipherText, err := hex.DecodeString(a.cryptoData.CipherText)
	if err != nil {
		return err
	}

	nonce, err := hex.DecodeString(a.cryptoData.CipherIv)
	if err != nil {
		return err
	}

	mac, err := hex.DecodeString(a.cryptoData.Mac)
	if err != nil {
		return err
	}

	// authenticate cipherText using mac
	expectedMac := crypto.Sha256(dk[16:32], cipherText)

	if subtle.ConstantTimeCompare(mac, expectedMac) != 1 {
		return errors.New("mac auth error")
	}

	// aes decrypt private key
	privKeyData, err := crypto.AesCTRXOR(aesKey, cipherText, nonce)
	if err != nil {
		log.Error("failed to aes decode private key", err)
		return err
	}

	privateKey, err := crypto.NewPrivateKey(privKeyData)
	if err != nil {
		return err
	}

	err = a.validatePublicKey(privateKey)
	if err != nil {
		return err
	}

	// store decrypted key and update accounts
	a.PrivKey = privateKey
	Accounts.Unlocked[a.String()] = a

	return nil
}

// ValidatePublicKey that the account's private key matches the provided private key.
func (a *Account) validatePublicKey(privateKey crypto.PrivateKey) error {

	publicKey := privateKey.GetPublicKey()
	publicKeyStr := publicKey.String()
	accountPubKeyStr := a.PubKey.String()

	if accountPubKeyStr != publicKeyStr {
		return fmt.Errorf("invalid extracted public key %s %s", accountPubKeyStr, publicKeyStr)
	}

	return nil
}
