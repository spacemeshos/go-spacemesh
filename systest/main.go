package main

import (
	"context"

	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
	"github.com/testground/sdk-go/sync"
)

type SystemTest struct {
	// ID holds the id number of the instance and is the range 1..max_instance
	ID      int64
	env     *runtime.RunEnv
	initCtx *run.InitContext
}

// NewSystemTest creates a new SystemTest object based on tesground enviornment
// vars and init context
func NewSystemTest(env *runtime.RunEnv, initCtx *run.InitContext) *SystemTest {
	t := SystemTest{env: env,
		initCtx: initCtx,
	}
	t.ID = t.SetState("new")
	return &t
}

// SetState uses the sync service to publish the new state
func (t *SystemTest) SetState(state string) int64 {
	t.Logf("Setting state to: %q", state)
	ctx := context.Background()
	count, err := t.initCtx.SyncClient.SignalEntry(ctx, sync.State(state))
	if err != nil {
		t.Errorf("Failed to signal state: %s", err)
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
