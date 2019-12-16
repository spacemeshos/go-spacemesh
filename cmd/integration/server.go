package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/spacemeshos/go-spacemesh/log"
)

const harnessPort = "9092"

// ServerConfig contains all the args and data required to launch a node
// server instance.
type ServerConfig struct {
	logLevel  string
	rpcListen string
	exe       string
}

// DefaultConfig returns a newConfig with all default values.
func DefaultConfig(execPath string) (*ServerConfig, error) {
	cfg := &ServerConfig{
		logLevel:  "debug",
		rpcListen: "127.0.0.1:" + harnessPort,
		exe:       execPath,
	}

	return cfg, nil
}

// genArgs generates a slice of command line arguments from ServerConfig instance.
func (cfg *ServerConfig) genArgs() []string {
	var args []string

	args = append(args)

	return args
}

// server houses the necessary state required to configure, launch,
// and manage node server process.
type server struct {
	cfg *ServerConfig
	cmd *exec.Cmd

	// errChan is an error channel to pass errors from cmd
	errChan chan error
	// quit channel for an out source to quit process
	quit chan struct{}
	wg   sync.WaitGroup
	buff *bytes.Buffer
}

// newServer creates a new node server instance according to the passed cfg.
func newServer(cfg *ServerConfig) (*server, error) {
	return &server{
		cfg:     cfg,
		errChan: make(chan error, 5),
		buff:    &bytes.Buffer{},
	}, nil
}

// start launches a new running process of node server.
func (s *server) start(addArgs []string) error {
	s.quit = make(chan struct{})

	args := s.cfg.genArgs()
	// adding additional full go-spacemesh node arguments origin in
	// yaml specification files, starting from index 1 to remove exec path
	args = append(args, addArgs...)

	s.cmd = exec.Command(s.cfg.exe, args...)
	// Redirect stderr and stdout output to current harness buffers
	s.cmd.Stdout = os.Stdout
	s.cmd.Stderr = s.buff

	// start go-spacemesh server
	if err := s.cmd.Start(); err != nil {
		s.errChan <- fmt.Errorf("cmd.Start() failed with '%s'", err)
	}

	err := s.cmd.Wait()

	if err != nil {
		// move err to error channel
		log.Error("an error has occurred during go-spacemesh command wait: %v", err)
		s.errChan <- fmt.Errorf("cmd.Run() failed with %s", err)
	}

	log.With().Info("exiting integration server")

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
	if err := s.cmd.Process.Kill(); err != nil {
		return fmt.Errorf("failed to kill process: %v", err)
	}

	close(s.quit)
	s.wg.Wait()

	s.quit = nil
	return nil
}
