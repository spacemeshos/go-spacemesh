package types

import "math/big"

// Account struct represents basic account data: nonce and balance
type Account struct {
	Nonce   uint64 		`json:"nonce"`
	Balance *big.Int 	`json:"balance"`
}

// AccountsState is a struct used to dump an entire state root
type AccountsState struct {
	Root     string              `json:"root"`
	Accounts map[string] Account `json:"accounts"` //key is in hex string format e.g. 0x12....
}
