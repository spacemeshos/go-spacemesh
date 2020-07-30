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
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/net"
	"github.com/spacemeshos/go-spacemesh/p2p/node"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	r.Equal(9092, app.Config.API.NewGrpcServerPort)
	r.Equal(false, app.Config.API.StartNodeService)

	// Try enabling an illegal service
	Cmd.Run = func(cmd *cobra.Command, args []string) {
		err := app.Initialize(cmd, args)
		r.Error(err)
		r.Equal("unrecognized GRPC service requested: illegal", err.Error())
	}
	str, err := testArgs(app, "--grpc-port-new", strconv.Itoa(port), "--grpc", "illegal")
	r.NoError(err)
	r.Empty(str)
	// This should still be set
	r.Equal(port, app.Config.API.NewGrpcServerPort)
	r.Equal(false, app.Config.API.StartNodeService)

	resetFlags()

	// Try enabling two services, one with a legal name and one with an illegal name
	// In this case, the node service will be enabled because it comes first
	// Uses Cmd.Run as defined above
	str, err = testArgs(app, "--grpc-port-new", strconv.Itoa(port), "--grpc", "node", "--grpc", "illegal")
	r.NoError(err)
	r.Empty(str)
	r.Equal(true, app.Config.API.StartNodeService)

	resetFlags()

	// Try the same thing but change the order of the flags
	// In this case, the node service will not be enabled because it comes second
	// Uses Cmd.Run as defined above
	str, err = testArgs(app, "--grpc", "illegal", "--grpc-port-new", strconv.Itoa(port), "--grpc", "node")
	r.NoError(err)
	r.Empty(str)
	r.Equal(false, app.Config.API.StartNodeService)

	resetFlags()

	// Use commas instead
	// In this case, the node service will be enabled because it comes first
	// Uses Cmd.Run as defined above
	str, err = testArgs(app, "--grpc", "node,illegal", "--grpc-port-new", strconv.Itoa(port))
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
	setup()

	r := require.New(t)
	app := NewSpacemeshApp()
	r.Equal(9093, app.Config.API.NewJSONServerPort)
	r.Equal(false, app.Config.API.StartNewJSONServer)
	r.Equal(false, app.Config.API.StartNodeService)

	// Try enabling just the JSON service (without the GRPC service)
	Cmd.Run = func(cmd *cobra.Command, args []string) {
		err := app.Initialize(cmd, args)
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
	setup()

	// Use a unique port
	port := 1242

	r := require.New(t)
	app := NewSpacemeshApp()

	Cmd.Run = func(cmd *cobra.Command, args []string) {
		r.NoError(app.Initialize(cmd, args))
		app.Config.API.NewGrpcServerPort = port
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

	// Test starting the server from the commandline
	// uses Cmd.Run from above
	str, err = testArgs(app, "--grpc-port-new", strconv.Itoa(port), "--grpc", "node")
	r.Empty(str)
	r.NoError(err)
	r.Equal(port, app.Config.API.NewGrpcServerPort)
	r.Equal(true, app.Config.API.StartNodeService)

	// Give the services a few seconds to start running - this is important on travis.
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
	r.Equal(false, app.Config.API.StartMeshService)
	r.Equal(false, app.Config.API.StartGlobalStateService)
	r.Equal(false, app.Config.API.StartSmesherService)
	r.Equal(false, app.Config.API.StartTransactionService)

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
	events.CloseEventReporter()

	// Test starting the JSON server from the commandline
	// uses Cmd.Run from above
	str, err = testArgs(app,
		"--json-server-new",
		"--grpc", "node",
		"--json-port-new", "1234",
		//"--grpc-interface-new", "127.0.0.1",
	)
	r.Empty(str)
	r.NoError(err)
	r.Equal(1234, app.Config.API.NewJSONServerPort)
	r.Equal(true, app.Config.API.StartNewJSONServer)
	r.Equal(true, app.Config.API.StartNodeService)

	// Give the server a chance to start up
	time.Sleep(2 * time.Second)

	// We expect this one to succeed
	respBody, respStatus := callEndpoint(t, "v1/node/echo", payload, app.Config.API.NewJSONServerPort)
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

	app := NewSpacemeshApp()

	Cmd.Run = func(cmd *cobra.Command, args []string) {
		defer app.Cleanup(cmd, args)
		require.NoError(t, app.Initialize(cmd, args))

		// Give the error channel a buffer
		events.CloseEventReporter()
		require.NoError(t, events.InitializeEventReporterWithOptions("", 10, true))

		// Speed things up a little
		app.Config.SyncInterval = 1
		app.Config.LayerDurationSec = 2

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
		str, err := testArgs(app,
			"--grpc-port-new", strconv.Itoa(port),
			"--grpc", "node",
			// the following prevents obnoxious warning in macOS
			"--acquire-port=false",
			"--tcp-interface", "127.0.0.1",
			"--grpc-interface-new", "localhost",
		)
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
		if strings.Contains(myError.Message, "error adding genesis block block already exist in database") {
			myError = nextError()
		}

		require.Equal(t, "test123", myError.Message)
		require.Equal(t, int(zapcore.ErrorLevel), int(myError.ErrorType))

		myError = nextError()
		require.Equal(t, "test456", myError.Message)
		require.Equal(t, int(zapcore.ErrorLevel), int(myError.ErrorType))

		// The panic gets wrapped in an ERROR, and we get both, in this order
		myError = nextError()
		require.Contains(t, myError.Message, "Fatal: goroutine panicked.")
		require.Equal(t, int(zapcore.ErrorLevel), int(myError.ErrorType))

		myError = nextError()
		require.Equal(t, "testPANIC", myError.Message)
		require.Equal(t, int(zapcore.FatalLevel), int(myError.ErrorType))

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
			code := status.Code(err)
			// We expect this to happen when the server disconnects
			if code == codes.Unavailable {
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

	// Use a unique port
	port := 1236

	r := require.New(t)
	app := NewSpacemeshApp()

	Cmd.Run = func(cmd *cobra.Command, args []string) {
		defer app.Cleanup(cmd, args)
		r.NoError(app.Initialize(cmd, args))

		// GRPC configuration
		app.Config.API.NewGrpcServerPort = port
		app.Config.API.NewGrpcServerInterface = "localhost"
		app.Config.API.StartTransactionService = true

		// Prevent obnoxious warning in macOS
		app.Config.P2P.AcquirePort = false
		app.Config.P2P.TCPInterface = "127.0.0.1"

		// Speed things up a little
		app.Config.SyncInterval = 1
		app.Config.LayerDurationSec = 2

		// Force gossip to always listen, even when not synced
		app.Config.AlwaysListen = true

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
		str, err := testArgs(app)
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
	signer, err := signing.NewEdSignerFromBuffer(util.FromHex(config.Account1Private))
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
	p2pnet.Start(listener)
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
	r.NoError(swarm.Start())
	defer swarm.Shutdown()

	// Try to connect again: this should succeed
	conn, err := p2pnet.Dial(cmdp.Ctx, &tcpAddr, l.PublicKey())
	r.NoError(err)
	defer conn.Close()
	r.Equal(fmt.Sprintf("%s:%d", addr, app.Config.P2P.TCPPort), conn.RemoteAddr().String())
	r.Equal(l.PublicKey(), conn.RemotePublicKey())
}
