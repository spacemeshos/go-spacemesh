package accounts

import (
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/UnrulyOS/go-unruly/crypto"
	"github.com/UnrulyOS/go-unruly/log"
	"github.com/UnrulyOS/go-unruly/p2p2/keys"
)

func (a *Account) Pretty() string {
	return fmt.Sprintf("Account %s", a.PubKey.Pretty())
}

func (a *Account) String() string {
	return a.PubKey.String()
}

// Log account info
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

	// get derived key from params and pass-phrase
	dk, err := crypto.DeriveKeyFromPassword(passphrase, a.kdParams)
	if err != nil {
		log.Error("kdf failure: %v", err)
		return err
	}

	// extract 16 bytes aes-128-ctr key from the derived key
	aesKey := dk[:16]
	cipherText, err := hex.DecodeString(a.cryptoData.CipherText)
	if err != nil {
		log.Error("Failed to decode cipherText: %v", err)
		return err
	}

	nonce, err := hex.DecodeString(a.cryptoData.CipherIv)
	if err != nil {
		log.Error("Failed to decode iv: %v", err)
		return err
	}

	mac, err := hex.DecodeString(a.cryptoData.Mac)
	if err != nil {
		log.Error("Failed to decode mac: %v", err)
		return err
	}

	// authenticate cipherText using macs
	expectedMac := crypto.Sha256(dk[16:32], cipherText)

	if subtle.ConstantTimeCompare(mac, expectedMac) != 1 {
		return errors.New("mac auth error")
	}

	// aes decrypt private key
	privKeyData, err := crypto.AesCTRXOR(aesKey, cipherText, nonce)
	if err != nil {
		log.Error("Failed to aes decode private key: %v", err)
		return err
	}

	privateKey := keys.NewPrivateKey(privKeyData)

	err = a.validatePublickKey(privateKey)
	if err != nil {
		return err
	}

	// store decrypted key and update accounts
	a.PrivKey = privateKey
	Accounts.Unlocked[a.String()] = a

	return nil
}

// Validate that the account's private key matches provided private key
func (a *Account) validatePublickKey(privateKey keys.PrivateKey) error {
	publicKey := privateKey.GetPublicKey()
	publicKeyStr := publicKey.String()
	accountPubKeyStr := a.PubKey.String()

	if accountPubKeyStr != publicKeyStr {
		return errors.New("Invalid extracted public key %s %s")
	}

	return nil
}
