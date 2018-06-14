package repl

import "github.com/spacemeshos/go-spacemesh/accounts"

// Mock struct created temporarily while node
// doesn't implement the methods.
type Mock struct{}

// CreateAccount creates account.
func (Mock) CreateAccount(generatePassphrase bool, accountInfo string) error {
	return nil
}

// LocalAccount returns local account.
func (Mock) LocalAccount() *accounts.Account {
	acct, err := accounts.NewAccount("")
	if err != nil {
		panic(err)
	}

	return acct
}

// Unlock unlock local account or the account by passphrase.
func (Mock) Unlock(passphrase string) error {
	return nil
}

// IsAccountUnLock checks if the account with id is unlock.
func (Mock) IsAccountUnLock(id string) bool {
	return false
}

// Lock locks local account or the account by passphrase.
func (Mock) Lock(passphrase string) error {
	return nil
}

// AccountInfo prints account info.
func (Mock) AccountInfo(id string) {
}

// Transfer transfers the amount from an account to the other.
func (Mock) Transfer(from, to, amount, passphrase string) error {
	return nil
}

// SetVariables sets params or CLI flags values.
func (Mock) SetVariables(params, flags []string) error {
	return nil
}

// GetVariable gets variable in node by key
func (Mock) GetVariable(key string) string {
	if key == "Mock" {
		return "mock"
	}

	return ""
}

// NeedRestartNode checks if the params and flags that will be set need
// restart the node.
func (Mock) NeedRestartNode(params, flags []string) bool {
	return false
}

// Restart restarts node.
func (Mock) Restart(params, flags []string) error {
	return nil
}

// Setup setup POST.
func (Mock) Setup(allocation string) error {
	return nil
}
