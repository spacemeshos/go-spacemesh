package accounts

import (
	"fmt"
)

func (a *Account) Pretty() string {
	return fmt.Sprintf("Account %s", a.Identifier.Pretty())
}

func (a *Account) IsAccountLocked() bool {
	return a.privKey == nil
}

func (a *Account) IsAccountUnlocked() bool {
	return !a.IsAccountLocked()
}

func (a *Account) LockAccount(passPhrase string) {
	a.privKey = nil
}

func (a *Account) UnlockAccount(passPhrase string) error {

	if a.IsAccountUnlocked() {
		return nil
	}

	// unlock account using the provided passphrase and account data

	// after unlocking privKey != nil

	return nil
}