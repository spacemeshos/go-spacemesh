package types

// Account represents account state at a certain layer.
type Account struct {
	Layer   LayerID `json:"-"`
	Address Address `json:"-"`
	Balance uint64  `json:"balance"`
}

// AccountState struct represents basic account data nonce and balance.
type AccountState struct {
	Nonce   uint64 `json:"nonce"`
	Balance uint64 `json:"balance"`
}

// MultipleAccountsState is a struct used to dump an entire state root.
type MultipleAccountsState struct {
	Root     string                  `json:"root"`
	Accounts map[string]AccountState `json:"accounts"` // key is in hex string format e.g. 0x12...
}
