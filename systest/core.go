package systest

import (
	"context"
	"fmt"
	"strconv"
	"time"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
	"github.com/testground/sdk-go/sync"
	"google.golang.org/grpc"
)

const (
	defaultGasLimit = 10
	defaultFee      = 1
	pollingInterval = 3 * time.Second
)

type SystemTest struct {
	// ID holds the id number of the instance and is the range 1..max_instance
	ID       int64
	re       *runtime.RunEnv
	ic       *run.InitContext
	Account1 *signing.EdSigner
	Account2 *signing.EdSigner
	Cfg      *config.Config
	GRPC     *grpc.ClientConn
	ctx      context.Context
}

// NewSystemTest creates a new SystemTest object based on tesground enviornment
// vars and init context
func NewSystemTest(ctx context.Context, re *runtime.RunEnv, ic *run.InitContext) *SystemTest {
	c := config.DefaultConfig()

	t := SystemTest{re: re,
		ic:  ic,
		Cfg: &c,
		ctx: ctx,
	}
	// setup Acount1 & Account2
	var err error
	t.Account1, err = signing.NewEdSignerFromBuffer(
		util.FromHex(config.Account1Private))
	if err != nil {
		t.Errorf("Failed to create a ed signer for Account1: %s", err)
	}
	t.Account2, err = signing.NewEdSignerFromBuffer(
		util.FromHex(config.Account2Private))
	if err != nil {
		t.Errorf("Failed to create a ed signer for Account2: %s", err)
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

	t.re.RecordFailure(fmt.Errorf(msg, a...))
}

// Error signals the test has returned an error
func (t *SystemTest) Errorf(msg string, a ...interface{}) {

	t.re.RecordCrash(fmt.Errorf(msg, a...))
}

// Log adds a log messages
func (t *SystemTest) Log(msg string) {
	t.re.RecordMessage(msg)
}

// Logf adds a formatted log message
func (t *SystemTest) Logf(msg string, a ...interface{}) {
	t.re.RecordMessage(msg, a...)
}

// WaitAll waits for all instances to report a state
func (t *SystemTest) WaitAll(state sync.State) {
	b, err := t.ic.SyncClient.Barrier(
		context.Background(), state, t.re.TestInstanceCount)
	if err != nil {
		t.Errorf("failed while setting barrier for state %s: %w", state, err)
	}
	<-b.C
}

// GetAccountState returns an account's state
func (t *SystemTest) GetAccountState(account *signing.EdSigner) *types.AccountState {
	c := pb.NewGlobalStateServiceClient(t.GRPC)
	res, err := c.Account(context.Background(), &pb.AccountRequest{
		AccountId: &pb.AccountId{Address: account.PublicKey().Bytes()},
	})
	if err != nil {
		t.Errorf("Failed to get account details: %s", err)
	}
	return &types.AccountState{
		Balance: res.AccountWrapper.StateCurrent.Balance.Value,
		Nonce:   res.AccountWrapper.StateCurrent.Counter,
	}
}

// GetBalance send coins from one acounts to another
func (t *SystemTest) SendCoins(nonce uint64, recipient *signing.EdSigner,
	amount uint64, signer *signing.EdSigner) {
	to := types.BytesToAddress(recipient.PublicKey().Bytes())
	tx, err := types.NewSignedTx(nonce, to, amount,
		defaultGasLimit, defaultFee, signer)
	if err != nil {
		t.Errorf("Failed to create a new transaction: %s", err)
	}
	c := pb.NewTransactionServiceClient(t.GRPC)
	serializedTx, err := types.InterfaceToBytes(tx)
	if err != nil {
		t.Errorf("Failed to create a new transaction client: %s", err)
	}
	// TODO: use a better context
	_, err = c.SubmitTransaction(context.Background(),
		&pb.SubmitTransactionRequest{Transaction: serializedTx})
	if err != nil {
		t.Errorf("Failed to create a new transaction client: %s", err)
	}
	// TODO: test _ == result
}

// WaitTillEpoch wait till the next epoch
func (t *SystemTest) WaitTillEpoch() {
	c := pb.NewMeshServiceClient(t.GRPC)
	res, err := c.CurrentEpoch(t.ctx, &pb.CurrentEpochRequest{})
	if err != nil {
		t.Errorf("Failed to get CurrentEpoc %s", err)
	}
	start := res.Epochnum.Value
	keepGoing := make(chan uint64)
	for v := start; v == start; v = v {
		time.Sleep(pollingInterval)
		response, err := c.CurrentEpoch(t.ctx, &pb.CurrentEpochRequest{})
		if err != nil {
			t.Errorf("Failed to get CurrentEpoc %s", err)
		}
		v = response.Epochnum.Value
		select {
		case <-t.ctx.Done():
			t.Errorf("Stopping test on done signal")
		case keepGoing <- v:
		}
	}
}

// RequireBalance fail if an account is not of a given value
func (t *SystemTest) RequireBalance(account *signing.EdSigner, balance uint64) {
	s := t.GetAccountState(account)
	if s.Balance != balance {
		t.Failf("Account balance is %d expecting %d account: %v", s.Balance, balance, account)
	}
}

// NewAccount creates a new account and returns it's key
func (t *SystemTest) NewAccount() *signing.EdSigner {
	// TODO: code it
	return signing.NewEdSigner()
}

// Close cleans up after the test
func (t *SystemTest) Close() {
	// TODO: code it
	t.GRPC.Close()
}
