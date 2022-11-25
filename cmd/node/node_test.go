package node

//lint:file-ignore SA1019 hide deprecated protobuf version error
import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
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
	_ "github.com/nullstyle/go-xdr/xdr3" // without this go mod tidy will remove a necessary dependency
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/post/initialization"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/activation"
	apiConfig "github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/beacon"
	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/config/presets"
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
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
	infos, err := os.ReadDir(tempdir)
	r.NoError(err)
	r.Len(infos, 1)

	// Load existing identity.
	signer2, err := app.LoadOrCreateEdSigner()
	r.NoError(err)
	infos, err = os.ReadDir(tempdir)
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
	infos, err = os.ReadDir(tempdir)
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

func testArgs(ctx context.Context, root *cobra.Command, args ...string) (string, error) {
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	// Workaround: will fail if args is nil (defaults to os args)
	if args == nil {
		args = []string{""}
	}
	root.SetArgs(args)
	_, err := root.ExecuteContextC(ctx) // runs Run()
	return buf.String(), err
}

func cmdWithRun(run func(*cobra.Command, []string)) *cobra.Command {
	cmd := GetCommand()
	cmd.Run = run
	return cmd
}

func TestSpacemeshApp_Cmd(t *testing.T) {
	r := require.New(t)
	app := New(WithLog(logtest.New(t)))

	expected := `unknown command "illegal" for "node"`
	expected2 := "Error: " + expected + "\nRun 'node --help' for usage.\n"
	r.Equal(config.ConsoleLogEncoder, app.Config.LOGGING.Encoder)

	// Test an illegal flag
	cmd := cmdWithRun(func(*cobra.Command, []string) {
		// We don't expect this to be called at all
		r.Fail("Command.Run not expected to run")
	})

	str, err := testArgs(context.Background(), cmd, "illegal")
	r.Error(err)
	r.Equal(expected, err.Error())
	r.Equal(expected2, str)

	// Test a legal flag
	cmd = cmdWithRun(func(cmd *cobra.Command, args []string) {
		r.NoError(cmdp.EnsureCLIFlags(cmd, app.Config))
	})

	str, err = testArgs(context.Background(), cmd, "--log-encoder", "json")

	r.NoError(err)
	r.Empty(str)
	r.Equal(config.JSONLogEncoder, app.Config.LOGGING.Encoder)
}

func setup() {
	// Reset the shutdown context
	// oof, globals make testing really difficult
	ctx, cancel := context.WithCancel(context.Background())

	cmdp.SetCtx(ctx)
	cmdp.SetCancel(cancel)

	events.CloseEventReporter()
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
	run := func(cmd *cobra.Command, args []string) {
		err := cmdp.EnsureCLIFlags(cmd, app.Config)
		r.Error(err)
		r.Equal("parse services list: unrecognized GRPC service requested: illegal", err.Error())
	}

	str, err := testArgs(context.Background(), cmdWithRun(run), "--grpc-port", strconv.Itoa(port), "--grpc", "illegal")
	r.NoError(err)
	r.Empty(str)
	// This should still be set
	r.Equal(port, app.Config.API.GrpcServerPort)
	r.Equal(false, app.Config.API.StartNodeService)

	events.CloseEventReporter()

	// Try enabling two services, one with a legal name and one with an illegal name
	// In this case, the node service will be enabled because it comes first
	str, err = testArgs(context.Background(), cmdWithRun(run), "--grpc-port", strconv.Itoa(port), "--grpc", "node", "--grpc", "illegal")
	r.NoError(err)
	r.Empty(str)
	r.Equal(true, app.Config.API.StartNodeService)

	events.CloseEventReporter()
	app.Config.API = apiConfig.DefaultTestConfig()

	// Try the same thing but change the order of the flags
	// In this case, the node service will not be enabled because it comes second
	str, err = testArgs(context.Background(), cmdWithRun(run), "--grpc", "illegal", "--grpc-port", strconv.Itoa(port), "--grpc", "node")
	r.NoError(err)
	r.Empty(str)
	r.Equal(false, app.Config.API.StartNodeService)

	events.CloseEventReporter()

	// Use commas instead
	// In this case, the node service will be enabled because it comes first
	// Uses Cmd.Run as defined above
	str, err = testArgs(context.Background(), cmdWithRun(run), "--grpc", "node,illegal", "--grpc-port", strconv.Itoa(port))
	r.NoError(err)
	r.Empty(str)
	r.Equal(true, app.Config.API.StartNodeService)

	events.CloseEventReporter()

	// This should work
	run = func(cmd *cobra.Command, args []string) {
		r.NoError(cmdp.EnsureCLIFlags(cmd, app.Config))
	}
	str, err = testArgs(context.Background(), cmdWithRun(run), "--grpc", "node")
	r.Empty(str)
	r.NoError(err)
	r.Equal(true, app.Config.API.StartNodeService)

	events.CloseEventReporter()

	// This should work too
	str, err = testArgs(context.Background(), cmdWithRun(run), "--grpc", "node,node")
	r.Empty(str)
	r.NoError(err)
	r.Equal(true, app.Config.API.StartNodeService)

	// Test enabling two services both ways
	events.CloseEventReporter()
	app.Config.API = apiConfig.DefaultTestConfig()

	r.Equal(false, app.Config.API.StartNodeService)
	r.Equal(false, app.Config.API.StartMeshService)
	str, err = testArgs(context.Background(), cmdWithRun(run), "--grpc", "node,mesh")
	r.Empty(str)
	r.NoError(err)
	r.Equal(true, app.Config.API.StartNodeService)
	r.Equal(true, app.Config.API.StartMeshService)

	events.CloseEventReporter()
	app.Config.API = apiConfig.DefaultTestConfig()

	r.Equal(false, app.Config.API.StartNodeService)
	r.Equal(false, app.Config.API.StartMeshService)
	str, err = testArgs(context.Background(), cmdWithRun(run), "--grpc", "node", "--grpc", "mesh")
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
	run := func(cmd *cobra.Command, args []string) {
		err := cmdp.EnsureCLIFlags(cmd, app.Config)
		r.Error(err)
		r.Equal("parse services list: must enable at least one GRPC service along with JSON gateway service", err.Error())
	}
	str, err := testArgs(context.Background(), cmdWithRun(run), "--json-server")
	r.NoError(err)
	r.Empty(str)
	r.Equal(true, app.Config.API.StartJSONServer)
	r.Equal(false, app.Config.API.StartNodeService)

	events.CloseEventReporter()

	// Try enabling both the JSON and the GRPC services
	run = func(cmd *cobra.Command, args []string) {
		r.NoError(cmdp.EnsureCLIFlags(cmd, app.Config))
	}
	str, err = testArgs(context.Background(), cmdWithRun(run), "--grpc", "node", "--json-server")
	r.NoError(err)
	r.Empty(str)
	r.Equal(true, app.Config.API.StartNodeService)
	r.Equal(true, app.Config.API.StartJSONServer)

	events.CloseEventReporter()
	app.Config.API = apiConfig.DefaultTestConfig()

	// Try changing the port
	// Uses Cmd.Run as defined above
	str, err = testArgs(context.Background(), cmdWithRun(run), "--json-port", "1234")
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
	buf, err := io.ReadAll(resp.Body)
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

	run := func(cmd *cobra.Command, args []string) {
		r.NoError(cmdp.EnsureCLIFlags(cmd, app.Config))
		app.Config.API.GrpcServerPort = port
		app.Config.DataDirParent = path
		app.startAPIServices(context.TODO())
	}
	defer app.stopServices()

	// Make sure the service is not running by default
	str, err := testArgs(context.Background(), cmdWithRun(run)) // no args
	r.Empty(str)
	r.NoError(err)
	r.Equal(false, app.Config.API.StartNodeService)

	_, err = grpc.Dial(
		fmt.Sprintf("localhost:%d", port),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithTimeout(2*time.Second),
	)
	r.ErrorContains(err, "context deadline exceeded")

	events.CloseEventReporter()

	// Test starting the server from the command line
	str, err = testArgs(context.Background(), cmdWithRun(run), "--grpc-port", strconv.Itoa(port), "--grpc", "node")
	r.Empty(str)
	r.NoError(err)
	r.Equal(port, app.Config.API.GrpcServerPort)
	r.True(app.Config.API.StartNodeService)

	conn, err := grpc.Dial(
		fmt.Sprintf("localhost:%d", port),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithTimeout(2*time.Second),
	)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(conn.Close()) })
	c := pb.NewNodeServiceClient(conn)

	// call echo and validate result
	// We expect this one to succeed
	const message = "Hello World"
	response, err := c.Echo(context.Background(), &pb.EchoRequest{
		Msg: &pb.SimpleString{Value: message},
	})
	r.NoError(err)
	r.Equal(message, response.Msg.Value)
}

func TestSpacemeshApp_JsonServiceNotRunning(t *testing.T) {
	setup()
	r := require.New(t)
	app := New(WithLog(logtest.New(t)))

	// Make sure the service is not running by default
	run := func(cmd *cobra.Command, args []string) {
		r.NoError(cmdp.EnsureCLIFlags(cmd, app.Config))
		app.Config.DataDirParent = t.TempDir()
		app.startAPIServices(context.TODO())
	}

	str, err := testArgs(context.Background(), cmdWithRun(run))
	r.Empty(str)
	r.NoError(err)
	defer app.stopServices()
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

	events.CloseEventReporter()
}

func TestSpacemeshApp_JsonService(t *testing.T) {
	setup()
	r := require.New(t)
	app := New(WithLog(logtest.New(t)))
	const message = "nihao shijie"
	payload := marshalProto(t, &pb.EchoRequest{Msg: &pb.SimpleString{Value: message}})

	// Make sure the service is not running by default
	run := func(cmd *cobra.Command, args []string) {
		r.NoError(cmdp.EnsureCLIFlags(cmd, app.Config))
		app.Config.DataDirParent = t.TempDir()
		app.startAPIServices(context.TODO())
	}

	// Test starting the JSON server from the commandline
	// uses Cmd.Run from above
	str, err := testArgs(context.Background(), cmdWithRun(run), "--json-server", "--grpc", "node", "--json-port", "1234")
	r.Empty(str)
	r.NoError(err)
	defer app.stopServices()
	r.Equal(1234, app.Config.API.JSONServerPort)
	r.Equal(true, app.Config.API.StartJSONServer)
	r.Equal(true, app.Config.API.StartNodeService)

	var (
		respBody   string
		respStatus int
	)
	require.Eventually(t, func() bool {
		respBody, respStatus = callEndpoint(t, "v1/node/echo", payload, app.Config.API.JSONServerPort)
		return respStatus == http.StatusOK
	}, 2*time.Second, 100*time.Millisecond)
	var msg pb.EchoResponse
	require.NoError(t, jsonpb.UnmarshalString(respBody, &msg))
	require.Equal(t, message, msg.Msg.Value)
	require.Equal(t, http.StatusOK, respStatus)
	require.NoError(t, jsonpb.UnmarshalString(respBody, &msg))
	require.Equal(t, message, msg.Msg.Value)
}

// E2E app test of the stream endpoints in the NodeService.
func TestSpacemeshApp_NodeService(t *testing.T) {
	if util.IsWindows() {
		t.Skip("Skipping test in Windows (https://github.com/spacemeshos/go-spacemesh/issues/3626)")
	}

	// errlog should be used only for testing.
	logger := logtest.New(t)
	errlog := log.RegisterHooks(logtest.New(t, zap.ErrorLevel), events.EventHook())
	setup()

	// Use a unique port
	port := 1240
	path := t.TempDir()

	clock := timesync.NewClock(timesync.RealClock{}, time.Duration(1)*time.Second, time.Now(), logtest.New(t))
	mesh, err := mocknet.WithNPeers(1)
	require.NoError(t, err)
	cfg := getTestDefaultConfig()

	poetHarness, err := activation.NewHTTPPoetHarness(false)
	require.NoError(t, err)
	t.Cleanup(func() {
		err := poetHarness.Teardown(true)
		if assert.NoError(t, err, "failed to tear down harness") {
			t.Log("harness torn down")
		}
	})
	edSgn := signing.NewEdSigner()
	h, err := p2p.Upgrade(mesh.Hosts()[0], cfg.Genesis.GenesisID())
	require.NoError(t, err)
	app, err := initSingleInstance(logger, *cfg, 0, cfg.Genesis.GenesisTime,
		path, eligibility.New(logtest.New(t)),
		poetHarness.HTTPPoetClient, clock, h, edSgn)
	require.NoError(t, err)

	run := func(cmd *cobra.Command, args []string) {
		defer app.Cleanup()
		require.NoError(t, cmdp.EnsureCLIFlags(cmd, app.Config))

		// Give the error channel a buffer
		events.CloseEventReporter()
		events.InitializeReporter()

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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Run the app in a goroutine. As noted above, it blocks if it succeeds.
	// If there's an error in the args, it will return immediately.
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		// This makes sure the test doesn't end until this goroutine closes
		defer wg.Done()
		str, err := testArgs(ctx, cmdWithRun(run), "--grpc-port", strconv.Itoa(port), "--grpc", "node", "--grpc-interface", "localhost")
		assert.Empty(t, str)
		assert.NoError(t, err)
	}()

	conn, err := grpc.DialContext(
		ctx,
		fmt.Sprintf("localhost:%d", port),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	c := pb.NewNodeServiceClient(conn)

	wg.Add(1)
	go func() {
		defer wg.Done()

		streamStatus, err := c.StatusStream(ctx, &pb.StatusStreamRequest{})
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

	streamErr, err := c.ErrorStream(ctx, &pb.ErrorStreamRequest{})
	require.NoError(t, err)
	_, err = streamErr.Header()
	require.NoError(t, err)

	// Report two errors and make sure they're both received
	wg.Add(1)
	go func() {
		defer wg.Done()

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

	app := New(WithLog(logtest.New(t)))
	cfg := config.DefaultTestConfig()
	cfg.DataDirParent = t.TempDir()
	app.Config = &cfg

	signer := signing.NewEdSigner()
	address := wallet.Address(signer.PublicKey().Bytes())

	run := func(cmd *cobra.Command, args []string) {
		defer app.Cleanup()
		r.NoError(app.Initialize())

		// GRPC configuration
		app.Config.API.GrpcServerPort = port
		app.Config.API.GrpcServerInterface = "localhost"
		app.Config.API.StartTransactionService = true

		// Prevent obnoxious warning in macOS
		app.Config.P2P.Listen = "/ip4/127.0.0.1/tcp/7073"

		// Avoid waiting for new connections.
		app.Config.P2P.TargetOutbound = 0

		// syncer will cause the node to go out of sync (and not listen to gossip)
		// since we are testing single-node transaction service, we don't need the syncer to run
		app.Config.SyncInterval = 1000000
		app.Config.LayerDurationSec = 2

		app.Config.Genesis = &config.GenesisConfig{
			GenesisTime: time.Now().Add(20 * time.Second).Format(time.RFC3339),
			Accounts: map[string]uint64{
				address.String(): 100_000_000,
			},
		}

		// This will block. We need to run the full app here to make sure that
		// the various services are reporting events correctly. This could probably
		// be done more surgically, and we don't need _all_ of the services.
		require.NoError(t, app.Start())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Run the app in a goroutine. As noted above, it blocks if it succeeds.
	// If there's an error in the args, it will return immediately.
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		str, err := testArgs(ctx, cmdWithRun(run))
		r.Empty(str)
		r.NoError(err)
		wg.Done()
	}()

	<-app.Started()
	conn, err := grpc.Dial(
		fmt.Sprintf("localhost:%d", port),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithTimeout(5*time.Second),
	)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(conn.Close()) })
	c := pb.NewTransactionServiceClient(conn)

	tx1 := types.NewRawTx(wallet.SelfSpawn(signer.PrivateKey(), types.Nonce{}, sdk.WithGenesisID(cfg.Genesis.GenesisID())))

	stream, err := c.TransactionsStateStream(ctx, &pb.TransactionsStateStreamRequest{
		TransactionId:       []*pb.TransactionId{{Id: tx1.ID.Bytes()}},
		IncludeTransactions: true,
	})
	require.NoError(t, err)
	_, err = stream.Header()
	require.NoError(t, err)

	// TODO(dshulyak) synchronization below is messed up
	wg2 := sync.WaitGroup{}
	wg2.Add(1)
	go func() {
		defer wg2.Done()

		// Now listen on the stream
		res, err := stream.Recv()
		require.NoError(t, err)
		require.Equal(t, tx1.ID.Bytes(), res.TransactionState.Id.Id)

		// We expect the tx to go to the mempool
		inTx := res.Transaction
		require.Equal(t, pb.TransactionState_TRANSACTION_STATE_MEMPOOL, res.TransactionState.State)
		require.Equal(t, tx1.ID.Bytes(), inTx.Id)
		require.Equal(t, address.String(), inTx.Principal.Address)
	}()

	// Submit the txs
	res1, err := c.SubmitTransaction(context.Background(), &pb.SubmitTransactionRequest{
		Transaction: tx1.Raw,
	})
	require.NoError(t, err)
	require.Equal(t, int32(code.Code_OK), res1.Status.Code)
	require.Equal(t, tx1.ID.Bytes(), res1.Txstate.Id.Id)
	require.Equal(t, pb.TransactionState_TRANSACTION_STATE_MEMPOOL, res1.Txstate.State)

	// Wait for messages to be received
	wg2.Wait()

	// This stops the app
	cmdp.Cancel()()

	// Wait for it to stop
	wg.Wait()
}

func TestInitialize_BadTortoiseParams(t *testing.T) {
	conf := config.DefaultConfig()
	conf.DataDirParent = t.TempDir()
	app := New(WithLog(logtest.New(t)), WithConfig(&conf))
	require.NoError(t, app.Initialize())

	conf = config.DefaultTestConfig()
	conf.DataDirParent = t.TempDir()
	app = New(WithLog(logtest.New(t)), WithConfig(&conf))
	require.NoError(t, app.Initialize())

	tconf := getTestDefaultConfig()
	tconf.DataDirParent = t.TempDir()
	app = New(WithLog(logtest.New(t)), WithConfig(tconf))
	require.NoError(t, app.Initialize())

	conf.Tortoise.Zdist = 5
	app = New(WithLog(logtest.New(t)), WithConfig(&conf))
	err := app.Initialize()
	assert.EqualError(t, err, "incompatible tortoise hare params")
}

func TestConfig_Preset(t *testing.T) {
	const name = "testnet"

	t.Run("PresetApplied", func(t *testing.T) {
		preset, err := presets.Get(name)
		require.NoError(t, err)

		cmd := &cobra.Command{}
		cmdp.AddCommands(cmd)

		viper.Set("preset", name)
		t.Cleanup(viper.Reset)
		conf, err := loadConfig(cmd)
		require.NoError(t, err)
		require.Equal(t, preset, *conf)
	})

	t.Run("PresetOverwrittenByFlags", func(t *testing.T) {
		preset, err := presets.Get(name)
		require.NoError(t, err)

		cmd := &cobra.Command{}
		cmdp.AddCommands(cmd)
		const lowPeers = 1234
		require.NoError(t, cmd.ParseFlags([]string{"--low-peers=" + strconv.Itoa(lowPeers)}))

		viper.Set("preset", name)
		t.Cleanup(viper.Reset)
		conf, err := loadConfig(cmd)
		require.NoError(t, err)
		preset.P2P.LowPeers = lowPeers
		require.Equal(t, preset, *conf)
	})

	t.Run("PresetOverWrittenByConfigFile", func(t *testing.T) {
		preset, err := presets.Get(name)
		require.NoError(t, err)

		cmd := &cobra.Command{}
		cmdp.AddCommands(cmd)
		const lowPeers = 1234

		content := fmt.Sprintf(`{"p2p": {"low-peers": %d}}`, lowPeers)
		path := filepath.Join(t.TempDir(), "config.json")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		require.NoError(t, cmd.ParseFlags([]string{"--config=" + path}))

		viper.Set("preset", name)
		t.Cleanup(viper.Reset)
		conf, err := loadConfig(cmd)
		require.NoError(t, err)
		preset.P2P.LowPeers = lowPeers
		preset.ConfigFile = path
		require.Equal(t, preset, *conf)
	})

	t.Run("LoadedFromConfigFile", func(t *testing.T) {
		preset, err := presets.Get(name)
		require.NoError(t, err)

		cmd := &cobra.Command{}
		cmdp.AddCommands(cmd)

		content := fmt.Sprintf(`{"preset": "%s"}`, name)
		path := filepath.Join(t.TempDir(), "config.json")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		require.NoError(t, cmd.ParseFlags([]string{"--config=" + path}))

		t.Cleanup(viper.Reset)

		conf, err := loadConfig(cmd)
		require.NoError(t, err)
		preset.ConfigFile = path
		require.Equal(t, preset, *conf)
	})
}

func TestConfig_GenesisAccounts(t *testing.T) {
	t.Run("OverwriteDefaults", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmdp.AddCommands(cmd)

		const value = 100
		keys := []string{"0x03", "0x04"}
		args := []string{}
		for _, key := range keys {
			args = append(args, fmt.Sprintf("-a %s=%d", key, value))
		}
		require.NoError(t, cmd.ParseFlags(args))

		conf, err := loadConfig(cmd)
		require.NoError(t, err)
		for _, key := range keys {
			require.EqualValues(t, conf.Genesis.Accounts[key], value)
		}
	})
}

func TestGenesisConfig(t *testing.T) {
	t.Run("config is written to a file", func(t *testing.T) {
		app := New()
		app.Config = getTestDefaultConfig()
		app.Config.DataDirParent = t.TempDir()

		require.NoError(t, app.Initialize())
		var existing config.GenesisConfig
		require.NoError(t, existing.LoadFromFile(filepath.Join(app.Config.DataDir(), genesisFileName)))
		require.Empty(t, existing.Diff(app.Config.Genesis))
	})
	t.Run("no error if no diff", func(t *testing.T) {
		app := New()
		app.Config = getTestDefaultConfig()
		app.Config.DataDirParent = t.TempDir()

		require.NoError(t, app.Initialize())
		require.NoError(t, app.Initialize())
	})
	t.Run("fatal error on a diff", func(t *testing.T) {
		app := New()
		app.Config = getTestDefaultConfig()
		app.Config.DataDirParent = t.TempDir()

		require.NoError(t, app.Initialize())
		app.Config.Genesis.ExtraData = "changed"
		err := app.Initialize()
		require.ErrorContains(t, err, "genesis config")
	})
	t.Run("not valid time", func(t *testing.T) {
		app := New()
		app.Config = getTestDefaultConfig()
		app.Config.DataDirParent = t.TempDir()
		app.Config.Genesis.GenesisTime = time.Now().Format(time.RFC1123)

		require.ErrorContains(t, app.Initialize(), "time.RFC3339")
	})
	t.Run("long extra data", func(t *testing.T) {
		app := New()
		app.Config = getTestDefaultConfig()
		app.Config.DataDirParent = t.TempDir()
		app.Config.Genesis.ExtraData = string(make([]byte, 256))

		require.ErrorContains(t, app.Initialize(), "extra-data")
	})
}

func getTestDefaultConfig() *config.Config {
	cfg, err := LoadConfigFromFile()
	if err != nil {
		log.Error("cannot load config from file")
		return nil
	}
	// is set to 0 to make sync start immediately when node starts
	cfg.P2P.TargetOutbound = 0

	cfg.POST = activation.DefaultPostConfig()
	cfg.POST.MinNumUnits = 2
	cfg.POST.MaxNumUnits = 4
	cfg.POST.LabelsPerUnit = 32
	cfg.POST.BitsPerLabel = 8
	cfg.POST.K2 = 4

	cfg.SMESHING = config.DefaultSmeshingConfig()
	cfg.SMESHING.Start = true
	cfg.SMESHING.Opts.NumUnits = cfg.POST.MinNumUnits + 1
	cfg.SMESHING.Opts.NumFiles = 1
	cfg.SMESHING.Opts.ComputeProviderID = initialization.CPUProviderID()

	// note: these need to be set sufficiently low enough that turbohare finishes well before the LayerDurationSec
	cfg.HARE.RoundDuration = 2
	cfg.HARE.WakeupDelta = 1
	cfg.HARE.N = 5
	cfg.HARE.F = 2
	cfg.HARE.ExpectedLeaders = 5
	cfg.HARE.SuperHare = true
	cfg.LayerAvgSize = 5
	cfg.LayersPerEpoch = 3
	cfg.TxsPerProposal = 100
	cfg.Tortoise.Hdist = 5
	cfg.Tortoise.Zdist = 5

	cfg.LayerDurationSec = 20
	cfg.HareEligibility.ConfidenceParam = 4
	cfg.HareEligibility.EpochOffset = 0
	cfg.SyncRequestTimeout = 500
	cfg.SyncInterval = 2

	cfg.FETCH.RequestTimeout = 10
	cfg.FETCH.MaxRetriesForPeer = 5
	cfg.FETCH.BatchSize = 5
	cfg.FETCH.BatchTimeout = 5

	cfg.Beacon = beacon.NodeSimUnitTestConfig()

	cfg.Genesis = config.DefaultTestGenesisConfig()

	types.SetLayersPerEpoch(cfg.LayersPerEpoch)

	return cfg
}

// initSingleInstance initializes a node instance with given
// configuration and parameters, it does not stop the instance.
func initSingleInstance(lg log.Log, cfg config.Config, i int, genesisTime string, storePath string, rolacle *eligibility.FixedRolacle,
	poetClient *activation.HTTPPoetClient, clock TickProvider, host *p2p.Host, edSgn *signing.EdSigner,
) (*App, error) {
	smApp := New(WithLog(lg))
	smApp.Config = &cfg
	smApp.Config.Genesis.GenesisTime = genesisTime

	coinbaseAddressBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(coinbaseAddressBytes, uint32(i+1))
	smApp.Config.SMESHING.CoinbaseAccount = types.GenerateAddress(coinbaseAddressBytes).String()
	smApp.Config.SMESHING.Opts.DataDir, _ = os.MkdirTemp("", "sm-app-test-post-datadir")

	smApp.host = host
	smApp.edSgn = edSgn

	pub := edSgn.PublicKey()
	vrfSigner := edSgn.VRFSigner()

	nodeID := types.BytesToNodeID(pub.Bytes())

	err := smApp.initServices(context.TODO(), nodeID, storePath, edSgn,
		uint32(smApp.Config.LayerAvgSize), []activation.PoetProvingServiceClient{poetClient}, vrfSigner, smApp.Config.LayersPerEpoch, clock)
	if err != nil {
		return nil, err
	}

	return smApp, err
}
