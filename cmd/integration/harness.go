package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spacemeshos/go-spacemesh/log"
)

const execPathLabel = "executable-path"

// Contains tells whether a contains x.
// if it does it returns it's index otherwise -1
// TODO: this should be a util function.
func Contains(a []string, x string) int {
	for ind, n := range a {
		if strings.Contains(n, x) {
			return ind
		}
	}

	return -1
}

// Harness fully encapsulates an active node server process
// along with client connection and full api, created for rpc integration
// tests and may be used for any other purpose.
type Harness struct {
	server *server
}

func newHarnessDefaultServerConfig(args []string) (*Harness, error) {
	// same as in suite's yaml file
	// find executable path label in args
	execPathInd := Contains(args, execPathLabel)
	if execPathInd == -1 {
		return nil, fmt.Errorf("could not find executable path in arguments")
	}
	// next will be exec path value
	execPath := args[execPathInd+1]
	// remove executable path label and value
	args = append(args[:execPathInd], args[execPathInd+2:]...)
	// set servers' configuration
	cfg, errCfg := DefaultConfig(execPath)
	if errCfg != nil {
		return nil, fmt.Errorf("failed to build mock node: %v", errCfg)
	}
	return NewHarness(cfg, args)
}

// NewHarness creates and initializes a new instance of Harness.
func NewHarness(cfg *ServerConfig, args []string) (*Harness, error) {
	log.Info("Starting harness")
	server, err := newServer(cfg)
	if err != nil {
		return nil, err
	}

	// Spawn a new mockNode server process.
	log.Info("harness passing the following arguments: %v", args)
	log.Info("Full node server start listening on: %v", server.cfg.rpcListen)
	if err := server.start(args); err != nil {
		log.Error("Full node ERROR listening on: %v", server.cfg.rpcListen)
		return nil, err
	}

	h := &Harness{
		server: server,
	}

	return h, nil
}

func main() {
	log.JSONLog(true)

	// os.Args[0] contains the current process path
	h, err := newHarnessDefaultServerConfig(os.Args[1:])
	if err != nil {
		log.With().Error("harness: an error has occurred while generating a new harness:", log.Err(err))
		log.Panic("error occurred while generating a new harness")
	}
	log.With().Info("integration: harness is listening on a blocking dummy channel")

	// os.Interrupt for all systems, especially windows, syscall.SIGTERM is mainly for docker.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	func() {
		for {
			select {
			case errMsg := <-h.server.errChan:
				log.With().Error("harness: received an err from subprocess: ", log.Err(errMsg), log.String("harness-error", h.server.buff.String()))
				return
			case <-ctx.Done():
				log.With().Info("harness: got a quit signal from subprocess")
				return
			}
		}
	}()

	if err := h.server.stop(); err == nil {
		log.With().Error("harness: failed to stop", log.Err(err))
	}
}
