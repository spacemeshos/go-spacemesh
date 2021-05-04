package node

import (
	"bytes"
	"context"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/stretchr/testify/assert"
	"io"
	"io/ioutil"
	inet "net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	apiConfig "github.com/spacemeshos/go-spacemesh/api/config"
	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func tempDir() (dir string, cleanup func() error, err error) {
	path, err := ioutil.TempDir("/tmp", "datadir_")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() error {
		return os.RemoveAll(path)
	}
	return path, cleanup, err
}

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

func newLogger(buf *bytes.Buffer) log.Log {
	lvl := zap.NewAtomicLevelAt(zapcore.InfoLevel)
	syncer := zapcore.AddSync(buf)
	encConfig := zap.NewDevelopmentEncoderConfig()
	encConfig.TimeKey = ""
	enc := zapcore.NewConsoleEncoder(encConfig)
	core := zapcore.NewCore(enc, syncer, lvl)
	return log.NewFromLog(zap.New(core))
}

func TestSpacemeshApp_SetLoggers(t *testing.T) {
	r := require.New(t)

	var buf1, buf2 bytes.Buffer

	app := NewSpacemeshApp()
	mylogger := "anton"
	myLog := newLogger(&buf1)
	myLog2 := newLogger(&buf2)

	app.log = app.addLogger(mylogger, myLog)
	msg1 := "hi there"
	app.log.Info(msg1)
	r.Equal(fmt.Sprintf("INFO\t%-13s\t%s\n", mylogger, msg1), buf1.String())
	r.NoError(app.SetLogLevel(mylogger, "warn"))
	r.Equal("warn", app.loggers[mylogger].String())
	buf1.Reset()

	msg1 = "other logger"
	myLog2.Info(msg1)
	msg2 := "hi again"
	msg3 := "be careful"
	// This one should not be printed
	app.log.Info(msg2)
	// This one should be printed
	app.log.Warning(msg3)
	r.Equal(fmt.Sprintf("WARN\t%-13s\t%s\n", mylogger, msg3), buf1.String())
	r.Equal(fmt.Sprintf("INFO\t%s\n", msg1), buf2.String())
	buf1.Reset()

	r.NoError(app.SetLogLevel(mylogger, "info"))

	msg4 := "nihao"
	app.log.Info(msg4)
	r.Equal("info", app.loggers[mylogger].String())
	r.Equal(fmt.Sprintf("INFO\t%-13s\t%s\n", mylogger, msg4), buf1.String())

	// test bad logger name
	r.Error(app.SetLogLevel("anton3", "warn"))

	// test bad loglevel
	r.Error(app.SetLogLevel(mylogger, "lulu"))
	r.Equal("info", app.loggers[mylogger].String())
}

func TestSpacemeshApp_AddLogger(t *testing.T) {
	r := require.New(t)

	var buf bytes.Buffer

	lg := newLogger(&buf)
	app := NewSpacemeshApp()
	mylogger := "anton"
	subLogger := app.addLogger(mylogger, lg)
	subLogger.Debug("should not get printed")
	teststr := "should get printed"
	subLogger.Info(teststr)
	r.Equal(fmt.Sprintf("INFO\t%-13s\t%s\n", mylogger, teststr), buf.String())
}

func testArgs(args ...string) (string, error) {
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
	str, err := testArgs("illegal")
	r.Error(err)
	r.Equal(expected, err.Error())
	r.Equal(expected2, str)

	// Test a legal flag
	Cmd.Run = func(cmd *cobra.Command, args []string) {
		r.NoError(app.Initialize(cmd, args))
	}
	str, err = testArgs("--test-mode")

	// We must manually disable this, it's a global and simply removing the flag isn't sufficient
	defer log.JSONLog(false)

	r.NoError(err)
	r.Empty(str)
	r.Equal(true, app.Config.TestMode)
}

// This must be called in between each test that changes flags
func resetFlags() {
	Cmd.ResetFlags()
	cmdp.AddCommands(Cmd)
}

func setup() {
	// Reset the shutdown context
	// oof, globals make testing really difficult
	ctx, cancel := context.WithCancel(context.Background())
	cmdp.Ctx = ctx
	cmdp.Cancel = cancel

	events.CloseEventReporter()
	resetFlags()
}

func TestSpacemeshApp_GrpcFlags(t *testing.T) {
	setup()

	// Use a unique port
	port := 1244

	r := require.New(t)
	app := NewSpacemeshApp()
	r.Equal(9092, app.Config.API.GrpcServerPort)
	r.Equal(false, app.Config.API.StartNodeService)

	// Try enabling an illegal service
	Cmd.Run = func(cmd *cobra.Command, args []string) {
		err := app.Initialize(cmd, args)
		r.Error(err)
		r.Equal("unrecognized GRPC service requested: illegal", err.Error())
	}
	str, err := testArgs("--grpc-port", strconv.Itoa(port), "--grpc", "illegal")
	r.NoError(err)
	r.Empty(str)
	// This should still be set
	r.Equal(port, app.Config.API.GrpcServerPort)
	r.Equal(false, app.Config.API.StartNodeService)

	resetFlags()
	events.CloseEventReporter()

	// Try enabling two services, one with a legal name and one with an illegal name
	// In this case, the node service will be enabled because it comes first
	// Uses Cmd.Run as defined above
	str, err = testArgs("--grpc-port", strconv.Itoa(port), "--grpc", "node", "--grpc", "illegal")
	r.NoError(err)
	r.Empty(str)
	r.Equal(true, app.Config.API.StartNodeService)

	resetFlags()
	events.CloseEventReporter()

	// Try the same thing but change the order of the flags
	// In this case, the node service will not be enabled because it comes second
	// Uses Cmd.Run as defined above
	str, err = testArgs("--grpc", "illegal", "--grpc-port", strconv.Itoa(port), "--grpc", "node")
	r.NoError(err)
	r.Empty(str)
	r.Equal(false, app.Config.API.StartNodeService)

	resetFlags()
	events.CloseEventReporter()

	// Use commas instead
	// In this case, the node service will be enabled because it comes first
	// Uses Cmd.Run as defined above
	str, err = testArgs("--grpc", "node,illegal", "--grpc-port", strconv.Itoa(port))
	r.NoError(err)
	r.Empty(str)
	r.Equal(true, app.Config.API.StartNodeService)

	resetFlags()
	events.CloseEventReporter()

	// This should work
	Cmd.Run = func(cmd *cobra.Command, args []string) {
		r.NoError(app.Initialize(cmd, args))
	}
	str, err = testArgs("--grpc", "node")
	r.Empty(str)
	r.NoError(err)
	r.Equal(true, app.Config.API.StartNodeService)

	resetFlags()
	events.CloseEventReporter()

	// This should work too
	str, err = testArgs("--grpc", "node,node")
	r.Empty(str)
	r.NoError(err)
	r.Equal(true, app.Config.API.StartNodeService)

	// Test enabling two services both ways

	// Reset flags and config
	resetFlags()
	events.CloseEventReporter()
	app.Config.API = apiConfig.DefaultTestConfig()

	r.Equal(false, app.Config.API.StartNodeService)
	r.Equal(false, app.Config.API.StartMeshService)
	str, err = testArgs("--grpc", "node,mesh")
	r.Empty(str)
	r.NoError(err)
	r.Equal(true, app.Config.API.StartNodeService)
	r.Equal(true, app.Config.API.StartMeshService)

	// Reset flags and config
	resetFlags()
	events.CloseEventReporter()
	app.Config.API = apiConfig.DefaultTestConfig()

	r.Equal(false, app.Config.API.StartNodeService)
	r.Equal(false, app.Config.API.StartMeshService)
	str, err = testArgs("--grpc", "node", "--grpc", "mesh")
	r.Empty(str)
	r.NoError(err)
	r.Equal(true, app.Config.API.StartNodeService)
	r.Equal(true, app.Config.API.StartMeshService)
}

func TestSpacemeshApp_JsonFlags(t *testing.T) {
	setup()

	r := require.New(t)
	app := NewSpacemeshApp()
	r.Equal(9093, app.Config.API.JSONServerPort)
	r.Equal(false, app.Config.API.StartJSONServer)
	r.Equal(false, app.Config.API.StartNodeService)

	// Try enabling just the JSON service (without the GRPC service)
	Cmd.Run = func(cmd *cobra.Command, args []string) {
		err := app.Initialize(cmd, args)
		r.Error(err)
		r.Equal("must enable at least one GRPC service along with JSON gateway service", err.Error())
	}
	str, err := testArgs("--json-server")
	r.NoError(err)
	r.Empty(str)
	r.Equal(true, app.Config.API.StartJSONServer)
	r.Equal(false, app.Config.API.StartNodeService)

	resetFlags()
	events.CloseEventReporter()

	// Try enabling both the JSON and the GRPC services
	Cmd.Run = func(cmd *cobra.Command, args []string) {
		r.NoError(app.Initialize(cmd, args))
	}
	str, err = testArgs("--grpc", "node", "--json-server")
	r.NoError(err)
	r.Empty(str)
	r.Equal(true, app.Config.API.StartNodeService)
	r.Equal(true, app.Config.API.StartJSONServer)

	resetFlags()
	events.CloseEventReporter()

	// Try changing the port
	// Uses Cmd.Run as defined above
	str, err = testArgs("--json-port", "1234")
	r.NoError(err)
	r.Empty(str)
	r.Equal(false, app.Config.API.StartNodeService)
	r.Equal(false, app.Config.API.StartJSONServer)
	r.Equal(1234, app.Config.API.JSONServerPort)
}

type NetMock struct {
}

func (NetMock) SubscribePeerEvents() (conn, disc chan p2pcrypto.PublicKey) {
	return nil, nil
}
func (NetMock) Broadcast(context.Context, string, []byte) error {
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
	setup()

	// Use a unique port
	port := 1242

	r := require.New(t)
	app := NewSpacemeshApp()

	path, cleanup, err := tempDir()
	r.NoError(err)
	defer cleanup()

	Cmd.Run = func(cmd *cobra.Command, args []string) {
		r.NoError(app.Initialize(cmd, args))
		app.Config.API.GrpcServerPort = port
		app.Config.DataDirParent = path
		app.startAPIServices(context.TODO(), NetMock{})
	}
	defer app.stopServices()

	// Make sure the service is not running by default
	str, err := testArgs() // no args
	r.Empty(str)
	r.NoError(err)
	r.Equal(false, app.Config.API.StartNodeService)

	// Give the services a few seconds to start running - this is important on CI.
	time.Sleep(2 * time.Second)

	// Try talking to the server
	const message = "Hello World"

	// Set up a connection to the server.
	conn, err := grpc.Dial(fmt.Sprintf("localhost:%d", port), grpc.WithInsecure())
	r.NoError(err)
	c := pb.NewNodeServiceClient(conn)

	// We expect this one to fail
	response, err := c.Echo(context.Background(), &pb.EchoRequest{
		Msg: &pb.SimpleString{Value: message}})
	r.Error(err)
	r.Contains(err.Error(), "rpc error: code = Unavailable desc = connection error: desc = \"transport: Error while dialing dial tcp")
	r.NoError(conn.Close())

	resetFlags()
	events.CloseEventReporter()

	// Test starting the server from the commandline
	// uses Cmd.Run from above
	str, err = testArgs("--grpc-port", strconv.Itoa(port), "--grpc", "node")
	r.Empty(str)
	r.NoError(err)
	r.Equal(port, app.Config.API.GrpcServerPort)
	r.Equal(true, app.Config.API.StartNodeService)

	// Give the services a few seconds to start running - this is important on CI.
	time.Sleep(2 * time.Second)

	// Set up a new connection to the server
	conn, err = grpc.Dial(fmt.Sprintf("localhost:%d", port), grpc.WithInsecure())
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
	setup()

	r := require.New(t)
	app := NewSpacemeshApp()

	path, cleanup, err := tempDir()
	r.NoError(err)
	defer cleanup()

	// Make sure the service is not running by default
	Cmd.Run = func(cmd *cobra.Command, args []string) {
		r.NoError(app.Initialize(cmd, args))
		app.Config.DataDirParent = path
		app.startAPIServices(context.TODO(), NetMock{})
	}
	defer app.stopServices()
	str, err := testArgs()
	r.Empty(str)
	r.NoError(err)
	r.Equal(false, app.Config.API.StartJSONServer)
	r.Equal(false, app.Config.API.StartNodeService)
	r.Equal(false, app.Config.API.StartMeshService)
	r.Equal(false, app.Config.API.StartGlobalStateService)
	r.Equal(false, app.Config.API.StartSmesherService)
	r.Equal(false, app.Config.API.StartTransactionService)

	// Try talking to the server
	const message = "nihao shijie"

	// generate request payload (api input params)
	payload := marshalProto(t, &pb.EchoRequest{Msg: &pb.SimpleString{Value: message}})

	// We expect this one to fail
	url := fmt.Sprintf("http://127.0.0.1:%d/%s", app.Config.API.JSONServerPort, "v1/node/echo")
	_, err = http.Post(url, "application/json", strings.NewReader(payload))
	r.Error(err)
	r.Contains(err.Error(), fmt.Sprintf(
		"dial tcp 127.0.0.1:%d: connect: connection refused",
		app.Config.API.JSONServerPort))

	resetFlags()
	events.CloseEventReporter()

	// Test starting the JSON server from the commandline
	// uses Cmd.Run from above
	str, err = testArgs("--json-server", "--grpc", "node", "--json-port", "1234")
	r.Empty(str)
	r.NoError(err)
	r.Equal(1234, app.Config.API.JSONServerPort)
	r.Equal(true, app.Config.API.StartJSONServer)
	r.Equal(true, app.Config.API.StartNodeService)

	// Give the server a chance to start up
	time.Sleep(2 * time.Second)

	// We expect this one to succeed
	respBody, respStatus := callEndpoint(t, "v1/node/echo", payload, app.Config.API.JSONServerPort)
	log.Info("Got echo response: %v", respBody)
	var msg pb.EchoResponse
	r.NoError(jsonpb.UnmarshalString(respBody, &msg))
	r.Equal(message, msg.Msg.Value)
	require.Equal(t, http.StatusOK, respStatus)
	require.NoError(t, jsonpb.UnmarshalString(respBody, &msg))
	require.Equal(t, message, msg.Msg.Value)
}

// E2E app test of the stream endpoints in the NodeService
func TestSpacemeshApp_NodeService(t *testing.T) {
	setup()

	// Use a unique port
	port := 1240

	path, cleanup, err := tempDir()
	require.NoError(t, err)
	defer cleanup()

	clock := timesync.NewClock(timesync.RealClock{}, time.Duration(1)*time.Second, time.Now(), log.NewDefault("clock"))
	net := service.NewSimulator()
	cfg := getTestDefaultConfig(1)
	poetHarness, err := activation.NewHTTPPoetHarness(false)
	assert.NoError(t, err)
	app, err := InitSingleInstance(*cfg, 0, time.Now().Add(1*time.Second).Format(time.RFC3339), path, eligibility.New(), poetHarness.HTTPPoetClient, clock, net)

	//app := NewSpacemeshApp()

	Cmd.Run = func(cmd *cobra.Command, args []string) {
		defer app.Cleanup(cmd, args)
		require.NoError(t, app.Initialize(cmd, args))

		// Give the error channel a buffer
		events.CloseEventReporter()
		require.NoError(t, events.InitializeEventReporterWithOptions("", 10, true))

		// Speed things up a little
		app.Config.SyncInterval = 1
		app.Config.LayerDurationSec = 2
		app.Config.DataDirParent = path

		// This will block. We need to run the full app here to make sure that
		// the various services are reporting events correctly. This could probably
		// be done more surgically, and we don't need _all_ of the services.
		app.Start(cmd, args)
	}

	// Run the app in a goroutine. As noted above, it blocks if it succeeds.
	// If there's an error in the args, it will return immediately.
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		// This makes sure the test doesn't end until this goroutine closes
		defer wg.Done()
		str, err := testArgs("--grpc-port", strconv.Itoa(port), "--grpc", "node", "--acquire-port=false", "--tcp-interface", "127.0.0.1", "--grpc-interface", "localhost")
		require.Empty(t, str)
		require.NoError(t, err)
	}()

	// Wait for the app and services to start
	// Strictly speaking, this does not indicate that all of the services
	// have started, we could add separate channels for that.
	<-app.started

	// Unfortunately sometimes we need to wait even longer
	time.Sleep(3 * time.Second)

	// Set up a new connection to the server
	addr := fmt.Sprintf("localhost:%d", port)
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	defer func() {
		require.NoError(t, conn.Close())
	}()
	require.NoError(t, err)
	c := pb.NewNodeServiceClient(conn)

	// Use a channel to coordinate ending the test
	end := make(chan struct{})

	go func() {
		// Don't close the channel twice
		var once sync.Once
		oncebody := func() { close(end) }
		defer once.Do(oncebody)

		// Open an error stream and a status stream
		streamErr, err := c.ErrorStream(context.Background(), &pb.ErrorStreamRequest{})
		require.NoError(t, err)

		nextError := func() *pb.NodeError {
			in, err := streamErr.Recv()
			require.NoError(t, err)
			log.Info("Got streamed error: %v", in.Error)
			return in.Error
		}

		// We expect a specific series of errors in a specific order!
		// Check each one
		var myError *pb.NodeError

		myError = nextError()

		// Ignore this error which happens if you have a local database file
		if strings.Contains(myError.Msg, "error adding genesis block, block already exists in database") {
			myError = nextError()
		}

		require.Equal(t, "test123", myError.Msg)
		require.Equal(t, pb.LogLevel_LOG_LEVEL_ERROR, myError.Level)

		myError = nextError()
		require.Equal(t, "test456", myError.Msg)
		require.Equal(t, pb.LogLevel_LOG_LEVEL_ERROR, myError.Level)

		// The panic gets wrapped in an ERROR, and we get both, in this order
		myError = nextError()
		require.Contains(t, myError.Msg, "Fatal: goroutine panicked.")
		require.Equal(t, pb.LogLevel_LOG_LEVEL_ERROR, myError.Level)

		myError = nextError()
		require.Equal(t, "testPANIC", myError.Msg)
		require.Equal(t, pb.LogLevel_LOG_LEVEL_PANIC, myError.Level)

		// Let the test end
		once.Do(oncebody)
	}()

	wg.Add(1)
	go func() {
		// This makes sure the test doesn't end until this goroutine closes
		defer wg.Done()

		streamStatus, err := c.StatusStream(context.Background(), &pb.StatusStreamRequest{})
		require.NoError(t, err)

		// We don't really control the order in which these are received,
		// unlike the errorStream. So just loop and listen here while the
		// app is running, and make sure there are no errors.
		for {
			in, err := streamStatus.Recv()
			if err == io.EOF {
				return
			}
			errCode := status.Code(err)
			// We expect this to happen when the server disconnects
			if errCode == codes.Unavailable {
				return
			}
			require.NoError(t, err)

			// Note that, for some reason, protobuf does not display fields
			// that still have their default value, so the output here will
			// only be partial.
			log.Info("Got status message: %s", in.Status)

			// Check if the test should end
			select {
			case <-end:
				return
			default:
				continue
			}
		}
	}()
	time.Sleep(4 * time.Second)

	// Report two errors and make sure they're both received
	log.Error("test123")
	log.Error("test456")
	time.Sleep(4 * time.Second)

	// Trap a panic
	func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Println("Recovered", r)
			}
		}()
		log.Panic("testPANIC")
	}()

	// Wait for messages to be received
	<-end

	// This stops the app
	cmdp.Cancel()

	// Wait for everything to stop cleanly before ending test
	wg.Wait()
}

// E2E app test of the transaction service
func TestSpacemeshApp_TransactionService(t *testing.T) {
	setup()
	r := require.New(t)

	// Use a unique port
	port := 1236

	// Use a unique dir for data so we don't read existing state
	path, cleanup, err := tempDir()
	r.NoError(err, "error creating tempdir")
	defer cleanup()

	app := NewSpacemeshApp()
	cfg := config.DefaultTestConfig()
	app.Config = &cfg

	Cmd.Run = func(cmd *cobra.Command, args []string) {
		defer app.Cleanup(cmd, args)
		r.NoError(app.Initialize(cmd, args))

		app.Config.DataDirParent = path

		// GRPC configuration
		app.Config.API.GrpcServerPort = port
		app.Config.API.GrpcServerInterface = "localhost"
		app.Config.API.StartTransactionService = true

		// Prevent obnoxious warning in macOS
		app.Config.P2P.AcquirePort = false
		app.Config.P2P.TCPInterface = "127.0.0.1"

		// Avoid waiting for new connections.
		app.Config.P2P.SwarmConfig.RandomConnections = 0

		// Speed things up a little
		app.Config.SyncInterval = 1
		app.Config.LayerDurationSec = 2

		// Force gossip to always listen, even when not synced
		app.Config.AlwaysListen = true

		app.Config.GenesisTime = time.Now().Add(20 * time.Second).Format(time.RFC3339)

		// This will block. We need to run the full app here to make sure that
		// the various services are reporting events correctly. This could probably
		// be done more surgically, and we don't need _all_ of the services.
		app.Start(cmd, args)
	}

	// Run the app in a goroutine. As noted above, it blocks if it succeeds.
	// If there's an error in the args, it will return immediately.
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		str, err := testArgs()
		r.Empty(str)
		r.NoError(err)
		wg.Done()
	}()

	// Wait for the app and services to start
	// Strictly speaking, this does not indicate that all of the services
	// have started, we could add separate channels for that, but it seems
	// to work well enough for testing.
	<-app.started
	time.Sleep(2 * time.Second)

	// Set up a new connection to the server
	addr := fmt.Sprintf("localhost:%d", port)
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	defer func() {
		r.NoError(conn.Close())
	}()
	r.NoError(err)
	c := pb.NewTransactionServiceClient(conn)

	// Construct some mock tx data
	// The tx origin must be the hardcoded test account or else it will have no balance.
	signer, err := signing.NewEdSignerFromBuffer(util.FromHex(apiConfig.Account1Private))
	require.NoError(t, err)
	txorigin := types.Address{}
	txorigin.SetBytes(signer.PublicKey().Bytes())
	dst := types.BytesToAddress([]byte{0x02})
	tx, err := types.NewSignedTx(0, dst, 10, 1, 1, signer)
	require.NoError(t, err, "unable to create signed mock tx")
	txbytes, _ := types.InterfaceToBytes(tx)

	// Coordinate ending the test
	wg2 := sync.WaitGroup{}
	wg2.Add(1)
	go func() {
		// Make sure the channel is closed if we encounter an unexpected
		// error, but don't close the channel twice if we don't!
		var once sync.Once
		oncebody := func() { wg2.Done() }
		defer once.Do(oncebody)

		// Open a transaction stream
		stream, err := c.TransactionsStateStream(context.Background(), &pb.TransactionsStateStreamRequest{
			TransactionId:       []*pb.TransactionId{{Id: tx.ID().Bytes()}},
			IncludeTransactions: true,
		})
		require.NoError(t, err)

		// Now listen on the stream
		res, err := stream.Recv()
		require.NoError(t, err)
		require.Equal(t, tx.ID().Bytes(), res.TransactionState.Id.Id)

		// We expect the tx to go to the mempool
		inTx := res.Transaction
		require.Equal(t, pb.TransactionState_TRANSACTION_STATE_MEMPOOL, res.TransactionState.State)
		require.Equal(t, tx.ID().Bytes(), inTx.Id.Id)
		require.Equal(t, tx.Origin().Bytes(), inTx.Sender.Address)
		require.Equal(t, tx.GasLimit, inTx.GasOffered.GasProvided)
		require.Equal(t, tx.Amount, inTx.Amount.Value)
		require.Equal(t, tx.AccountNonce, inTx.Counter)
		require.Equal(t, tx.Origin().Bytes(), inTx.Signature.PublicKey)
		switch x := inTx.Datum.(type) {
		case *pb.Transaction_CoinTransfer:
			require.Equal(t, tx.Recipient.Bytes(), x.CoinTransfer.Receiver.Address,
				"inner coin transfer tx has bad recipient")
		default:
			require.Fail(t, "inner tx has wrong tx data type")
		}

		// Let the test end
		once.Do(oncebody)

		// Wait for the app to exit
		wg.Wait()
	}()

	time.Sleep(4 * time.Second)

	// Submit the tx
	res, err := c.SubmitTransaction(context.Background(), &pb.SubmitTransactionRequest{
		Transaction: txbytes,
	})
	require.NoError(t, err)
	require.Equal(t, int32(code.Code_OK), res.Status.Code)
	require.Equal(t, tx.ID().Bytes(), res.Txstate.Id.Id)
	require.Equal(t, pb.TransactionState_TRANSACTION_STATE_MEMPOOL, res.Txstate.State)

	// Wait for messages to be received
	wg2.Wait()

	// This stops the app
	cmdp.Cancel()

	// Wait for it to stop
	wg.Wait()
}

func TestSpacemeshApp_P2PInterface(t *testing.T) {
	setup()

	// Use a unique port
	port := 1238
	addr := "127.0.0.1"

	r := require.New(t)
	app := NewSpacemeshApp()

	// Initialize the network: we don't want to listen but this lets us dial out
	l, err := node.NewNodeIdentity()
	r.NoError(err)
	p2pnet, err := net.NewNet(app.Config.P2P, l, log.AppLog)
	r.NoError(err)
	// We need to listen on a different port
	listener, err := inet.Listen("tcp", fmt.Sprintf("%s:%d", addr, 9270))
	r.NoError(err)
	p2pnet.Start(context.TODO(), listener)
	defer p2pnet.Shutdown()

	// Try to connect before we start the P2P service: this should fail
	tcpAddr := inet.TCPAddr{IP: inet.ParseIP(addr), Port: port}
	_, err = p2pnet.Dial(cmdp.Ctx, &tcpAddr, l.PublicKey())
	r.Error(err)

	// Start P2P services
	app.Config.P2P.TCPPort = port
	app.Config.P2P.TCPInterface = addr
	app.Config.P2P.AcquirePort = false
	swarm, err := p2p.New(cmdp.Ctx, app.Config.P2P, log.AppLog, app.Config.DataDir())
	r.NoError(err)
	r.NoError(swarm.Start(context.TODO()))
	defer swarm.Shutdown()

	// Try to connect again: this should succeed
	conn, err := p2pnet.Dial(cmdp.Ctx, &tcpAddr, l.PublicKey())
	r.NoError(err)
	defer conn.Close()
	r.Equal(fmt.Sprintf("%s:%d", addr, app.Config.P2P.TCPPort), conn.RemoteAddr().String())
	r.Equal(l.PublicKey(), conn.RemotePublicKey())
}
