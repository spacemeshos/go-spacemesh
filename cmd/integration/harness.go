package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
)

const execPathLabel = "executable-path"

// TODO: this should be a util function
// Contains tells whether a contains x.
// if it does it returns it's index otherwise -1
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

func NewHarnessDefaultServerConfig(args []string) (*Harness, error) {
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

	// kill node process in case it is already up
	if isListening(cfg.rpcListen) {
		err = killProcess(cfg.rpcListen)
		if err != nil {
			return nil, err
		}
	}

	// Spawn a new mockNode server process.
	log.Info("harness passing the following arguments:\n", args)
	log.Info("Full node server start listening on:", server.cfg.rpcListen+"\n")
	if err := server.start(args); err != nil {
		fmt.Println("Full node ERROR listening on:", server.cfg.rpcListen+"\n")
		return nil, err
	}

	h := &Harness{
		server: server,
	}

	return h, nil
}

func isListening(addr string) bool {
	conn, _ := net.DialTimeout("tcp", addr, 1*time.Second)
	if conn != nil {
		_ = conn.Close()
		return true
	}
	return false
}

func killProcess(address string) error {
	addr, err := net.ResolveTCPAddr("tcp", address)
	if err != nil {
		return err
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		args := fmt.Sprintf("(Get-NetTCPConnection -LocalPort %d).OwningProcess -Force", addr.Port)
		cmd = exec.Command("Stop-Process", "-Id", args)
	} else {
		args := fmt.Sprintf("lsof -i tcp:%d | grep LISTEN | awk '{print $2}' | xargs kill -9", addr.Port)
		cmd = exec.Command("bash", "-c", args)
	}

	var errb bytes.Buffer
	cmd.Stderr = &errb

	if err := cmd.Start(); err != nil {
		return err
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("error during killing process: %s | %s", err, errb.String())
	}

	return nil
}

func startServer(args []string) (*Harness, error) {
	harness, err := NewHarnessDefaultServerConfig(args)
	if err != nil {
		log.Error("An error has occurred while generating a new harness: ", err)
	}

	return harness, nil
}

func main() {
	// os.Args[0] contains the current process path
	_, err := startServer(os.Args[1:])
	if err != nil {
		log.Error("An error has occurred while generating a new harness: ", err)
	}

	srv := &http.Server{Addr: ":6060"}
	defer func() {
		if err := srv.Shutdown(context.TODO()); err != nil {
			log.Error("cannot shutdown http server: ", err)
		}
	}()

	err = srv.ListenAndServe()
	if err != nil {
		log.Error("cannot start http server: ", err)
	}
}
