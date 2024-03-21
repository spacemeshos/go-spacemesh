package main

import (
	"flag"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/accounts"
	"github.com/spacemeshos/go-spacemesh/sql/rewards"
	"github.com/spacemeshos/go-spacemesh/sql/transactions"
)

func main() {
	dbpath := flag.String("db", "", "")
	acc := flag.String("acc", "", "")
	flag.Parse()

	db, err := sql.Open("file:" + *dbpath)
	must(err)
	addr, err := types.StringToAddress(*acc)
	must(err)
	account, err := accounts.Latest(db, addr)
	must(err)
	rews, err := rewards.List(db, addr)
	must(err)

	var (
		spending uint64
		earnings uint64
		rewards  uint64
	)
	for _, rew := range rews {
		rewards += rew.TotalReward
	}
	filter := transactions.ResultsFilter{Address: &addr}
	must(transactions.IterateResults(db, filter,
		func(rst *types.TransactionWithResult) bool {
			status := "PASS"
			if rst.Status != types.TransactionSuccess {
				status = "FAIL"
			}
			fmt.Printf(
				"%s: ID=%s PRINCIPAL=%s AMOUNT=%d\n",
				status,
				rst.ID.String(),
				rst.Principal.String(),
				rst.MaxSpend,
			)
			if rst.Principal == addr {
				// in case of sending to yourself you just spend gas
				if rst.Status == types.TransactionSuccess && len(rst.Addresses) != 1 {
					spending += rst.MaxSpend
				}
				spending += rst.Fee
			} else {
				if rst.Status == types.TransactionSuccess {
					earnings += rst.MaxSpend
				}
			}
			return true
		}))
	fmt.Printf(
		"address=%s\nlayer=%d\nearned=%d\nrewards=%d\nspent=%d\ntally=%d\naccount=%d\n",
		account.Address.String(),
		account.Layer,
		earnings,
		rewards,
		spending,
		rewards+earnings-spending,
		account.Balance,
	)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
