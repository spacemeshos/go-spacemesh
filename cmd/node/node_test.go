package node

// Hide deprecated protobuf version error.
// nolint: staticcheck
import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
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
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/activation"
	apiConfig "github.com/spacemeshos/go-spacemesh/api/config"
	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

func TestSpacemeshApp_getEdIdentity(t *testing.T) {
	r := require.New(t)

	defer func() {
		// cleanup
		err := os.RemoveAll("tmp")
		r.NoError(err)
	}()

	tempdir := t.TempDir()

	// setup spacemesh app
	app := New(WithLog(logtest.New(t)))
	app.Config.SMESHING.Opts.DataDir = tempdir
	app.log = logtest.New(t)

	// Create new identity.
	signer1, err := app.LoadOrCreateEdSigner()
	r.NoError(err)
	infos, err := ioutil.ReadDir(tempdir)
	r.NoError(err)
	r.Len(infos, 1)

	// Load existing identity.
	signer2, err := app.LoadOrCreateEdSigner()
	r.NoError(err)
	infos, err = ioutil.ReadDir(tempdir)
	r.NoError(err)
	r.Len(infos, 1)
	r.Equal(signer1.PublicKey(), signer2.PublicKey())

	// Invalidate the identity by changing its file name.
	filename := filepath.Join(tempdir, infos[0].Name())
	err = os.Rename(filename, filename+"_")
	r.NoError(err)

	// Create new identity.
	signer3, err := app.LoadOrCreateEdSigner()
	r.NoError(err)
	infos, err = ioutil.ReadDir(tempdir)
	r.NoError(err)
	r.Len(infos, 2)
	r.NotEqual(signer1.PublicKey(), signer3.PublicKey())
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

	app := New(WithLog(logtest.New(t)))
	mylogger := "anton"
	myLog := newLogger(&buf1)
	myLog2 := newLogger(&buf2)

	app.log = app.addLogger(mylogger, myLog)
	msg1 := "hi there"
	app.log.Info(msg1)
	r.Equal(fmt.Sprintf("INFO\t%-13s\t%s\t{\"module\": \"%s\"}\n", mylogger, msg1, mylogger), buf1.String())
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
	r.Equal(fmt.Sprintf("WARN\t%-13s\t%s\t{\"module\": \"%s\"}\n", mylogger, msg3, mylogger), buf1.String())
	r.Equal(fmt.Sprintf("INFO\t%s\n", msg1), buf2.String())
	buf1.Reset()

	r.NoError(app.SetLogLevel(mylogger, "info"))

	msg4 := "nihao"
	app.log.Info(msg4)
	r.Equal("info", app.loggers[mylogger].String())
	r.Equal(fmt.Sprintf("INFO\t%-13s\t%s\t{\"module\": \"%s\"}\n", mylogger, msg4, mylogger), buf1.String())

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
	app := New(WithLog(logtest.New(t)))
	mylogger := "anton"
	subLogger := app.addLogger(mylogger, lg)
	subLogger.Debug("should not get printed")
	teststr := "should get printed"
	subLogger.Info(teststr)
	r.Equal(fmt.Sprintf("INFO\t%-13s\t%s\t{\"module\": \"%s\"}\n", mylogger, teststr, mylogger), buf.String())
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
	app := New(WithLog(logtest.New(t)))

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
		r.NoError(cmdp.EnsureCLIFlags(cmd, app.Config))
	}
	str, err = testArgs("--test-mode")

	r.NoError(err)
	r.Empty(str)
	r.Equal(true, app.Config.TestMode)
}

// This must be called in between each test that changes flags.
func resetFlags() {
	Cmd.ResetFlags()
	cmdp.AddCommands(Cmd)
}

func setup() {
	// Reset the shutdown context
	// oof, globals make testing really difficult
	ctx, cancel := context.WithCancel(context.Background())

	cmdp.SetCtx(ctx)
	cmdp.SetCancel(cancel)

	events.CloseEventReporter()
	resetFlags()
}

func TestSpacemeshApp_GrpcFlags(t *testing.T) {
	setup()

	// Use a unique port
	port := 1244

	r := require.New(t)
	app := New(WithLog(logtest.New(t)))
	r.Equal(9092, app.Config.API.GrpcServerPort)
	r.Equal(false, app.Config.API.StartNodeService)

	// Try enabling an illegal service
	Cmd.Run = func(cmd *cobra.Command, args []string) {
		err := cmdp.EnsureCLIFlags(cmd, app.Config)
		r.Error(err)
		r.Equal("parse services list: unrecognized GRPC service requested: illegal", err.Error())
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
	app.Config.API = apiConfig.DefaultTestConfig()

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
		r.NoError(cmdp.EnsureCLIFlags(cmd, app.Config))
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
	app := New(WithLog(logtest.New(t)))
	r.Equal(9093, app.Config.API.JSONServerPort)
	r.Equal(false, app.Config.API.StartJSONServer)
	r.Equal(false, app.Config.API.StartNodeService)

	// Try enabling just the JSON service (without the GRPC service)
	Cmd.Run = func(cmd *cobra.Command, args []string) {
		err := cmdp.EnsureCLIFlags(cmd, app.Config)
		r.Error(err)
		r.Equal("parse services list: must enable at least one GRPC service along with JSON gateway service", err.Error())
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
		r.NoError(cmdp.EnsureCLIFlags(cmd, app.Config))
	}
	str, err = testArgs("--grpc", "node", "--json-server")
	r.NoError(err)
	r.Empty(str)
	r.Equal(true, app.Config.API.StartNodeService)
	r.Equal(true, app.Config.API.StartJSONServer)

	resetFlags()
	events.CloseEventReporter()
	app.Config.API = apiConfig.DefaultTestConfig()

	// Try changing the port
	// Uses Cmd.Run as defined above
	str, err = testArgs("--json-port", "1234")
	r.NoError(err)
	r.Empty(str)
	r.Equal(false, app.Config.API.StartNodeService)
	r.Equal(false, app.Config.API.StartJSONServer)
	r.Equal(1234, app.Config.API.JSONServerPort)
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
	app := New(WithLog(logtest.New(t)))

	path := t.TempDir()

	Cmd.Run = func(cmd *cobra.Command, args []string) {
		r.NoError(cmdp.EnsureCLIFlags(cmd, app.Config))
		app.Config.API.GrpcServerPort = port
		app.Config.DataDirParent = path
		app.startAPIServices(context.TODO())
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
	_, err = c.Echo(context.Background(), &pb.EchoRequest{
		Msg: &pb.SimpleString{Value: message},
	})
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
	response, err := c.Echo(context.Background(), &pb.EchoRequest{
		Msg: &pb.SimpleString{Value: message},
	})
	r.NoError(err)
	r.Equal(message, response.Msg.Value)
}

func TestSpacemeshApp_JsonService(t *testing.T) {
	setup()

	r := require.New(t)
	app := New(WithLog(logtest.New(t)))

	path := t.TempDir()

	// Make sure the service is not running by default
	Cmd.Run = func(cmd *cobra.Command, args []string) {
		r.NoError(cmdp.EnsureCLIFlags(cmd, app.Config))
		app.Config.DataDirParent = path
		app.startAPIServices(context.TODO())
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
	var msg pb.EchoResponse
	r.NoError(jsonpb.UnmarshalString(respBody, &msg))
	r.Equal(message, msg.Msg.Value)
	require.Equal(t, http.StatusOK, respStatus)
	require.NoError(t, jsonpb.UnmarshalString(respBody, &msg))
	require.Equal(t, message, msg.Msg.Value)
}

// E2E app test of the stream endpoints in the NodeService.
func TestSpacemeshApp_NodeService(t *testing.T) {
	// errlog should be used only for testing.
	logger := logtest.New(t)
	errlog := log.RegisterHooks(logtest.New(t, zap.ErrorLevel), events.EventHook())
	setup()

	// Use a unique port
	port := 1240

	path := t.TempDir()

	clock := timesync.NewClock(timesync.RealClock{}, time.Duration(1)*time.Second, time.Now(), logtest.New(t))
	mesh, err := mocknet.WithNPeers(context.TODO(), 1)
	require.NoError(t, err)
	cfg := getTestDefaultConfig()

	poetHarness, err := activation.NewHTTPPoetHarness(false)
	require.NoError(t, err)
	edSgn := signing.NewEdSigner()
	h, err := p2p.Upgrade(mesh.Hosts()[0])
	require.NoError(t, err)
	app, err := InitSingleInstance(logger, *cfg, 0, time.Now().Add(1*time.Second).Format(time.RFC3339),
		path, eligibility.New(logtest.New(t)),
		poetHarness.HTTPPoetClient, clock, h, edSgn)
	require.NoError(t, err)

	Cmd.Run = func(cmd *cobra.Command, args []string) {
		defer app.Cleanup()
		require.NoError(t, cmdp.EnsureCLIFlags(cmd, app.Config))

		// Give the error channel a buffer
		events.CloseEventReporter()
		require.NoError(t, events.InitializeEventReporterWithOptions("", 10, true))

		// Speed things up a little
		app.Config.SyncInterval = 1
		app.Config.LayerDurationSec = 2
		app.Config.DataDirParent = path
		app.Config.LOGGING = cfg.LOGGING

		// This will block. We need to run the full app here to make sure that
		// the various services are reporting events correctly. This could probably
		// be done more surgically, and we don't need _all_ of the services.
		require.NoError(t, app.Start())
	}

	// Run the app in a goroutine. As noted above, it blocks if it succeeds.
	// If there's an error in the args, it will return immediately.
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		// This makes sure the test doesn't end until this goroutine closes
		defer wg.Done()
		str, err := testArgs("--grpc-port", strconv.Itoa(port), "--grpc", "node", "--grpc-interface", "localhost")
		assert.Empty(t, str)
		assert.NoError(t, err)
	}()

	// Set up a new connection to the server
	var (
		conn *grpc.ClientConn
	)
	for start := time.Now(); time.Since(start) <= 10*time.Second; {
		conn, err = grpc.Dial(fmt.Sprintf("localhost:%d", port), grpc.WithInsecure(), grpc.WithBlock())
		if err == nil {
			break
		}
	}
	require.NotNil(t, conn)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, conn.Close())
	})
	c := pb.NewNodeServiceClient(conn)

	wg.Add(1)
	go func() {
		defer wg.Done()

		streamStatus, err := c.StatusStream(context.Background(), &pb.StatusStreamRequest{})
		require.NoError(t, err)

		// We don't really control the order in which these are received,
		// unlike the errorStream. So just loop and listen here while the
		// app is running, and make sure there are no errors.
		for {
			in, err := streamStatus.Recv()
			if err != nil {
				code := status.Code(err)
				if code == codes.Unavailable {
					return
				}
				assert.NoError(t, err)
				return
			}
			// Note that, for some reason, protobuf does not display fields
			// that still have their default value, so the output here will
			// only be partial.
			logger.Debug("Got status message: %s", in.Status)
		}
	}()

	streamErr, err := c.ErrorStream(context.Background(), &pb.ErrorStreamRequest{})
	require.NoError(t, err)

	// Report two errors and make sure they're both received
	go func() {
		errlog.Error("test123")
		errlog.Error("test456")
		assert.Panics(t, func() {
			errlog.Panic("testPANIC")
		})
	}()

	// We expect a specific series of errors in a specific order!
	expected := []string{
		"test123",
		"test456",
		"Fatal: goroutine panicked.",
		"testPANIC",
	}
	for _, errmsg := range expected {
		in, err := streamErr.Recv()
		require.NoError(t, err)
		current := in.Error
		require.Contains(t, current.Msg, errmsg)
	}

	// This stops the app
	cmdp.Cancel()() // stop the app

	// Wait for everything to stop cleanly before ending test
	wg.Wait()
}

// E2E app test of the transaction service.
func TestSpacemeshApp_TransactionService(t *testing.T) {
	setup()
	r := require.New(t)

	// Use a unique port
	port := 1236

	// Use a unique dir for data so we don't read existing state
	path := t.TempDir()

	app := New(WithLog(logtest.New(t)))
	cfg := config.DefaultTestConfig()
	app.Config = &cfg

	Cmd.Run = func(cmd *cobra.Command, args []string) {
		defer app.Cleanup()
		r.NoError(app.Initialize())

		app.Config.DataDirParent = path

		// GRPC configuration
		app.Config.API.GrpcServerPort = port
		app.Config.API.GrpcServerInterface = "localhost"
		app.Config.API.StartTransactionService = true

		// Prevent obnoxious warning in macOS
		app.Config.P2P.Listen = "/ip4/127.0.0.1/tcp/7073"

		// Avoid waiting for new connections.
		app.Config.P2P.TargetOutbound = 0

		// Speed things up a little
		app.Config.SyncInterval = 1
		app.Config.LayerDurationSec = 2

		// Force gossip to always listen, even when not synced
		app.Config.AlwaysListen = true

		app.Config.GenesisTime = time.Now().Add(20 * time.Second).Format(time.RFC3339)

		// This will block. We need to run the full app here to make sure that
		// the various services are reporting events correctly. This could probably
		// be done more surgically, and we don't need _all_ of the services.
		require.NoError(t, app.Start())
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
	cmdp.Cancel()()

	// Wait for it to stop
	wg.Wait()
}

func TestInitialize_BadTortoiseParams(t *testing.T) {
	conf := config.DefaultConfig()
	app := New(WithLog(logtest.New(t)), WithConfig(&conf))
	require.NoError(t, app.Initialize())

	conf = config.DefaultTestConfig()
	app = New(WithLog(logtest.New(t)), WithConfig(&conf))
	require.NoError(t, app.Initialize())

	app = New(WithLog(logtest.New(t)), WithConfig(getTestDefaultConfig()))
	require.NoError(t, app.Initialize())

	conf.Zdist = 5
	app = New(WithLog(logtest.New(t)), WithConfig(&conf))
	err := app.Initialize()
	assert.EqualError(t, err, "incompatible tortoise hare params")
}
