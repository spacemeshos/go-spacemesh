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

func testArgs(t *testing.T, app *SpacemeshApp, args ...string) (output string, err error) {
	root := Cmd

	// We need to run Initialize to read the args, but we don't
	// want to run Start to actually boot up the node.
	root.Run = func(*cobra.Command, []string) {
		if err := app.Initialize(root, nil); err != nil {
			t.Error(err)
		}
	}

	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	_, err = root.ExecuteC()
	return buf.String(), err
}

func TestSpacemeshApp_Cmd(t *testing.T) {
	r := require.New(t)
	app := NewSpacemeshApp()

	// Test an illegal flag
	got, err := testArgs(t, app, "illegal")
	expected := `unknown command "illegal" for "node"`
	expected2 := "Error: " + expected + "\nRun 'node --help' for usage.\n"
	r.Error(err)
	r.Equal(expected, err.Error())
	r.Equal(expected2, got)
	r.Equal(false, app.Config.TestMode)

	// Test a legal flag
	got2, err2 := testArgs(t, app, "--test-mode")
	r.NoError(err2)
	r.Empty(got2)
	r.Equal(true, app.Config.TestMode)
}

func TestSpacemeshApp_CmdGrpcPortNew(t *testing.T) {
	r := require.New(t)
	app := NewSpacemeshApp()
	r.Equal(9092, app.Config.API.NewGrpcServerPort)

	got, err := testArgs(t, app, "--grpc-port-new", "123")
	r.Empty(got)
	r.NoError(err)
	r.Equal(123, app.Config.API.NewGrpcServerPort)
}
