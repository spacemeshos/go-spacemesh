package main

import (
	"github.com/testground/sdk-go/runtime"
)

type SystemTest struct {
	env     *runtime.RunEnv
	initCtx *InitContext
}

// NewSystemTest creates a new SystemTest object
func NewSystemTest(env *runtime.RunEnv, initCtx *InitContext) SystemTest {
	return SystemTest{env: env,
		initCtx: initCtx}
}

// SetState uses the sync service to publish the new state
func (t *SystemTest) SetState(state string) {
	t.Log("Setting state to: %q", state)
}
