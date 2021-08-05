package main

import (
	"bytes"
	"context"
	"net/http"
	"time"

	"github.com/spacemeshos/go-spacemesh/systest"
	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
)

var testcases = map[string]interface{}{
	"account_state": run.InitializedTestCaseFn(testAccountState),
}

// test_Account_state tests the methodused to get accounts' state
func testAccountState(env *runtime.RunEnv, initCtx *run.InitContext) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	t := systest.NewMockedNodeTest(ctx, env, initCtx)
	defer t.Close()
	t.Log("Starting account state test")
	retBal := bytes.NewBufferString(`
{
  "service":"GlobalState",
  "method":"AccountRequest",
  "input":{ // input matching rule. see Input Matching Rule section below
	"equals": {
		"AccountId": "0x7be017a967db77fd10ac7c891b3d6d946dea7e3e14756e2f0f9e09b9663f0d9c"
	}
  },
 "output":{ // output json if input were matched
	"data":{
		"Nonce": 1
		"Balalnce": 2
	},
  }
}`)
	//TODO: check res
	_, err := http.Post("http://localhost:4771", "application/json", retBal)
	if err != nil {
		t.Errorf("Failed sending a post request to peermock: %s", err)
	}
	s := t.GetAccountState(t.Account1)
	if s.Nonce != 1 {
		t.Failf("Got wrong account nonce expected 1 got %d", s.Balance)
	}
	if s.Balance != 2 {
		t.Failf("Got wrong account balance expecrted 2 got %d", s.Balance)
	}
	t.RequireBalance(t.Account2, 2)
	return nil
}

func main() {
	run.InvokeMap(testcases)
}
