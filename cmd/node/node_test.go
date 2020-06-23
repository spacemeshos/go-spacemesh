package node

import (
	"bytes"
	"fmt"
	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
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

func testArgs(app *SpacemeshApp, run func(*cobra.Command, []string), args ...string) (string, error) {
	root := Cmd
	root.Run = run
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	_, err := root.ExecuteC() // runs Run()
	return buf.String(), err
}

func TestSpacemeshApp_Cmd(t *testing.T) {
	r := require.New(t)
	app := NewSpacemeshApp()

	expected := `unknown command "illegal" for "node"`
	expected2 := "Error: " + expected + "\nRun 'node --help' for usage.\n"
	r.Equal(false, app.Config.TestMode)

	// Test an illegal flag
	str, err := testArgs(app,
		func(*cobra.Command, []string) {
			// We don't expect this to be called at all
			r.Fail("Command.Run not expected to run")
		}, "illegal")
	r.Error(err)
	r.Equal(expected, err.Error())
	r.Equal(expected2, str)

	// Test a legal flag
	str, err = testArgs(app,
		func(cmd *cobra.Command, args []string) {
			r.NoError(app.Initialize(cmd, args))
		}, "--test-mode")
	r.NoError(err)
	r.Empty(str)
	r.Equal(true, app.Config.TestMode)
}

func TestSpacemeshApp_GrpcFlags(t *testing.T) {
	r := require.New(t)
	app := NewSpacemeshApp()
	r.Equal(9092, app.Config.API.NewGrpcServerPort)
	r.Equal(false, app.Config.API.StartNodeService)

	str, err := testArgs(app,
		func(cmd *cobra.Command, args []string) {
			err := app.Initialize(cmd, args)
			r.Error(err)
			r.Equal("Unrecognized GRPC service requested: illegal", err.Error())
		}, "--grpc-port-new", "1234", "--grpc", "illegal")
	r.NoError(err)
	r.Empty(str)

	// Reset the flags
	Cmd.ResetFlags()
	cmdp.AddCommands(Cmd)
	str, err = testArgs(app,
		func(cmd *cobra.Command, args []string) {
			err := app.Initialize(cmd, args)
			r.NoError(err)
		}, "--grpc-port-new", "1234", "--grpc", "node")
	r.Empty(str)
	r.NoError(err)
	r.Equal(1234, app.Config.API.NewGrpcServerPort)
}
