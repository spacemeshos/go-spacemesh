package integration

import (
	"bytes"
	"fmt"
	"os/exec"
	"sync"
)

const harnessPort = "9092"

// ServerConfig contains all the args and data required to launch a node
// server instance.
type ServerConfig struct {
	logLevel             string
	rpcListen            string
	baseDir              string
	exe                  string
}

// DefaultConfig returns a newConfig with all default values.
func DefaultConfig(srcCodePath string) (*ServerConfig, error) {
	baseDir, err := baseDir()
	if err != nil {
		return nil, err
	}

	nodePath, err := nodeExecutablePath(srcCodePath, baseDir)
	if err != nil {
		return nil, err
	}

	cfg := &ServerConfig{
		logLevel:  "debug",
		rpcListen: "127.0.0.1:" + harnessPort,
		baseDir:   baseDir,
		exe:       nodePath,
	}

	return cfg, nil
}

// genArgs generates a slice of command line arguments from ServerConfig instance.
func (cfg *ServerConfig) genArgs() []string {
	var args []string

	args = append(args, cfg.rpcListen)

	return args
}

// server houses the necessary state required to configure, launch,
// and manage node server process.
type server struct {
	cfg *ServerConfig
	cmd *exec.Cmd

	// processExit is a channel that's closed once it's detected that the
	// process this instance is bound to has exited.
	processExit chan struct{}
	// quit channel for an out source to quit process
	quit chan struct{}
	wg   sync.WaitGroup
	// error channel for the server error messages
	errChan chan error
}

// newServer creates a new node server instance according to the passed cfg.
func newServer(cfg *ServerConfig) (*server, error) {
	return &server{
		cfg:     cfg,
		errChan: make(chan error),
	}, nil
}

// start launches a new running process of node server.
func (s *server) start() error {
	s.quit = make(chan struct{})

	args := s.cfg.genArgs()
	s.cmd = exec.Command(s.cfg.exe, args...)

	// Redirect stderr output to buffer
	var errb bytes.Buffer
	s.cmd.Stderr = &errb

	if err := s.cmd.Start(); err != nil {
		return err
	}

	// Launch a new goroutine that bubbles up any potential fatal
	// process errors to errChan.
	s.processExit = make(chan struct{})
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		err := s.cmd.Wait()

		if err != nil {
			// move err to error channel
			s.errChan <- fmt.Errorf("%v\n%v\n", err, errb.String())
		}

		// Signal any onlookers that this process has exited.
		close(s.processExit)
	}()

	return nil
}

// shutdown terminates the running node server process, and cleans up
// all files/directories created by it.
func (s *server) shutdown() error {
	if err := s.stop(); err != nil {
		return err
	}

	return nil
}

// stop kills the server running process, since it doesn't support
// RPC-driven stop functionality.
func (s *server) stop() error {
	// Do nothing if the process is not running.
	if s.processExit == nil {
		return nil
	}

	if err := s.cmd.Process.Kill(); err != nil {
		return fmt.Errorf("failed to kill process: %v", err)
	}

	close(s.quit)
	s.wg.Wait()

	s.quit = nil
	s.processExit = nil
	return nil
}
