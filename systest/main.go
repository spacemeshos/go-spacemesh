package systest

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
	"github.com/testground/sdk-go/sync"
)

type SystemTest struct {
	// ID holds the id number of the instance and is the range 1..max_instance
	ID       int64
	env      *runtime.RunEnv
	ic       *run.InitContext
	Account1 string
	Account2 string
}

// NewSystemTest creates a new SystemTest object based on tesground enviornment
// vars and init context
func NewSystemTest(env *runtime.RunEnv, ic *run.InitContext) *SystemTest {
	t := SystemTest{env: env,
		ic:       ic,
		Account1: config.Account1Pub,
		Account2: config.Account2Pub,
	}
	t.ID = t.SetState("init")
	return &t
}

// SetState uses the sync service to publish the new state return the current
// number of nodes in the given state
func (t *SystemTest) SetState(state string) int64 {
	t.Logf("Setting state to: %q", state)
	ctx := context.Background()
	count, err := t.ic.SyncClient.SignalEntry(ctx, sync.State(state))
	if err != nil {
		t.Logf("Failed to signal state: %s", err)
		return -1
	}
	return count
}

// Log adds a log messages
func (t *SystemTest) Log(msg string) {
	t.env.RecordMessage(msg)
}

// Logf adds a formatted log message
func (t *SystemTest) Logf(msg string, a ...interface{}) {
	t.env.RecordMessage(msg, a...)
}

// WaitAll waits for all instances to report a state
func (t *SystemTest) WaitAll(state sync.State) {
	// TODO: code it
}

// GetBalance TODO: returns an accounts balance
func (t *SystemTest) GetBalance(account string) uint64 {
	// TODO: code it
	return 0
}

// GetBalance TODO: send coins from one acounts to another
func (t *SystemTest) SendCoins(from string, to string, amount uint64) {
	// TODO: code it
}

// WaitTillEpoch TODO: wait till the next epoch
func (t *SystemTest) WaitTillEpoch() {
	// TODO: code it
}

// RequireBalance fail if an account is not of a given value
func (t *SystemTest) RequireBalance(account string, balance uint64) {
	// TODO: code it
}

// NewAccount creates a new account and returns it's key
func (t *SystemTest) NewAccount() string {
	// TODO: code it
	return "TODO"
}

// Close cleans up after the test
func (t *SystemTest) Close() {
	// TODO: code it
}
