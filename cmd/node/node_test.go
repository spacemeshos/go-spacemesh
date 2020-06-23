package node

import (
	"bytes"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSpacemeshApp_getEdIdentity(t *testing.T) {
	r := require.New(t)

	defer func() {
		// cleanup
		err := os.RemoveAll("tmp")
		r.NoError(err)
	}()

	// setup spacemesh app
	app := NewSpacemeshApp()
	app.Config.POST.DataDir = "tmp"
	app.log = log.NewDefault("logger")

	// get new identity
	sgn, err := app.LoadOrCreateEdSigner()
	r.NoError(err)

	// ensure we have a single subdirectory under tmp
	infos, err := ioutil.ReadDir("tmp")
	r.NoError(err)
	r.Len(infos, 1)

	// run the method again
	sgn2, err := app.LoadOrCreateEdSigner()
	r.NoError(err)

	// ensure that we didn't create another identity
	infos, err = ioutil.ReadDir("tmp")
	r.NoError(err)
	r.Len(infos, 1)

	// ensure both signers are identical
	r.Equal(sgn.PublicKey(), sgn2.PublicKey())

	// mess with the directory name
	err = os.Rename(filepath.Join("tmp", infos[0].Name()), filepath.Join("tmp", "wrong name"))
	r.NoError(err)

	// run the method again
	_, err = app.LoadOrCreateEdSigner()
	r.EqualError(err, fmt.Sprintf("identity file path ('tmp/wrong name') does not match public key (%v)", sgn.PublicKey().String()))
}

func TestSpacemeshApp_SetLoggers(t *testing.T) {
	r := require.New(t)

	app := NewSpacemeshApp()
	mylogger := "anton"
	tmpDir := os.TempDir()
	tmpFile, err := ioutil.TempFile(tmpDir, "tmp_")
	r.NoError(err)
	myLog := log.New("logger", tmpDir, tmpFile.Name())
	myLog2 := log.New("logger2", tmpDir, tmpFile.Name())

	//myLog := log.NewDefault("logger")
	app.log = app.addLogger(mylogger, myLog)
	app.log.Info("hi there")
	err = app.SetLogLevel("anton", "warn")
	r.NoError(err)
	r.Equal("warn", app.loggers["anton"].String())

	myLog2.Info("other logger")
	app.log.Info("hi again")
	app.log.Warning("warn")

	err = app.SetLogLevel("anton", "info")
	r.NoError(err)

	app.log.Info("hi again 2")
	r.Equal("info", app.loggers["anton"].String())

	// test wrong logger called
	err = app.SetLogLevel("anton3", "warn")
	r.Error(err)

	// test wrong loglevel
	err = app.SetLogLevel("anton", "lulu")
	r.Error(err)
	r.Equal("info", app.loggers["anton"].String())
}

func TestSpacemeshApp_AddLogger(t *testing.T) {
	r := require.New(t)

	app := NewSpacemeshApp()
	app.Config.LOGGING.HareLoggerLevel = "warn"
	tmpDir := os.TempDir()
	tmpFile, err := ioutil.TempFile(tmpDir, "tmp_")
	r.NoError(err)
	myLog := log.New("logger", tmpDir, tmpFile.Name())
	l := app.addLogger(HareLogger, myLog)
	r.Equal("warn", app.loggers["hare"].String())
	l.Info("not supposed to be printed")
}

func testArgs(t *testing.T, app *SpacemeshApp, args ...string) (<-chan error, <-chan error, <-chan string) {
	root := Cmd

	// We need to run Initialize to read the args, but we don't
	// want to run Start to actually boot up the node.
	errChanExternal := make(chan error)
	errChanInternal := make(chan error)
	strChan := make(chan string)

	root.Run = func(*cobra.Command, []string) {
		defer close(errChanInternal)
		errChanInternal <- app.Initialize(root, nil)
	}

	// This needs to be a goroutine. Otherwise, Run() would get called
	// before we can even return the channel that it writes to.
	go func() {
		defer close(errChanExternal)
		defer close(strChan)
		buf := new(bytes.Buffer)
		root.SetOut(buf)
		root.SetErr(buf)
		root.SetArgs(args)
		_, err := root.ExecuteC() // runs Run()
		errChanExternal <- err
		strChan <- buf.String()
	}()
	return errChanInternal, errChanExternal, strChan
}

func TestSpacemeshApp_Cmd(t *testing.T) {
	r := require.New(t)
	app := NewSpacemeshApp()

	// Test an illegal flag
	errChanInt, errChanExt, strChan := testArgs(t, app, "illegal")
	expected := `unknown command "illegal" for "node"`
	expected2 := "Error: " + expected + "\nRun 'node --help' for usage.\n"
	r.Equal(false, app.Config.TestMode)

	// We expect exactly two messages from two of the channels
	for i := 0; i < 2; i++ {
		select {
		case err := <-errChanExt:
			r.Error(err)
			r.Equal(expected, err.Error())
		case got := <-strChan:
			r.Equal(expected2, got)
		case err2 := <-errChanInt:
			// Run should not even run, so this channel should receive nothing
			r.Fail("received unexpected error: ", err2)
		case <-time.After(5 * time.Second):
			r.Fail("timed out waiting for command result")
		}
	}

	// Test a legal flag
	errChanInt, errChanExt, strChan = testArgs(t, app, "--test-mode")

	// We expect exactly three messages, one from each channel
	for i := 0; i < 3; i++ {
		select {
		case err := <-errChanExt:
			r.NoError(err)
		case got := <-strChan:
			r.Empty(got)
		case err2 := <-errChanInt:
			r.NoError(err2)
		case <-time.After(5 * time.Second):
			r.Fail("timed out waiting for command result")
		}
	}
	r.Equal(true, app.Config.TestMode)
}

func TestSpacemeshApp_GrpcFlags(t *testing.T) {
	r := require.New(t)
	app := NewSpacemeshApp()
	r.Equal(9092, app.Config.API.NewGrpcServerPort)
	r.Equal(false, app.Config.API.StartNodeService)

	//errChan, got, err := testArgs(t, app, "--grpc-port-new", "1234", "--grpc", "illegal")
	//<-errChan
	//r.Empty(got)
	////r.Equal("Unrecognized GRPC service requested: illegal", got)
	//r.Error(err)
	//r.Equal("Unrecognized GRPC service requested: illegal", err.Error())
	//
	//
	//
	//
	//errChan, got, err = testArgs(t, app, "--grpc-port-new", "1234", "--grpc", "node")
	//r.Empty(got)
	//r.NoError(err)
	//r.Equal(123, app.Config.API.NewGrpcServerPort)
}
