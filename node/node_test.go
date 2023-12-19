package node

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
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

	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/post/initialization"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/sync/errgroup"
	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/beacon"
	"github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/config/presets"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk"
	"github.com/spacemeshos/go-spacemesh/genvm/sdk/wallet"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/signing"
)

const layersPerEpoch = 3

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(layersPerEpoch)

	res := m.Run()
	os.Exit(res)
}

func TestSpacemeshApp_getEdIdentity(t *testing.T) {
	r := require.New(t)

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

	t.Run("bad length", func(t *testing.T) {
		testLoadOrCreateEdSigner(t,
			bytes.Repeat([]byte("ab"), signing.PrivateKeySize-1),
			"invalid key size 63/64",
		)
	})
	t.Run("bad hex", func(t *testing.T) {
		testLoadOrCreateEdSigner(t,
			bytes.Repeat([]byte("CV"), signing.PrivateKeySize),
			"decoding private key: encoding/hex: invalid byte",
		)
	})
	t.Run("good key", func(t *testing.T) {
		_, priv, err := ed25519.GenerateKey(nil)
		require.NoError(t, err)
		testLoadOrCreateEdSigner(t,
			[]byte(hex.EncodeToString(priv)),
			"",
		)
	})
}

func testLoadOrCreateEdSigner(t *testing.T, data []byte, expect string) {
	tempdir := t.TempDir()
	app := New(WithLog(logtest.New(t)))
	app.Config.SMESHING.Opts.DataDir = tempdir
	keyfile := filepath.Join(app.Config.SMESHING.Opts.DataDir, edKeyFileName)
	require.NoError(t, os.WriteFile(keyfile, data, 0o600))
	signer, err := app.LoadOrCreateEdSigner()
	if len(expect) > 0 {
		require.ErrorContains(t, err, expect)
		require.Nil(t, signer)
	} else {
		require.NoError(t, err)
		require.NotEmpty(t, signer)
	}
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
	r.Equal(
		fmt.Sprintf("INFO\t%s\t%s\t{\"module\": \"%s\"}\n", mylogger, msg1, mylogger),
		buf1.String(),
	)
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
	r.Equal(
		fmt.Sprintf("WARN\t%s\t%s\t{\"module\": \"%s\"}\n", mylogger, msg3, mylogger),
		buf1.String(),
	)
	r.Equal(fmt.Sprintf("INFO\t%s\n", msg1), buf2.String())
	buf1.Reset()

	r.NoError(app.SetLogLevel(mylogger, "info"))

	msg4 := "nihao"
	app.log.Info(msg4)
	r.Equal("info", app.loggers[mylogger].String())
	r.Equal(
		fmt.Sprintf("INFO\t%s\t%s\t{\"module\": \"%s\"}\n", mylogger, msg4, mylogger),
		buf1.String(),
	)

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
	r.Equal(
		fmt.Sprintf("INFO\t%s\t%s\t{\"module\": \"%s\"}\n", mylogger, teststr, mylogger),
		buf.String(),
	)
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
	c := cmdWithRun(func(*cobra.Command, []string) {
		// We don't expect this to be called at all
		r.Fail("Command.Run not expected to run")
	})

	str, err := testArgs(context.Background(), c, "illegal")
	r.Error(err)
	r.Equal(expected, err.Error())
	r.Equal(expected2, str)

	// Test a legal flag
	c = cmdWithRun(func(c *cobra.Command, args []string) {
		r.NoError(cmd.EnsureCLIFlags(c, app.Config))
	})

	str, err = testArgs(context.Background(), c, "--log-encoder", "json")

	r.NoError(err)
	r.Empty(str)
	r.Equal(config.JSONLogEncoder, app.Config.LOGGING.Encoder)
}

func marshalProto(t *testing.T, msg proto.Message) []byte {
	buf, err := protojson.Marshal(msg)
	require.NoError(t, err)
	return buf
}

func callEndpoint(t *testing.T, url string, payload []byte) ([]byte, int) {
	resp, err := http.Post(url, "application/json", bytes.NewReader(payload))
	require.NoError(t, err)
	require.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	buf, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	return buf, resp.StatusCode
}

func TestSpacemeshApp_GrpcService(t *testing.T) {
	// Use a unique port
	listener := "127.0.0.1:1242"

	r := require.New(t)
	app := New(WithLog(logtest.New(t)))

	path := t.TempDir()

	run := func(c *cobra.Command, args []string) {
		app.Config.API.PublicListener = listener
		app.Config.API.PublicServices = nil
		app.Config.API.PrivateServices = nil
		r.NoError(cmd.EnsureCLIFlags(c, app.Config))
		app.Config.DataDirParent = path
		app.startAPIServices(context.Background())
	}
	defer app.stopServices(context.Background())

	// Make sure the service is not running by default
	str, err := testArgs(context.Background(), cmdWithRun(run)) // no args
	r.Empty(str)
	r.NoError(err)
	r.Empty(app.Config.API.PublicServices)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = grpc.DialContext(
		ctx,
		listener,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	r.ErrorContains(err, "context deadline exceeded")

	events.CloseEventReporter()

	// Test starting the server from the command line
	str, err = testArgs(
		context.Background(),
		cmdWithRun(run),
		"--grpc-public-listener",
		listener,
		"--grpc-public-services",
		"node",
	)
	r.Empty(str)
	r.NoError(err)
	r.Equal(listener, app.Config.API.PublicListener)
	r.Contains(app.Config.API.PublicServices, "node")

	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(
		ctx,
		listener,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
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
	r := require.New(t)
	app := New(WithLog(logtest.New(t)))

	// Make sure the service is not running by default
	run := func(c *cobra.Command, args []string) {
		r.NoError(cmd.EnsureCLIFlags(c, app.Config))
		app.Config.DataDirParent = t.TempDir()
		app.startAPIServices(context.Background())
	}

	str, err := testArgs(context.Background(), cmdWithRun(run))
	r.Empty(str)
	r.NoError(err)
	defer app.stopServices(context.Background())

	// Try talking to the server
	const message = "nihao shijie"

	// generate request payload (api input params)
	payload := marshalProto(t, &pb.EchoRequest{Msg: &pb.SimpleString{Value: message}})

	// We expect this one to fail
	url := fmt.Sprintf("http://%s/%s", app.Config.API.JSONListener, "v1/node/echo")
	_, err = http.Post(url, "application/json", bytes.NewReader(payload))
	r.Error(err)

	events.CloseEventReporter()
}

func TestSpacemeshApp_JsonService(t *testing.T) {
	r := require.New(t)
	app := New(WithLog(logtest.New(t)))
	const message = "nihao shijie"
	payload := marshalProto(t, &pb.EchoRequest{Msg: &pb.SimpleString{Value: message}})

	// Make sure the service is not running by default
	run := func(c *cobra.Command, args []string) {
		app.Config.API.PrivateServices = nil
		r.NoError(cmd.EnsureCLIFlags(c, app.Config))
		app.Config.DataDirParent = t.TempDir()
		app.startAPIServices(context.Background())
	}

	// Test starting the JSON server from the commandline
	// uses Cmd.Run from above
	listener := "127.0.0.1:1234"
	str, err := testArgs(
		context.Background(),
		cmdWithRun(run),
		"--grpc-public-services",
		"node",
		"--grpc-json-listener",
		listener,
	)
	r.Empty(str)
	r.NoError(err)
	defer app.stopServices(context.Background())
	r.Equal(listener, app.Config.API.JSONListener)
	r.Contains(app.Config.API.PublicServices, "node")

	var (
		respBody   []byte
		respStatus int
	)
	require.Eventually(t, func() bool {
		respBody, respStatus = callEndpoint(
			t,
			fmt.Sprintf("http://%s/v1/node/echo", app.Config.API.JSONListener),
			payload,
		)
		return respStatus == http.StatusOK
	}, 2*time.Second, 100*time.Millisecond)
	var msg pb.EchoResponse
	require.NoError(t, protojson.Unmarshal(respBody, &msg))
	require.Equal(t, message, msg.Msg.Value)
	require.Equal(t, http.StatusOK, respStatus)
	require.NoError(t, protojson.Unmarshal(respBody, &msg))
	require.Equal(t, message, msg.Msg.Value)
}

// E2E app test of the stream endpoints in the NodeService.
func TestSpacemeshApp_NodeService(t *testing.T) {
	logger := logtest.New(t)
	errlog := log.RegisterHooks(
		logtest.New(t, zap.ErrorLevel),
		events.EventHook(),
	) // errlog is used to simulate errors in the app

	// Use a unique port
	port := 1240

	app := New(WithLog(logger))
	app.Config = getTestDefaultConfig(t)
	app.Config.SMESHING.CoinbaseAccount = types.GenerateAddress([]byte{1}).String()
	app.Config.SMESHING.Opts.DataDir = t.TempDir()

	edSgn, err := signing.NewEdSigner()
	require.NoError(t, err)
	app.edSgn = edSgn

	mesh, err := mocknet.WithNPeers(1)
	require.NoError(t, err)
	h, err := p2p.Upgrade(mesh.Hosts()[0])
	require.NoError(t, err)
	app.host = h

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	run := func(c *cobra.Command, args []string) {
		require.NoError(t, cmd.EnsureCLIFlags(c, app.Config))

		// Give the error channel a buffer
		events.CloseEventReporter()
		events.InitializeReporter()

		// Speed things up a little
		app.Config.Sync.Interval = time.Second
		app.Config.LayerDuration = 2 * time.Second
		app.Config.DataDirParent = t.TempDir()

		// This will block. We need to run the full app here to make sure that
		// the various services are reporting events correctly. This could probably
		// be done more surgically, and we don't need _all_ of the services.
		require.NoError(t, app.Start(context.Background()))
	}

	// Run the app in a goroutine. As noted above, it blocks if it succeeds.
	// If there's an error in the args, it will return immediately.
	var eg errgroup.Group
	eg.Go(func() error {
		str, err := testArgs(
			ctx,
			cmdWithRun(run),
			"--grpc-private-listener",
			fmt.Sprintf("localhost:%d", port),
			"--grpc-private-services",
			"node",
			"--grpc-public-services",
			"debug",
		)
		assert.Empty(t, str)
		assert.NoError(t, err)
		return nil
	})

	conn, err := grpc.DialContext(
		ctx,
		fmt.Sprintf("localhost:%d", port),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, conn.Close()) })
	c := pb.NewNodeServiceClient(conn)

	eg.Go(func() error {
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
					return nil
				}
				assert.NoError(t, err)
				return nil
			}
			// Note that, for some reason, protobuf does not display fields
			// that still have their default value, so the output here will
			// only be partial.
			logger.Debug("Got status message: %s", in.Status)
		}
	})

	streamErr, err := c.ErrorStream(ctx, &pb.ErrorStreamRequest{})
	require.NoError(t, err)
	_, err = streamErr.Header()
	require.NoError(t, err)

	// Report two errors and make sure they're both received
	eg.Go(func() error {
		errlog.Error("test123")
		errlog.Error("test456")
		assert.Panics(t, func() { errlog.Panic("testPANIC") })
		return nil
	})

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

	// Cleanup stops all services and thereby the app
	<-app.Started() // prevents races when app is not started yet
	app.Cleanup(context.Background())

	// Wait for everything to stop cleanly before ending test
	eg.Wait()
}

// E2E app test of the transaction service.
func TestSpacemeshApp_TransactionService(t *testing.T) {
	r := require.New(t)

	listener := "127.0.0.1:14236"

	app := New(WithLog(logtest.New(t)))
	cfg := config.DefaultTestConfig()
	cfg.DataDirParent = t.TempDir()
	app.Config = &cfg

	signer, err := signing.NewEdSigner()
	r.NoError(err)
	app.edSgn = signer
	address := wallet.Address(signer.PublicKey().Bytes())

	run := func(c *cobra.Command, args []string) {
		r.NoError(app.Initialize())

		// GRPC configuration
		app.Config.API.PublicListener = listener
		app.Config.API.PublicServices = []grpcserver.Service{grpcserver.Transaction}
		app.Config.API.PrivateServices = nil

		// Prevent obnoxious warning in macOS
		app.Config.P2P.Listen = p2p.MustParseAddresses("/ip4/127.0.0.1/tcp/7073")

		// Avoid waiting for new connections.
		app.Config.P2P.MinPeers = 0

		// syncer will cause the node to go out of sync (and not listen to gossip)
		// since we are testing single-node transaction service, we don't need the syncer to run
		app.Config.Sync.Interval = 1000000 * time.Second
		app.Config.LayerDuration = 2 * time.Second

		app.Config.Genesis = &config.GenesisConfig{
			GenesisTime: time.Now().Add(20 * time.Second).Format(time.RFC3339),
			Accounts: map[string]uint64{
				address.String(): 100_000_000,
			},
		}

		// This will block. We need to run the full app here to make sure that
		// the various services are reporting events correctly. This could probably
		// be done more surgically, and we don't need _all_ of the services.
		require.NoError(t, app.Start(context.Background()))
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
	require.Eventually(
		t,
		func() bool { return app.syncer.IsSynced(ctx) },
		4*time.Second,
		10*time.Millisecond,
	)

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(
		ctx,
		listener,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(conn.Close()) })
	c := pb.NewTransactionServiceClient(conn)

	tx1 := types.NewRawTx(
		wallet.SelfSpawn(signer.PrivateKey(), 0, sdk.WithGenesisID(cfg.Genesis.GenesisID())),
	)

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
	// Cleanup stops all services and thereby the app
	app.Cleanup(context.Background())

	// Wait for it to stop
	wg.Wait()
}

func TestInitialize_BadTortoiseParams(t *testing.T) {
	conf := config.DefaultConfig()
	conf.DataDirParent = t.TempDir()
	conf.FileLock = filepath.Join(t.TempDir(), "LOCK")
	app := New(WithLog(logtest.New(t)), WithConfig(&conf))
	require.NoError(t, app.Initialize())
	app.Cleanup(context.Background())

	conf = config.DefaultTestConfig()
	conf.DataDirParent = t.TempDir()
	conf.FileLock = filepath.Join(t.TempDir(), "LOCK")
	app = New(WithLog(logtest.New(t)), WithConfig(&conf))
	require.NoError(t, app.Initialize())
	app.Cleanup(context.Background())

	tconf := getTestDefaultConfig(t)
	tconf.DataDirParent = t.TempDir()
	app = New(WithLog(logtest.New(t)), WithConfig(tconf))
	require.NoError(t, app.Initialize())
	app.Cleanup(context.Background())

	conf.Tortoise.Zdist = 5
	app = New(WithLog(logtest.New(t)), WithConfig(&conf))
	err := app.Initialize()
	require.EqualError(t, err, "incompatible tortoise hare params")
}

func TestConfig_Preset(t *testing.T) {
	const name = "testnet"

	t.Run("PresetApplied", func(t *testing.T) {
		preset, err := presets.Get(name)
		require.NoError(t, err)

		c := &cobra.Command{}
		cmd.AddCommands(c)

		viper.Set("preset", name)
		t.Cleanup(viper.Reset)
		t.Cleanup(cmd.ResetConfig)
		conf, err := loadConfig(c)
		require.NoError(t, err)
		require.Equal(t, preset, *conf)
	})

	t.Run("PresetOverwrittenByFlags", func(t *testing.T) {
		preset, err := presets.Get(name)
		require.NoError(t, err)

		c := &cobra.Command{}
		cmd.AddCommands(c)
		const lowPeers = 1234
		require.NoError(t, c.ParseFlags([]string{"--low-peers=" + strconv.Itoa(lowPeers)}))

		viper.Set("preset", name)
		t.Cleanup(viper.Reset)
		t.Cleanup(cmd.ResetConfig)
		conf, err := loadConfig(c)
		require.NoError(t, err)
		preset.P2P.LowPeers = lowPeers
		require.Equal(t, preset, *conf)
	})

	t.Run("PresetOverWrittenByConfigFile", func(t *testing.T) {
		preset, err := presets.Get(name)
		require.NoError(t, err)

		c := &cobra.Command{}
		cmd.AddCommands(c)
		const lowPeers = 1234

		content := fmt.Sprintf(`{"p2p": {"low-peers": %d}}`, lowPeers)
		path := filepath.Join(t.TempDir(), "config.json")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		require.NoError(t, c.ParseFlags([]string{"--config=" + path}))

		viper.Set("preset", name)
		t.Cleanup(viper.Reset)
		t.Cleanup(cmd.ResetConfig)
		conf, err := loadConfig(c)
		require.NoError(t, err)
		preset.P2P.LowPeers = lowPeers
		preset.ConfigFile = path
		require.Equal(t, preset, *conf)
	})

	t.Run("LoadedFromConfigFile", func(t *testing.T) {
		preset, err := presets.Get(name)
		require.NoError(t, err)

		c := &cobra.Command{}
		cmd.AddCommands(c)

		content := fmt.Sprintf(`{"preset": "%s"}`, name)
		path := filepath.Join(t.TempDir(), "config.json")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		require.NoError(t, c.ParseFlags([]string{"--config=" + path}))

		t.Cleanup(viper.Reset)
		t.Cleanup(cmd.ResetConfig)

		conf, err := loadConfig(c)
		require.NoError(t, err)
		preset.ConfigFile = path
		require.Equal(t, preset, *conf)
	})
}

func TestConfig_CustomTypes(t *testing.T) {
	const name = "testnet"

	tt := []struct {
		name         string
		cli          string
		config       string
		updatePreset func(*testing.T, *config.Config)
	}{
		{
			name:   "smeshing-opts-provider",
			cli:    "--smeshing-opts-provider=1337",
			config: `{"smeshing": {"smeshing-opts": {"smeshing-opts-provider": 1337}}}`,
			updatePreset: func(t *testing.T, c *config.Config) {
				c.SMESHING.Opts.ProviderID.SetInt64(1337)
			},
		},
		{
			// TODO(mafa): remove this test case, see https://github.com/spacemeshos/go-spacemesh/issues/4801
			name:   "smeshing-opts-provider",
			cli:    "--smeshing-opts-provider=-1",
			config: `{"smeshing": {"smeshing-opts": {"smeshing-opts-provider": -1}}}`,
			updatePreset: func(t *testing.T, c *config.Config) {
				c.SMESHING.Opts.ProviderID.SetInt64(-1)
			},
		},
		{
			name:   "post-pow-difficulty",
			cli:    "--post-pow-difficulty=00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
			config: `{"post": {"post-pow-difficulty": "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"}}`,
			updatePreset: func(t *testing.T, c *config.Config) {
				diff, err := hex.DecodeString(
					"00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
				)
				require.NoError(t, err)
				copy(c.POST.PowDifficulty[:], diff)
			},
		},
		{
			name:   "address-list-single",
			cli:    "--listen=/ip4/0.0.0.0/tcp/5555 --advertise-address=/ip4/10.20.30.40/tcp/5555",
			config: `{"p2p":{"listen":"/ip4/0.0.0.0/tcp/5555","advertise-address":"/ip4/10.20.30.40/tcp/5555"}}`,
			updatePreset: func(t *testing.T, c *config.Config) {
				c.P2P.Listen = p2p.MustParseAddresses("/ip4/0.0.0.0/tcp/5555")
				c.P2P.AdvertiseAddress = p2p.MustParseAddresses("/ip4/10.20.30.40/tcp/5555")
			},
		},
		{
			name: "address-list-multiple",
			cli: "--listen=/ip4/0.0.0.0/tcp/5555 --listen=/ip4/0.0.0.0/udp/5555/quic-v1" +
				" --advertise-address=/ip4/10.20.30.40/tcp/5555" +
				" --advertise-address=/ip4/10.20.30.40/udp/5555/quic-v1",
			config: `{"p2p":{"listen":["/ip4/0.0.0.0/tcp/5555","/ip4/0.0.0.0/udp/5555/quic-v1"],
                                  "advertise-address":[
                                    "/ip4/10.20.30.40/tcp/5555","/ip4/10.20.30.40/udp/5555/quic-v1"]}}`,
			updatePreset: func(t *testing.T, c *config.Config) {
				c.P2P.Listen = p2p.MustParseAddresses(
					"/ip4/0.0.0.0/tcp/5555",
					"/ip4/0.0.0.0/udp/5555/quic-v1")
				c.P2P.AdvertiseAddress = p2p.MustParseAddresses(
					"/ip4/10.20.30.40/tcp/5555",
					"/ip4/10.20.30.40/udp/5555/quic-v1")
			},
		},
	}

	for _, tc := range tt {
		t.Run(fmt.Sprintf("%s_Flags", tc.name), func(t *testing.T) {
			mainnet := config.MainnetConfig()
			require.Nil(t, mainnet.SMESHING.Opts.ProviderID.Value())

			c := &cobra.Command{}
			cmd.AddCommands(c)
			require.NoError(t, c.ParseFlags(strings.Fields(tc.cli)))

			t.Cleanup(viper.Reset)
			t.Cleanup(cmd.ResetConfig)

			conf, err := loadConfig(c)
			require.NoError(t, err)
			tc.updatePreset(t, &mainnet)
			require.Equal(t, mainnet, *conf)
		})

		t.Run(fmt.Sprintf("%s_ConfigFile", tc.name), func(t *testing.T) {
			mainnet := config.MainnetConfig()
			require.Nil(t, mainnet.SMESHING.Opts.ProviderID.Value())

			c := &cobra.Command{}
			cmd.AddCommands(c)

			path := filepath.Join(t.TempDir(), "config.json")
			require.NoError(t, os.WriteFile(path, []byte(tc.config), 0o600))
			require.NoError(t, c.ParseFlags([]string{"--config=" + path}))

			t.Cleanup(viper.Reset)
			t.Cleanup(cmd.ResetConfig)

			conf, err := loadConfig(c)
			require.NoError(t, err)
			tc.updatePreset(t, &mainnet)
			mainnet.ConfigFile = path
			require.Equal(t, mainnet, *conf)
		})

		t.Run(fmt.Sprintf("%s_PresetOverwrittenByFlags", tc.name), func(t *testing.T) {
			preset, err := presets.Get(name)
			require.NoError(t, err)

			c := &cobra.Command{}
			cmd.AddCommands(c)
			require.NoError(t, c.ParseFlags(strings.Fields(tc.cli)))

			viper.Set("preset", name)
			t.Cleanup(viper.Reset)
			t.Cleanup(cmd.ResetConfig)

			conf, err := loadConfig(c)
			require.NoError(t, err)
			tc.updatePreset(t, &preset)
			require.Equal(t, preset, *conf)
		})

		t.Run(fmt.Sprintf("%s_PresetOverWrittenByConfigFile", tc.name), func(t *testing.T) {
			preset, err := presets.Get(name)
			require.NoError(t, err)

			c := &cobra.Command{}
			cmd.AddCommands(c)

			path := filepath.Join(t.TempDir(), "config.json")
			require.NoError(t, os.WriteFile(path, []byte(tc.config), 0o600))
			require.NoError(t, c.ParseFlags([]string{"--config=" + path}))

			viper.Set("preset", name)
			t.Cleanup(viper.Reset)
			t.Cleanup(cmd.ResetConfig)

			conf, err := loadConfig(c)
			require.NoError(t, err)
			tc.updatePreset(t, &preset)
			preset.ConfigFile = path
			require.Equal(t, preset, *conf)
		})
	}
}

func TestConfig_PostProviderID_InvalidValues(t *testing.T) {
	tt := []struct {
		name        string
		cliValue    string
		configValue string
	}{
		{
			name:        "not a number",
			cliValue:    "not-a-number",
			configValue: "\"not-a-number\"",
		},
		// TODO(mafa): still accepted for backward compatibility, see https://github.com/spacemeshos/go-spacemesh/issues/4801
		// {
		// 	name:        "negative number",
		// 	cliValue:    "-1",
		// 	configValue: "-1",
		// },
		{
			name:        "number too large for uint32",
			cliValue:    "4294967296",
			configValue: "4294967296",
		},
	}

	for _, tc := range tt {
		t.Run(fmt.Sprintf("%s_Flags", tc.name), func(t *testing.T) {
			c := &cobra.Command{}
			cmd.AddCommands(c)
			t.Cleanup(cmd.ResetConfig)

			err := c.ParseFlags([]string{fmt.Sprintf("--smeshing-opts-provider=%s", tc.cliValue)})
			require.ErrorContains(t, err, "failed to parse PoST Provider ID")
		})

		t.Run(fmt.Sprintf("%s_ConfigFile", tc.name), func(t *testing.T) {
			c := &cobra.Command{}
			cmd.AddCommands(c)

			path := filepath.Join(t.TempDir(), "config.json")
			require.NoError(
				t,
				os.WriteFile(
					path,
					[]byte(
						fmt.Sprintf(
							`{"smeshing": {"smeshing-opts": {"smeshing-opts-provider": %s}}}`,
							tc.configValue,
						),
					),
					0o600,
				),
			)
			require.NoError(t, c.ParseFlags([]string{"--config=" + path}))

			t.Cleanup(cmd.ResetConfig)

			_, err := loadConfig(c)
			require.ErrorContains(t, err, "invalid provider ID value")
		})
	}
}

func TestConfig_Load(t *testing.T) {
	t.Run("invalid fails to load", func(t *testing.T) {
		c := &cobra.Command{}
		cmd.AddCommands(c)

		path := filepath.Join(t.TempDir(), "config.json")
		require.NoError(t, os.WriteFile(path, []byte("}"), 0o600))
		require.NoError(t, c.ParseFlags([]string{"--config=" + path}))

		t.Cleanup(viper.Reset)
		t.Cleanup(cmd.ResetConfig)

		_, err := loadConfig(c)
		require.ErrorContains(t, err, path)
	})
	t.Run("missing default doesn't fail", func(t *testing.T) {
		c := &cobra.Command{}
		cmd.AddCommands(c)
		require.NoError(t, c.ParseFlags([]string{}))

		t.Cleanup(viper.Reset)
		t.Cleanup(cmd.ResetConfig)

		_, err := loadConfig(c)
		require.NoError(t, err)
	})
}

func TestConfig_GenesisAccounts(t *testing.T) {
	t.Run("OverwriteDefaults", func(t *testing.T) {
		c := &cobra.Command{}
		cmd.AddCommands(c)
		t.Cleanup(cmd.ResetConfig)

		const value = 100
		keys := []string{"0x03", "0x04"}
		args := []string{}
		for _, key := range keys {
			args = append(args, fmt.Sprintf("-a %s=%d", key, value))
		}
		require.NoError(t, c.ParseFlags(args))

		conf, err := loadConfig(c)
		require.NoError(t, err)
		for _, key := range keys {
			require.EqualValues(t, conf.Genesis.Accounts[key], value)
		}
	})
}

func TestHRP(t *testing.T) {
	c := &cobra.Command{}
	cmd.AddCommands(c)
	t.Cleanup(cmd.ResetConfig)

	data := `{"main": {"network-hrp": "TEST"}}`
	cfg := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(cfg, []byte(data), 0o600))
	require.NoError(t, c.ParseFlags([]string{"-c=" + cfg}))
	conf, err := loadConfig(c)
	require.NoError(t, err)
	app := New(WithConfig(conf))
	require.NotNil(t, app)
	require.Equal(t, "TEST", types.NetworkHRP())
}

func TestGenesisConfig(t *testing.T) {
	t.Run("config is written to a file", func(t *testing.T) {
		app := New()
		app.Config = getTestDefaultConfig(t)
		app.Config.DataDirParent = t.TempDir()

		require.NoError(t, app.Initialize())
		t.Cleanup(func() { app.Cleanup(context.Background()) })

		var existing config.GenesisConfig
		require.NoError(
			t,
			existing.LoadFromFile(filepath.Join(app.Config.DataDir(), genesisFileName)),
		)
		require.Empty(t, existing.Diff(app.Config.Genesis))
	})

	t.Run("no error if no diff", func(t *testing.T) {
		app := New()
		app.Config = getTestDefaultConfig(t)
		app.Config.DataDirParent = t.TempDir()

		require.NoError(t, app.Initialize())
		app.Cleanup(context.Background())

		require.NoError(t, app.Initialize())
		app.Cleanup(context.Background())
	})

	t.Run("fatal error on a diff", func(t *testing.T) {
		app := New()
		app.Config = getTestDefaultConfig(t)
		app.Config.DataDirParent = t.TempDir()

		require.NoError(t, app.Initialize())
		t.Cleanup(func() { app.Cleanup(context.Background()) })

		app.Config.Genesis.ExtraData = "changed"
		app.Cleanup(context.Background())
		err := app.Initialize()
		require.ErrorContains(t, err, "genesis config")
	})

	t.Run("not valid time", func(t *testing.T) {
		app := New()
		app.Config = getTestDefaultConfig(t)
		app.Config.DataDirParent = t.TempDir()
		app.Config.Genesis.GenesisTime = time.Now().Format(time.RFC1123)

		require.ErrorContains(t, app.Initialize(), "time.RFC3339")
	})
	t.Run("long extra data", func(t *testing.T) {
		app := New()
		app.Config = getTestDefaultConfig(t)
		app.Config.DataDirParent = t.TempDir()
		app.Config.Genesis.ExtraData = string(make([]byte, 256))

		require.ErrorContains(t, app.Initialize(), "extra-data")
	})
}

func TestFlock(t *testing.T) {
	t.Run("sanity", func(t *testing.T) {
		app := New()
		app.Config = getTestDefaultConfig(t)

		require.NoError(t, app.Lock())
		t.Cleanup(app.Unlock)

		app1 := *app
		require.ErrorContains(t, app1.Lock(), "only one spacemesh instance")
		app.Unlock()
		require.NoError(t, app.Lock())
	})

	t.Run("dir doesn't exist", func(t *testing.T) {
		app := New()
		app.Config = getTestDefaultConfig(t)
		app.Config.FileLock = filepath.Join(t.TempDir(), "newdir", "LOCK")

		require.NoError(t, app.Lock())
		t.Cleanup(app.Unlock)
	})
}

func TestAdminEvents(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	cfg, err := presets.Get("standalone")
	require.NoError(t, err)
	cfg.DataDirParent = t.TempDir()
	cfg.FileLock = filepath.Join(cfg.DataDirParent, "LOCK")
	cfg.SMESHING.Opts.DataDir = t.TempDir()
	cfg.Genesis.GenesisTime = time.Now().Add(5 * time.Second).Format(time.RFC3339)
	types.SetLayersPerEpoch(cfg.LayersPerEpoch)

	app := New(WithConfig(&cfg), WithLog(logtest.New(t)))
	signer, err := app.LoadOrCreateEdSigner()
	require.NoError(t, err)
	app.edSgn = signer // https://github.com/spacemeshos/go-spacemesh/issues/4653
	require.NoError(t, app.Initialize())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var eg errgroup.Group
	eg.Go(func() error {
		if err := app.Start(ctx); err != nil {
			return err
		}
		app.Cleanup(context.Background())
		app.eg.Wait() // https://github.com/spacemeshos/go-spacemesh/issues/4653
		return nil
	})
	t.Cleanup(func() { eg.Wait() })

	grpcCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(
		grpcCtx,
		cfg.API.PrivateListener,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, conn.Close()) })
	client := pb.NewAdminServiceClient(conn)

	tctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	// 4 is arbitrary, if we received events once, they must be
	// cached and should be returned immediately
	for i := 0; i < 4; i++ {
		stream, err := client.EventsStream(tctx, &pb.EventStreamRequest{})
		require.NoError(t, err)
		success := []pb.IsEventDetails{
			&pb.Event_Beacon{},
			&pb.Event_InitStart{},
			&pb.Event_InitComplete{},
			&pb.Event_PostStart{},
			&pb.Event_PostComplete{},
			&pb.Event_PoetWaitRound{},
			&pb.Event_PoetWaitProof{},
			&pb.Event_PostStart{},
			&pb.Event_PostComplete{},
			&pb.Event_AtxPublished{},
		}
		for _, ev := range success {
			msg, err := stream.Recv()
			require.NoError(t, err, "stream %d", i)
			require.IsType(t, ev, msg.Details, "stream %d", i)
		}
		require.NoError(t, stream.CloseSend())
	}
}

func TestEmptyExtraData(t *testing.T) {
	cfg := getTestDefaultConfig(t)
	cfg.Genesis.ExtraData = ""
	app := New(WithConfig(cfg), WithLog(logtest.New(t)))
	require.Error(t, app.Initialize())
}

func getTestDefaultConfig(tb testing.TB) *config.Config {
	cfg, err := LoadConfigFromFile()
	require.NoError(tb, err, "cannot load config from file")

	// is set to 0 to make sync start immediately when node starts
	cfg.P2P.MinPeers = 0

	cfg.POST = activation.DefaultPostConfig()
	cfg.POST.MinNumUnits = 2
	cfg.POST.MaxNumUnits = 4
	cfg.POST.LabelsPerUnit = 32
	cfg.POST.K2 = 4

	cfg.SMESHING = config.DefaultSmeshingConfig()
	cfg.SMESHING.Start = true
	cfg.SMESHING.Opts.NumUnits = cfg.POST.MinNumUnits + 1
	cfg.SMESHING.Opts.ProviderID.SetInt64(int64(initialization.CPUProviderID()))

	// note: these need to be set sufficiently low enough that turbohare finishes well before the LayerDurationSec
	cfg.HARE.RoundDuration = 2
	cfg.HARE.WakeupDelta = 1
	cfg.HARE.N = 5
	cfg.HARE.ExpectedLeaders = 5

	cfg.HARE3.RoundDuration = 2
	cfg.HARE3.PreroundDelay = 1

	cfg.LayerAvgSize = 5
	cfg.LayersPerEpoch = 3
	cfg.TxsPerProposal = 100
	cfg.Tortoise.Hdist = 5
	cfg.Tortoise.Zdist = 5

	cfg.LayerDuration = 20 * time.Second
	cfg.HareEligibility.ConfidenceParam = 1
	cfg.Sync.Interval = 2 * time.Second
	tmp := tb.TempDir()
	cfg.DataDirParent = tmp
	cfg.FileLock = filepath.Join(tmp, "LOCK")

	cfg.FETCH.RequestTimeout = 10
	cfg.FETCH.BatchSize = 5
	cfg.FETCH.BatchTimeout = 5

	cfg.Beacon = beacon.NodeSimUnitTestConfig()

	cfg.Genesis = config.DefaultTestGenesisConfig()

	types.SetLayersPerEpoch(cfg.LayersPerEpoch)

	return cfg
}
