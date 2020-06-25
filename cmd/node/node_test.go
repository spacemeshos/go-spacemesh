package node

import (
	"bytes"
	"context"
	"fmt"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

func testArgs(app *SpacemeshApp, args ...string) (string, error) {
	root := Cmd
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	// Workaround: will fail if args is nil (defaults to os args)
	if args == nil {
		args = []string{""}
	}
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
	Cmd.Run = func(*cobra.Command, []string) {
		// We don't expect this to be called at all
		r.Fail("Command.Run not expected to run")
	}
	str, err := testArgs(app, "illegal")
	r.Error(err)
	r.Equal(expected, err.Error())
	r.Equal(expected2, str)

	// Test a legal flag
	Cmd.Run = func(cmd *cobra.Command, args []string) {
		r.NoError(app.Initialize(cmd, args))
	}
	str, err = testArgs(app, "--test-mode")
	r.NoError(err)
	r.Empty(str)
	r.Equal(true, app.Config.TestMode)
}

// This must be called in between each test that changes flags
func resetFlags() {
	Cmd.ResetFlags()
	cmdp.AddCommands(Cmd)
}

func TestSpacemeshApp_GrpcFlags(t *testing.T) {
	r := require.New(t)
	app := NewSpacemeshApp()
	r.Equal(9092, app.Config.API.NewGrpcServerPort)
	r.Equal(false, app.Config.API.StartNodeService)

	// Try enabling an illegal service
	Cmd.Run = func(cmd *cobra.Command, args []string) {
		err := app.Initialize(cmd, args)
		r.Error(err)
		r.Equal("unrecognized GRPC service requested: illegal", err.Error())
	}
	str, err := testArgs(app, "--grpc-port-new", "1234", "--grpc", "illegal")
	r.NoError(err)
	r.Empty(str)
	// This should still be set
	r.Equal(1234, app.Config.API.NewGrpcServerPort)
	r.Equal(false, app.Config.API.StartNodeService)

	resetFlags()

	// Try enabling two services, one with a legal name and one with an illegal name
	// In this case, the node service will be enabled because it comes first
	// Uses Cmd.Run as defined above
	str, err = testArgs(app, "--grpc-port-new", "1234", "--grpc", "node", "--grpc", "illegal")
	r.NoError(err)
	r.Empty(str)
	r.Equal(true, app.Config.API.StartNodeService)

	resetFlags()

	// Try the same thing but change the order of the flags
	// In this case, the node service will not be enabled because it comes second
	// Uses Cmd.Run as defined above
	str, err = testArgs(app, "--grpc", "illegal", "--grpc-port-new", "1234", "--grpc", "node")
	r.NoError(err)
	r.Empty(str)
	r.Equal(false, app.Config.API.StartNodeService)

	resetFlags()

	// Use commas instead
	// In this case, the node service will be enabled because it comes first
	// Uses Cmd.Run as defined above
	str, err = testArgs(app, "--grpc", "node,illegal", "--grpc-port-new", "1234")
	r.NoError(err)
	r.Empty(str)
	r.Equal(true, app.Config.API.StartNodeService)

	resetFlags()

	// This should work
	Cmd.Run = func(cmd *cobra.Command, args []string) {
		r.NoError(app.Initialize(cmd, args))
	}
	str, err = testArgs(app, "--grpc", "node")
	r.Empty(str)
	r.NoError(err)
	r.Equal(true, app.Config.API.StartNodeService)

	resetFlags()

	// This should work too
	Cmd.Run = func(cmd *cobra.Command, args []string) {
		r.NoError(app.Initialize(cmd, args))
	}
	str, err = testArgs(app, "--grpc", "node,node")
	r.Empty(str)
	r.NoError(err)
	r.Equal(true, app.Config.API.StartNodeService)
}

func TestSpacemeshApp_JsonFlags(t *testing.T) {
	resetFlags()

	r := require.New(t)
	app := NewSpacemeshApp()
	r.Equal(9093, app.Config.API.NewJSONServerPort)
	r.Equal(false, app.Config.API.StartNewJSONServer)
	r.Equal(false, app.Config.API.StartNodeService)

	// Try enabling just the JSON service (without the GRPC service)
	Cmd.Run = func(cmd *cobra.Command, args []string) {
		err := app.Initialize(cmd, args)
		//r.NoError(err)
		r.Error(err)
		r.Equal("must enable at least one GRPC service along with JSON gateway service", err.Error())
	}
	str, err := testArgs(app, "--json-server-new")
	r.NoError(err)
	r.Empty(str)
	r.Equal(true, app.Config.API.StartNewJSONServer)
	r.Equal(false, app.Config.API.StartNodeService)

	resetFlags()

	// Try enabling both the JSON and the GRPC services
	Cmd.Run = func(cmd *cobra.Command, args []string) {
		r.NoError(app.Initialize(cmd, args))
	}
	str, err = testArgs(app, "--grpc", "node", "--json-server-new")
	r.NoError(err)
	r.Empty(str)
	r.Equal(true, app.Config.API.StartNodeService)
	r.Equal(true, app.Config.API.StartNewJSONServer)

	resetFlags()

	// Try changing the port
	// Uses Cmd.Run as defined above
	str, err = testArgs(app, "--json-port-new", "1234")
	r.NoError(err)
	r.Empty(str)
	r.Equal(false, app.Config.API.StartNodeService)
	r.Equal(false, app.Config.API.StartNewJSONServer)
	r.Equal(1234, app.Config.API.NewJSONServerPort)
}

type PostMock struct {
}

func (PostMock) Reset() error {
	return nil
}

type NetMock struct {
}

func (NetMock) SubscribePeerEvents() (conn, disc chan p2pcrypto.PublicKey) {
	return nil, nil
}
func (NetMock) Broadcast(string, []byte) error {
	return nil
}

func marshalProto(t *testing.T, msg proto.Message) string {
	var buf bytes.Buffer
	var m jsonpb.Marshaler
	require.NoError(t, m.Marshal(&buf, msg))
	return buf.String()
}

func callEndpoint(t *testing.T, endpoint, payload string, port int) (string, int) {
	url := fmt.Sprintf("http://127.0.0.1:%d/%s", port, endpoint)
	resp, err := http.Post(url, "application/json", strings.NewReader(payload))
	require.NoError(t, err)
	require.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	buf, err := ioutil.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	return string(buf), resp.StatusCode
}

func TestSpacemeshApp_GrpcService(t *testing.T) {
	resetFlags()

	r := require.New(t)
	app := NewSpacemeshApp()

	// Make sure the service is not running by default
	Cmd.Run = func(cmd *cobra.Command, args []string) {
		r.NoError(app.Initialize(cmd, args))
		app.startAPIServices(PostMock{}, NetMock{})
	}
	defer app.stopServices()
	str, err := testArgs(app) // no args
	r.Empty(str)
	r.NoError(err)
	r.Equal(false, app.Config.API.StartNodeService)

	// Try talking to the server
	const message = "Hello World"

	// Set up a connection to the server.
	conn, err := grpc.Dial("localhost:1234", grpc.WithInsecure())
	r.NoError(err)
	c := pb.NewNodeServiceClient(conn)

	// We expect this one to fail
	response, err := c.Echo(context.Background(), &pb.EchoRequest{
		Msg: &pb.SimpleString{Value: message}})
	r.Error(err)
	r.Equal("rpc error: code = Unavailable desc = connection error: desc = \"transport: Error while dialing dial tcp 127.0.0.1:1234: connect: connection refused\"", err.Error())
	r.NoError(conn.Close())

	resetFlags()

	// Test starting the server from the commandline
	// uses Cmd.Run from above
	str, err = testArgs(app, "--grpc-port-new", "1234", "--grpc", "node")
	r.Empty(str)
	r.NoError(err)
	r.Equal(1234, app.Config.API.NewGrpcServerPort)
	r.Equal(true, app.Config.API.StartNodeService)

	// Set up a new connection to the server
	conn, err = grpc.Dial("localhost:1234", grpc.WithInsecure())
	defer func() {
		r.NoError(conn.Close())
	}()
	r.NoError(err)
	c = pb.NewNodeServiceClient(conn)

	// call echo and validate result
	// We expect this one to succeed
	response, err = c.Echo(context.Background(), &pb.EchoRequest{
		Msg: &pb.SimpleString{Value: message}})
	r.NoError(err)
	r.Equal(message, response.Msg.Value)
}

func TestSpacemeshApp_JsonService(t *testing.T) {
	resetFlags()

	r := require.New(t)
	app := NewSpacemeshApp()

	// Make sure the service is not running by default
	Cmd.Run = func(cmd *cobra.Command, args []string) {
		r.NoError(app.Initialize(cmd, args))
		app.startAPIServices(PostMock{}, NetMock{})
	}
	defer app.stopServices()
	str, err := testArgs(app)
	r.Empty(str)
	r.NoError(err)
	r.Equal(false, app.Config.API.StartNewJSONServer)
	r.Equal(false, app.Config.API.StartNodeService)

	// Try talking to the server
	const message = "nihao shijie"

	// generate request payload (api input params)
	payload := marshalProto(t, &pb.EchoRequest{Msg: &pb.SimpleString{Value: message}})

	// We expect this one to fail
	url := fmt.Sprintf("http://127.0.0.1:%d/%s", app.Config.API.NewJSONServerPort, "v1/node/echo")
	_, err = http.Post(url, "application/json", strings.NewReader(payload))
	r.Error(err)
	r.Contains(err.Error(), fmt.Sprintf(
		"dial tcp 127.0.0.1:%d: connect: connection refused",
		app.Config.API.NewJSONServerPort))

	resetFlags()

	// Test starting the JSON server from the commandline
	// uses Cmd.Run from above
	str, err = testArgs(app, "--json-server-new", "--grpc", "node", "--json-port-new", "1234")
	r.Empty(str)
	r.NoError(err)
	r.Equal(1234, app.Config.API.NewJSONServerPort)
	r.Equal(true, app.Config.API.StartNewJSONServer)
	r.Equal(true, app.Config.API.StartNodeService)

	// Give the server a chance to start up
	time.Sleep(2 * time.Second)

	// We expect this one to succeed
	respBody, respStatus := callEndpoint(t, "v1/node/echo", payload, 1234)
	var msg pb.EchoResponse
	r.NoError(jsonpb.UnmarshalString(respBody, &msg))
	r.Equal(message, msg.Msg.Value)
	require.Equal(t, http.StatusOK, respStatus)
	require.NoError(t, jsonpb.UnmarshalString(respBody, &msg))
	require.Equal(t, message, msg.Msg.Value)
}
