package types

// AccountState struct represents basic account data: nonce and balance
// Todo: get rid of big.Int everywhere and replace with uint64
// See https://github.com/spacemeshos/go-spacemesh/issues/2192
type AccountState struct {
	Nonce   uint64 `json:"nonce"`
	Balance uint64 `json:"balance"`
}

// MultipleAccountsState is a struct used to dump an entire state root
type MultipleAccountsState struct {
	Root     string                  `json:"root"`
	Accounts map[string]AccountState `json:"accounts"` // key is in hex string format e.g. 0x12...
}
