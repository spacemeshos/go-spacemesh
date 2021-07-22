package systest

import (
	"context"
	"fmt"
	"strconv"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
	"github.com/testground/sdk-go/sync"
	"google.golang.org/grpc"
)

type SystemTest struct {
	// ID holds the id number of the instance and is the range 1..max_instance
	ID       int64
	env      *runtime.RunEnv
	ic       *run.InitContext
	Account1 string
	Account2 string
	Cfg      config.Config
	GRPC     *grpc.ClientConn
}

// NewSystemTest creates a new SystemTest object based on tesground enviornment
// vars and init context
func NewSystemTest(env *runtime.RunEnv, ic *run.InitContext) *SystemTest {
	c := config.DefaultConfig()
	t := SystemTest{env: env,
		ic:       ic,
		Account1: config.Account1Pub,
		Account2: config.Account2Pub,
		Cfg:      c,
	}

	t.ID = t.SetState("init")
	addr := "localhost:" + strconv.Itoa(t.Cfg.GrpcServerPort)
	// TODO: add node setup
	// Set up a connection to the server.
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		t.Errorf("Failed to dial grpc: %s", err)
	}
	t.GRPC = conn

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

// Fail signals the test has failed
func (t *SystemTest) Failf(msg string, a ...interface{}) {

	t.env.RecordCrash(fmt.Errorf(msg, a...))
}

// Error signals the test has returned an error
func (t *SystemTest) Errorf(msg string, a ...interface{}) {

	t.env.RecordCrash(fmt.Errorf(msg, a...))
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

// GetAccountState returns an account's state
func (t *SystemTest) GetAccountState(account string) *types.AccountState {
	// TODO: code it
	c := pb.NewGlobalStateServiceClient(t.GRPC)
	res, err := c.Account(context.Background(), &pb.AccountRequest{
		AccountId: &pb.AccountId{Address: types.HexToAddress(account).Bytes()},
	})
	if err != nil {
		t.Errorf("Failed to get account details: %s", err)
	}
	return &types.AccountState{
		Balance: res.AccountWrapper.StateCurrent.Balance.Value,
		Nonce:   res.AccountWrapper.StateCurrent.Counter,
	}
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
	s := t.GetAccountState(account)
	if s.Balance != balance {
		t.Failf("Account %s balance is %d expecting %d", account[:6], s.Balance, balance)
	}
}

// NewAccount creates a new account and returns it's key
func (t *SystemTest) NewAccount() string {
	// TODO: code it
	return "TODO"
}

// Close cleans up after the test
func (t *SystemTest) Close() {
	// TODO: code it
	t.GRPC.Close()
}
