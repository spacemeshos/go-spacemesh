package accounts

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestLockAccount(t *testing.T) {

	// arrange
	const passphrase = "a-weak-passphrase-123"
	account, err := NewAccount(passphrase)
	if err != nil {
		t.Fatalf("Failed to create an account")
	}

	// act
	account.LockAccount(passphrase)

	// assert
	assert.True(t, account.IsAccountLocked(), "Expected account to be locked")
}
