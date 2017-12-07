package node

import "github.com/UnrulyOS/go-unruly/accounts"

func (n *Node) GetCoinbaseAccount() accounts.Account {
	return *n.coinbase
}
