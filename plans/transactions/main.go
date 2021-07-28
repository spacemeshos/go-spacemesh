package main

import (
	"github.com/spacemeshos/go-spacemesh/systest"
	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
	"github.com/testground/sdk-go/sync"
)

var testcases = map[string]interface{}{
	"transaction": run.InitializedTestCaseFn(testTransaction),
	"new_account": run.InitializedTestCaseFn(testNewAccount),
}

// test_transaction in this test each node starts with getting the ballance
// of the test account. Than the first node transers 100 coins between the
// tests accounts.
// Then all the nodes wait for the next epoch and assert the accounts' balance.
func testTransaction(env *runtime.RunEnv, initCtx *run.InitContext) error {
	t := systest.NewSystemTest(env, initCtx)
	defer t.Close()
	t.Logf("Starting transaction test, %d is up", t.ID)
	// TODO: ready should be replaced with starting the node and waiting for
	// genesis
	t.SetState("ready")
	t.WaitAll(sync.State("ready"))
	start1 := t.GetAccountState(t.Account1).Balance
	start2 := t.GetAccountState(t.Account2).Balance
	if t.ID == 1 {
		t.SendCoins(0, t.Account1, 100, t.Account2)
	}
	t.WaitTillEpoch()
	t.RequireBalance(t.Account1, start1-100)
	t.RequireBalance(t.Account2, start2+100)
	return nil
}

// test_new_account in this test each node starts with getting the ballance
// of the test account. Than the first node creates a new account and
// transers 100 coins between inb from a test account.
// Then all the nodes wait for the next epoch and assert the accounts' balance.

func testNewAccount(env *runtime.RunEnv, initCtx *run.InitContext) error {
	t := systest.NewSystemTest(env, initCtx)
	defer t.Close()
	t.Logf("Starting new account test, %d is up", t.ID)
	// TODO: ready should be replaced with starting the node and waiting for
	// genesis
	t.SetState("ready")
	t.WaitAll(sync.State("ready"))
	start1 := t.GetAccountState(t.Account1).Balance
	if t.ID == 1 {
		// create an acocunt and make the transfer
		a := t.NewAccount()
		t.SendCoins(0, t.Account1, 100, a)
		t.WaitTillEpoch()
		t.RequireBalance(a, 100)
		return nil
	}
	t.WaitTillEpoch()
	t.RequireBalance(t.Account1, start1-100)
	return nil
}

func main() {
	run.InvokeMap(testcases)
}
