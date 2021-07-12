package main

import (
	"github.com/testground/sdk-go/run"
)

var testcases = map[string]interface{}{
	"transaction": InitTestCase(testTransaction),
	"new_account": InitTestCase(testNewAccount),
}

// test_transaction transers 100 coins between the two tests accounts waits
//					for next epoch and asserts the accounts' balance
func testTransaction(t *T) {
	seq := t.GetSeq()
	t.Log("Starting transaction test, %d is up", seq)
	// TODO: ready should be replaced with starting the node and waiting for
	// genesis
	t.SetState("ready")
	t.WaitAll("ready")
	if seq == 1 {
		t.SendCoins(Account1, Account2, 100)
	}
	t.WaitForNextEpoch()
	t.RequireBalance(Account1, 100000000000000000-100)
	t.RequireBalance(Account2, 100000000000000000+100)
}

// test_new_account uses the firest node to creates a new account and
// send it 100 coins from a test account to it and asserts the accounts' balance
func testNewAccount(t *T) {
	seq := t.GetSeq()
	t.Log("Starting new account test, %d is up", seq)
	// TODO: ready should be replaced with starting the node and waiting for
	// genesis
	t.SetState("ready")
	t.WaitAll("ready")
	if seq == 1 {
		account := t.NewAccount()
		t.SendCoins(Account1, account, 100)
	}
	t.WaitForNextEpoch()
	t.RequireBalance(Account1, 100000000000000000-100)
	t.RequireBalance(account, 100)
}

func main() {
	run.InvokeMap(testcases)
}
