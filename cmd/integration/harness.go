package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spacemeshos/go-spacemesh/api/pb"

	"google.golang.org/grpc"
)

const execPathLabel = "executable-path"

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
	conn   *grpc.ClientConn
	pb.SpacemeshServiceClient
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
	fmt.Println("Starting harness")
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
	fmt.Println("harness passing the following arguments:\n", args)
	fmt.Println("Full node server start listening on:", server.cfg.rpcListen+"\n")
	if err := server.start(args); err != nil {
		fmt.Println("Full node ERROR listening on:", server.cfg.rpcListen+"\n")
		return nil, err
	}

	//// Verify the client connectivity.
	//// If failed, shutdown the server.
	//conn, err := connectClient(cfg.rpcListen)
	//if err != nil {
	//	_ = server.shutdown()
	//	return nil, err
	//}

	h := &Harness{
		server: server,
		//conn:                   conn,
		//SpacemeshServiceClient: pb.NewSpacemeshServiceClient(conn),
	}

	return h, nil
}

// TearDown stops the harness running instance.
// The created process is killed
func (h *Harness) TearDown() error {
	if err := h.server.shutdown(); err != nil {
		return err
	}

	if err := h.conn.Close(); err != nil {
		return err
	}

	return nil
}

// ProcessErrors returns a channel used for reporting any fatal process errors.
func (h *Harness) ProcessErrors() <-chan error {
	return h.server.errChan
}

// connectClient attempts to establish a gRPC Client connection
// to the provided target.
func connectClient(target string) (*grpc.ClientConn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	opts := []grpc.DialOption{
		grpc.WithInsecure(),
		grpc.WithBlock(),
	}
	defer cancel()

	conn, err := grpc.DialContext(ctx, target, opts...)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to RPC server at %s: %v", target, err)
	}
	return conn, nil
}

// baseDir is the directory path of the temp directory for all the harness files.
// NOTICE: right now it's not in use, might be useful in the near future
func baseDir() (string, error) {
	baseDir := filepath.Join(os.TempDir(), "full_node")
	err := os.MkdirAll(baseDir, 0755)
	return baseDir, err
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

func main() {
	// os.Args[0] contains the current process path
	_, err := NewHarnessDefaultServerConfig(os.Args[1:])
	if err != nil {
		fmt.Println("An error has occurred while generating a new harness:", err)
	}
	// Sleeping here to enable the test run before harness terminates
	fmt.Println("sleeping for 3000 seconds (50 mins)")
	time.Sleep(3000 * time.Second)
}
