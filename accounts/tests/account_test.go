package tests

import (
	"github.com/spacemeshos/go-spacemesh/accounts"
	"github.com/spacemeshos/go-spacemesh/assert"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/log"
	"os"
	"testing"
)

func TestAccountLoading(t *testing.T) {
	accounts.LoadAllAccounts()
}

func TestAccountOps(t *testing.T) {

	const passphrase = "a-weak-passphrase-123"
	accountsDataFolder, err := filesystem.GetAccountsDataDirectoryPath()
	if err != nil {
		t.Fatalf("Failed to get temp dir: %v", err)
	}

	account, err := accounts.NewAccount(passphrase)
	if err != nil {
		t.Fatalf("Failed to create an account")
	}

	account.Log()

	assert.True(t, account.IsAccountUnlocked(), "expected account to be unlocked")

	// get os temp dir here
	accountDataFilePath, err := account.Persist(accountsDataFolder)
	if err != nil {
		t.Fatalf("Failed to persist account", err)
	}

	// uncomment to cleanup the account data file from store
	//defer os.Remove(accountDataFilePath)

	log.Info("Persisted account to: %s", accountDataFilePath)

	// read the account back from store
	account1, err := accounts.NewAccountFromStore(account.String(), accountsDataFolder)
	if err != nil {
		t.Fatalf("Failed to load account", err)
	}

	accountPubKey := account.PubKey.String()
	account1PubKey := account1.PubKey.String()

	assert.Equal(t, accountPubKey, account1PubKey, "Expected same public id")
	assert.Equal(t, account.String(), account1.String(), "Expected same id")
	assert.True(t, account1.IsAccountLocked(), "Expected account1 to be locked")

	err = account1.UnlockAccount(passphrase)
	if err != nil {
		t.Fatalf("Failed to unlock account", err)
	}

	assert.True(t, account1.IsAccountUnlocked(), "Expected account to be unlocked")

	// verify private keys are the same
	accountPrivKey := account.PrivKey.String()
	account1PrivKey := account1.PrivKey.String()

	assert.Equal(t, accountPrivKey, account1PrivKey, "Expected same private key after unlocking")
}

func TestPassphrase(t *testing.T) {

	const passphrase = "a-weak-passphrase-1234"

	const wrongPassphrase = "a-weak-passphrase-1245"

	accountsDataFolder, err := filesystem.GetAccountsDataDirectoryPath()
	if err != nil {
		t.Fatalf("Failed to get temp dir: %v", err)
	}

	account, err := accounts.NewAccount(passphrase)
	if err != nil {
		t.Fatalf("Failed to create an account")
	}

	accountDataFilePath, err := account.Persist(accountsDataFolder)
	if err != nil {
		t.Fatalf("Failed to persist account", err)
	}

	// uncomment to cleanup the account data file from store
	defer os.Remove(accountDataFilePath)

	// read the account back from store
	account1, err := accounts.NewAccountFromStore(account.String(), accountsDataFolder)
	if err != nil {
		t.Fatalf("Failed to load account", err)
	}

	accountPubKey := account.PubKey.String()
	account1PubKey := account1.PubKey.String()

	assert.Equal(t, accountPubKey, account1PubKey, "Expected same public id")
	assert.Equal(t, account.String(), account1.String(), "Expected same id")
	assert.True(t, account1.IsAccountLocked(), "Expected account1 to be locked")

	// attempt to unlock
	err = account1.UnlockAccount(wrongPassphrase)

	assert.Err(t, err, "Expected unlock with wrong password op error")
	assert.True(t, account1.IsAccountLocked(), "Expected account to be locked")
	assert.Nil(t, account1.PrivKey, "expected nil private key for locked account")

}
