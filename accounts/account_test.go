package accounts

import (
	"os"
	"testing"

	"github.com/spacemeshos/go-spacemesh/filesystem"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/stretchr/testify/assert"
)

func TestAccountLoading(t *testing.T) {
	LoadAllAccounts()
}

func TestAccountOps(t *testing.T) {

	filesystem.SetupTestSpacemeshDataFolders(t, "account_ops")

	const passphrase = "a-weak-passphrase-123"
	accountsDataFolder, err := filesystem.GetAccountsDataDirectoryPath()
	if err != nil {
		t.Fatalf("Failed to get temp dir: %v", err)
	}

	account, err := NewAccount(passphrase)
	if err != nil {
		t.Fatalf("Failed to create an account")
	}

	account.Log()

	assert.True(t, account.IsAccountUnlocked(), "expected account to be unlocked")

	// get os temp dir here
	accountDataFilePath, err := account.Persist(accountsDataFolder)
	if err != nil {
		t.Fatalf("Failed to persist account %v", err)
	}

	// uncomment to cleanup the account data file from store
	//defer os.Remove(accountDataFilePath)

	log.Debug("Persisted account to: %s", accountDataFilePath)

	// read the account back from store
	account1, err := NewAccountFromStore(account.String(), accountsDataFolder)
	if err != nil {
		t.Fatalf("Failed to load account %v", err)
	}

	accountPubKey := account.PubKey.String()
	account1PubKey := account1.PubKey.String()

	assert.Equal(t, accountPubKey, account1PubKey, "Expected same public id")
	assert.Equal(t, account.String(), account1.String(), "Expected same id")
	assert.True(t, account1.IsAccountLocked(), "Expected account1 to be locked")

	err = account1.UnlockAccount(passphrase)
	if err != nil {
		t.Fatalf("Failed to unlock account %v", err)
	}

	assert.True(t, account1.IsAccountUnlocked(), "Expected account to be unlocked")

	// verify private keys are the same
	accountPrivKey := account.PrivKey.String()
	account1PrivKey := account1.PrivKey.String()

	assert.Equal(t, accountPrivKey, account1PrivKey, "Expected same private key after unlocking")

	filesystem.DeleteSpacemeshDataFolders(t)

}

func TestPersistence(t *testing.T) {

	filesystem.SetupTestSpacemeshDataFolders(t, "account_persistence")

	const passphrase = "a-weak-passphrase-1234"
	const wrongPassphrase = "a-weak-passphrase-1245"

	accountsDataFolder, err := filesystem.GetAccountsDataDirectoryPath()
	if err != nil {
		t.Fatalf("Failed to get temp dir: %v", err)
	}

	account, err := NewAccount(passphrase)
	if err != nil {
		t.Fatalf("Failed to create an account")
	}

	accountDataFilePath, err := account.Persist(accountsDataFolder)
	if err != nil {
		t.Fatalf("Failed to persist account %v", err)
	}

	// cleanup the account data file from store
	defer os.Remove(accountDataFilePath)

	// read the account back from store
	account1, err := NewAccountFromStore(account.String(), accountsDataFolder)
	if err != nil {
		t.Fatalf("Failed to load account %v", err)
	}

	accountPubKey := account.PubKey.String()
	account1PubKey := account1.PubKey.String()

	accountNetworkID := account.NetworkID
	account1NetworkID := account1.NetworkID

	assert.Equal(t, accountPubKey, account1PubKey, "Expected same public id")
	assert.Equal(t, account.String(), account1.String(), "Expected same id")
	assert.Equal(t, accountNetworkID, account1NetworkID, "Expected same network id")
	assert.True(t, account1.IsAccountLocked(), "Expected account1 to be locked")

	// attempt to unlock
	err = account1.UnlockAccount(wrongPassphrase)

	assert.Error(t, err, "Expected unlock with wrong password op error")
	assert.True(t, account1.IsAccountLocked(), "Expected account to be locked")
	assert.Nil(t, account1.PrivKey, "expected nil private key for locked account")

	filesystem.DeleteSpacemeshDataFolders(t)

}
