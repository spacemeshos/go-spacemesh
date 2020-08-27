package node

import (
	"bytes"
	"context"
	"fmt"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/api/config"
	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
	"io/ioutil"
	inet "net"
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

func TestSpacemeshApp_AddLogger(t *testing.T) {
	r := require.New(t)

	// construct a fake logger so we can read from it directly
	var buf bytes.Buffer
	lvl := zap.NewAtomicLevelAt(zapcore.InfoLevel)
	syncer := zapcore.AddSync(&buf)
	encConfig := zap.NewDevelopmentEncoderConfig()
	encConfig.TimeKey = ""
	enc := zapcore.NewConsoleEncoder(encConfig)
	core := zapcore.NewCore(enc, syncer, zap.LevelEnablerFunc(lvl.Enabled))
	lg := log.NewFromLog(zap.New(core))

	app := NewSpacemeshApp()
	mylogger := "anton"
	subLogger := app.addLogger(mylogger, lg)
	subLogger.Debug("should not get printed")
	teststr := "should get printed"
	subLogger.Info(teststr)
	r.Equal(fmt.Sprintf("INFO\t%-13s\t%s\n", mylogger, teststr), buf.String())
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
	str, err = testArgs(app, "--grpc", "node,node")
	r.Empty(str)
	r.NoError(err)
	r.Equal(true, app.Config.API.StartNodeService)

	// Test enabling two services both ways

	// Reset flags and config
	resetFlags()
	app.Config.API = config.DefaultConfig()

	r.Equal(false, app.Config.API.StartNodeService)
	r.Equal(false, app.Config.API.StartMeshService)
	str, err = testArgs(app, "--grpc", "node,mesh")
	r.Empty(str)
	r.NoError(err)
	r.Equal(true, app.Config.API.StartNodeService)
	r.Equal(true, app.Config.API.StartMeshService)

	// Reset flags and config
	resetFlags()
	app.Config.API = config.DefaultConfig()

	r.Equal(false, app.Config.API.StartNodeService)
	r.Equal(false, app.Config.API.StartMeshService)
	str, err = testArgs(app, "--grpc", "node", "--grpc", "mesh")
	r.Empty(str)
	r.NoError(err)
	r.Equal(true, app.Config.API.StartNodeService)
	r.Equal(true, app.Config.API.StartMeshService)
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

	Cmd.Run = func(cmd *cobra.Command, args []string) {
		r.NoError(app.Initialize(cmd, args))
		app.startAPIServices(PostMock{}, NetMock{})
	}
	defer app.stopServices()

	// Make sure the service is not running by default
	str, err := testArgs(app) // no args
	r.Empty(str)
	r.NoError(err)
	r.Equal(false, app.Config.API.StartNodeService)

	// Give the services a few seconds to start running - this is important on travis.
	time.Sleep(2 * time.Second)

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
	r.Contains(err.Error(), "rpc error: code = Unavailable desc = connection error: desc = \"transport: Error while dialing dial tcp")
	r.NoError(conn.Close())

	resetFlags()

	// Test starting the server from the commandline
	// uses Cmd.Run from above
	str, err = testArgs(app, "--grpc-port-new", "1234", "--grpc", "node")
	r.Empty(str)
	r.NoError(err)
	r.Equal(1234, app.Config.API.NewGrpcServerPort)
	r.Equal(true, app.Config.API.StartNodeService)

	// Give the services a few seconds to start running - this is important on travis.
	time.Sleep(2 * time.Second)

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

func TestSpacemeshApp_P2PInterface(t *testing.T) {
	r := require.New(t)
	app := NewSpacemeshApp()

	addr := "127.0.0.1"
	tcpAddr := inet.TCPAddr{IP: inet.ParseIP(addr), Port: app.Config.P2P.TCPPort}

	// Initialize the network: we don't want to listen but this lets us dial out
	l, err := node.NewNodeIdentity()
	r.NoError(err)
	p2pnet, err := net.NewNet(app.Config.P2P, l, log.AppLog)
	r.NoError(err)
	// We need to listen on a different port
	listener, err := inet.Listen("tcp", fmt.Sprintf("%s:%d", addr, 9270))
	r.NoError(err)
	p2pnet.Start(listener)
	defer p2pnet.Shutdown()

	// Try to connect before we start the P2P service: this should fail
	_, err = p2pnet.Dial(cmdp.Ctx, &tcpAddr, l.PublicKey())
	r.Error(err)

	// Start P2P services
	app.Config.P2P.TCPInterface = addr
	app.Config.P2P.AcquirePort = false
	swarm, err := p2p.New(cmdp.Ctx, app.Config.P2P, log.AppLog, app.Config.DataDir())
	r.NoError(err)
	r.NoError(swarm.Start())
	defer swarm.Shutdown()

	// Try to connect again: this should succeed
	conn, err := p2pnet.Dial(cmdp.Ctx, &tcpAddr, l.PublicKey())
	r.NoError(err)
	defer conn.Close()
	r.Equal(fmt.Sprintf("%s:%d", addr, app.Config.P2P.TCPPort), conn.RemoteAddr().String())
	r.Equal(l.PublicKey(), conn.RemotePublicKey())
}
