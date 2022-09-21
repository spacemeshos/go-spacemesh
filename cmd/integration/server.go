package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"

	"github.com/spacemeshos/go-spacemesh/log"
)

const harnessPort = "9094"

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

// server houses the necessary state required to configure, launch,
// and manage node server process.
type server struct {
	cfg *ServerConfig
	cmd *exec.Cmd

	// errChan is an error channel to pass errors from cmd
	errChan chan error
	// quit channel for an out source to quit process
	quit chan struct{}
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
func (s *server) start(args []string) error {
	s.quit = make(chan struct{})

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
		s.errChan <- fmt.Errorf("cmd.Run() failed with error: %w", err)
	}

	log.With().Info("exiting integration server")

	return nil
}
