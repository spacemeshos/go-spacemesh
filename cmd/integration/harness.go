package main

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
	"os"
	"strings"

	"google.golang.org/grpc"
)

// Contains tells whether a contains x.
// if it does it returns it's index otherwise -1
// TODO: this should be a util function
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
}

func newHarnessConfig(args []string) (*Harness, error) {
	var cfg *ServerConfig
	// same as in suite's yaml file
	// find executable path label in args
	execPathInd := Contains(args, "executable-path")
	if execPathInd == -1 {
		return nil, fmt.Errorf("could not find executable path in arguments")
	}
	// next will be exec path value
	execPath := args[execPathInd+1]
	// remove executable path label and value
	args = append(args[:execPathInd], args[execPathInd+2:]...)
	args = append(args, "--acquire-port=false")
	// set servers' configuration
	// get filename index under args
	restoreFileNameInd := Contains(args, "--data-paths")
	if restoreFileNameInd == -1 {
		cfg = DefaultConfig(execPath)
	} else {
		restoreFileName := args[restoreFileNameInd+1]
		args = append(args[:restoreFileNameInd], args[restoreFileNameInd+2:]...)
		stateBucketInd := Contains(args, "--state-bucket")
		if stateBucketInd == -1 {
			log.Panic("state bucket must be supplied")
		}
		stateBucket := args[stateBucketInd+1]
		args = append(args[:stateBucketInd], args[stateBucketInd+2:]...)
		cfg = RestoreConfig(execPath, stateBucket, restoreFileName)
	}
	return NewHarness(cfg, args)
}

// NewHarness creates and initializes a new instance of Harness.
func NewHarness(cfg *ServerConfig, args []string) (*Harness, error) {
	if cfg.restoreFileName != "" {
		// TODO: remove white spaces
		pathsList := strings.Split(cfg.restoreFileName, ",")
		for i := 0; i < len(pathsList); i++ {
			log.Info("Restoring backup from: %s", pathsList[i])
			util.Tarxzf(cfg.bucketName, pathsList[i])
		}
	}
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
	// setup logger
	log.JSONLog(true)

	dummyChan := make(chan string)
	// os.Args[0] contains the current process path
	h, err := newHarnessConfig(os.Args[1:])
	if err != nil {
		log.With().Error("harness: an error has occurred while generating a new harness:", log.Err(err))
		log.Panic("error occurred while generating a new harness")
	}

	// listen on error channel, quit when process stops
	go func() {
		for {
			select {
			case errMsg := <-h.server.errChan:
				log.With().Error("harness: received an err from subprocess: ", log.Err(errMsg))
				log.Error("harness: the err is: %s", h.server.buff.String())
			case <-h.server.quit:
				log.With().Info("harness: got a quit signal from subprocess")
				return
			}
		}
	}()

	log.With().Info("integration: harness is listening on a blocking dummy channel")
	<-dummyChan
}
