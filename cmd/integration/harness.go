package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"cloud.google.com/go/storage"
	"github.com/spacemeshos/go-spacemesh/log"

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
	restoreFileNameI := Contains(args, "--restore-filename")
	if restoreFileName == -1 {
		cfg = DefaultConfig(execPath)
	} else {
		cfg = RestoreConfig(execPath, args[restoreFileNameI+1])
	}
	return NewHarness(cfg, args)
}

// NewHarness creates and initializes a new instance of Harness.
func NewHarness(cfg *ServerConfig, args []string) (*Harness, error) {
	if cfg.restoreFileName != "" {
		log.Info("Restoring backup from: %s", cfg.restoreFileName)
		tarxzf(cfg.restoreFileName)
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

// tarxzf downloads a tar.gz file from gcloud an extract it in root
func tarxzf(filename string) {
	ctx := context.Background()

	client, err := storage.NewClient(ctx)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()
	// if load state from google storage
	bucketName := os.Getenv("STATE_BUCKET")
	if bucketName == "" {
		log.Fatal("Missing STATE_BUCKET")
		return
	}
	// Creates a Bucket instance.
	bucket := client.Bucket(bucketName)

	obj := bucket.Object(filename).ReadCompressed(true) // see https://developer.bestbuy.com/apis
	rdr, err := obj.NewReader(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer rdr.Close()

	gzr, err := gzip.NewReader(rdr)
	if err != nil {
		log.Fatal(err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()

		switch {
		// if no more files are found return
		case err == io.EOF:
			return nil
		// return any other error
		case err != nil:
			log.Fatalf("Failed to extract backup: %s", err)
			return
		// if the header is nil, just skip it (not sure how this happens)
		case header == nil:
			continue
		}

		// the target location where the dir/file should be created
		target := filepath.Join("/", header.Name)

		// the following switch could also be done using fi.Mode(), not sure if there
		// a benefit of using one vs. the other.
		// fi := header.FileInfo()

		// check the file type
		switch header.Typeflag {

		// if its a dir and it doesn't exist create it
		case tar.TypeDir:
			if _, err := os.Stat(target); err != nil {
				if err := os.MkdirAll(target, 0755); err != nil {
					log.Fatalf("Failed to mkdir: %s", err)
					return
				}
			}

		// if it's a file create it
		case tar.TypeReg:
			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				log.Fatalf("Failed to create a file: %s", err)
				return
			}

			// copy over contents
			if _, err := io.Copy(f, tr); err != nil {
				log.Fatalf("Failed to copy archive file to disk: %s", err)
				return
			}

			// manually close here after each file operation; defering would cause each file close
			// to wait until all operations have completed.
			f.Close()
		}
	}
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
				break
			}
		}
	}()

	log.With().Info("integration: harness is listening on a blocking dummy channel")
	<-dummyChan
}
