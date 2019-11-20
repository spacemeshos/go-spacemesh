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
	"time"

	"github.com/spacemeshos/go-spacemesh/api/pb"

	"google.golang.org/grpc"
)

// Harness fully encapsulates an active node server process
// along with client connection and full api, created for rpc integration
// tests and may be used for any other purpose.
type Harness struct {
	server *server
	conn   *grpc.ClientConn
	pb.SpacemeshServiceClient
}


func NewHarnessDefaultServerConfig(args []string) (*Harness, error) {
	// set servers' configuration
	// TODO #@! srcCodePath is empty string due to monkey patching in DefaultConfig function
	cfg, errCfg := DefaultConfig("")
	if errCfg != nil {
		return nil, fmt.Errorf("failed to build mock node: %v", errCfg)
	}
	return NewHarness(cfg, args)
}

// NewHarness creates and initializes a new instance of Harness.
func NewHarness(cfg *ServerConfig, args []string) (*Harness, error) {
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
	fmt.Println("Full node server start listening on:", server.cfg.rpcListen + "\n")
	if err := server.start(args); err != nil {
		fmt.Println("Full node ERROR listening on:", server.cfg.rpcListen + "\n")
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
		server:                 server,
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
	fmt.Println("received arguments =", os.Args)
	_, err := NewHarnessDefaultServerConfig(os.Args[1:])
	if err != nil {
		fmt.Println("An error has occurred while generating a new harness:", err)
	}
	fmt.Println("sleeping for 3000 seconds (50 mins)")
	time.Sleep(3000*time.Second)
}
