package tests

import (
	"github.com/UnrulyOS/go-unruly/accounts"
	"github.com/UnrulyOS/go-unruly/assert"
	"github.com/UnrulyOS/go-unruly/filesystem"
	"github.com/UnrulyOS/go-unruly/log"
	"testing"
)

func TestAccountCreation(t *testing.T) {

	const passphrase = "a-weak-passphrase123"

	accountsDataFolder, err := filesystem.GetAccountsDataDirectoryPath()

	if err != nil {
		t.Fatalf("Failed to get temp dir: %v", err)
	}

	account, err := accounts.NewAccount(passphrase)
	if err != nil {
		t.Fatalf("Failed to create an account")
	}

	assert.True(t, account.IsAccountUnlocked(), "expected account to be unlocked")

	// get os temp dir here
	accountDataFilePath, err := account.Persist(accountsDataFolder)
	if err != nil {
		t.Fatalf("Failed to persist account", err)
	}

	log.Info("Persisted account to: %s", accountDataFilePath)
	//defer os.Remove(accountDataFile)

	account.ToLog()

	// read the account back from store
	account1, err := accounts.NewAccountFromStore(account.String(), accountsDataFolder)
	if err != nil {
		t.Fatalf("Failed to load account", err)
	}

	account1.ToLog()

	assert.Equal(t, account.String(), account1.String(), "Expected same id")
	assert.True(t, account1.IsAccountLocked(), "Expected locked")

	err = account1.UnlockAccount(passphrase)
	if err != nil {
		t.Fatalf("Failed to unlock account", err)
	}

	assert.True(t, account1.IsAccountUnlocked(), "Expected unlocked")

	// verify private keys are the same
	accountPrivKey, err := account.PrivKey.String()
	assert.Nil(t, err, "expected nil error")

	account1PrivKe, err := account1.PrivKey.String()
	assert.Nil(t, err, "expected nil error")

	assert.Equal(t, accountPrivKey, account1PrivKe, "expected same private key")

}
